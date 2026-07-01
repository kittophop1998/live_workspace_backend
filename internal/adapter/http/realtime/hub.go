package realtime

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"kingdom_manager/backend/internal/adapter/http/middleware"
	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/usecase"
)

type message struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

type client struct {
	connection  *websocket.Conn
	send        chan message
	clientID    string
	workspaceID string
}

type Hub struct {
	service  *usecase.Service
	upgrader websocket.Upgrader
	mu       sync.RWMutex
	clients  map[*client]struct{}
	presence map[string]presence
}

type presence struct {
	ClientID       string `json:"client_id"`
	CollaboratorID string `json:"collaborator_id"`
	TS             int64  `json:"ts"`
	WorkspaceID    string `json:"-"`
}

func NewHub(allowedOrigins []string) *Hub {
	origins := make(map[string]bool, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origins[origin] = true
	}
	return &Hub{
		upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
			return r.Header.Get("Origin") == "" || origins[r.Header.Get("Origin")]
		}},
		clients: make(map[*client]struct{}), presence: make(map[string]presence),
	}
}

func (h *Hub) SetService(service *usecase.Service) { h.service = service }

func (h *Hub) Publish(event usecase.Event) {
	h.broadcast(event.WorkspaceID, message{Type: event.Type, Payload: eventPayload(event)})
}

func (h *Hub) Serve(c *gin.Context) {
	connection, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	workspaceID := middleware.WorkspaceID(c)
	current := &client{connection: connection, send: make(chan message, 32), workspaceID: workspaceID}
	h.mu.Lock()
	h.clients[current] = struct{}{}
	h.mu.Unlock()

	ws, err := h.service.ForWorkspace(workspaceID).Snapshot(c.Request.Context())
	if err != nil {
		_ = connection.Close()
		return
	}
	current.send <- message{Type: "snapshot", Payload: snapshotPayload(ws)}
	go h.writeLoop(current)
	h.readLoop(current, middleware.CollaboratorID(c))
}

func (h *Hub) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.prune()
		}
	}
}

func (h *Hub) readLoop(current *client, authenticatedID string) {
	defer h.remove(current)
	current.connection.SetReadLimit(4096)
	_ = current.connection.SetReadDeadline(time.Now().Add(12 * time.Second))
	current.connection.SetPongHandler(func(string) error {
		return current.connection.SetReadDeadline(time.Now().Add(12 * time.Second))
	})
	for {
		var input struct {
			Type    string   `json:"type"`
			Payload presence `json:"payload"`
		}
		if err := current.connection.ReadJSON(&input); err != nil {
			return
		}
		_ = current.connection.SetReadDeadline(time.Now().Add(12 * time.Second))
		switch input.Type {
		case "presence.heartbeat":
			if input.Payload.ClientID == "" || input.Payload.CollaboratorID != authenticatedID {
				continue
			}
			input.Payload.TS = time.Now().UnixMilli()
			input.Payload.WorkspaceID = current.workspaceID
			current.clientID = input.Payload.ClientID
			h.mu.Lock()
			h.presence[presenceKey(current.workspaceID, current.clientID)] = input.Payload
			h.mu.Unlock()
			h.broadcast(current.workspaceID, message{Type: "presence.update", Payload: input.Payload})
		case "presence.leave":
			if input.Payload.ClientID == current.clientID {
				h.leave(current.workspaceID, current.clientID)
			}
		}
	}
}

func (h *Hub) writeLoop(current *client) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case output, ok := <-current.send:
			if !ok {
				return
			}
			if err := current.connection.WriteJSON(output); err != nil {
				return
			}
		case <-ticker.C:
			if err := current.connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
				return
			}
		}
	}
}

func (h *Hub) remove(current *client) {
	h.mu.Lock()
	if _, exists := h.clients[current]; exists {
		delete(h.clients, current)
		close(current.send)
	}
	h.mu.Unlock()
	h.leave(current.workspaceID, current.clientID)
	_ = current.connection.Close()
}

func (h *Hub) leave(workspaceID, clientID string) {
	if clientID == "" {
		return
	}
	h.mu.Lock()
	key := presenceKey(workspaceID, clientID)
	_, exists := h.presence[key]
	delete(h.presence, key)
	h.mu.Unlock()
	if exists {
		h.broadcast(workspaceID, message{Type: "presence.leave", Payload: map[string]any{"client_id": clientID}})
	}
}

func (h *Hub) prune() {
	cutoff := time.Now().Add(-8 * time.Second).UnixMilli()
	h.mu.RLock()
	type stalePresence struct{ workspaceID, clientID string }
	stale := make([]stalePresence, 0)
	for _, value := range h.presence {
		if value.TS < cutoff {
			stale = append(stale, stalePresence{value.WorkspaceID, value.ClientID})
		}
	}
	h.mu.RUnlock()
	for _, item := range stale {
		h.leave(item.workspaceID, item.clientID)
	}
}

func (h *Hub) broadcast(workspaceID string, output message) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for current := range h.clients {
		if current.workspaceID != workspaceID {
			continue
		}
		select {
		case current.send <- output:
		default:
		}
	}
}

func presenceKey(workspaceID, clientID string) string { return workspaceID + ":" + clientID }

func eventPayload(event usecase.Event) any {
	switch value := event.Payload.(type) {
	case *usecase.ClearResult:
		return map[string]any{"rev": value.Rev, "resource_ids": value.ResourceIDs}
	case *usecase.MutationResult:
		if event.Type == "resource.deleted" && value.Resource != nil {
			return map[string]any{"rev": value.Rev, "resource_id": value.Resource.ID}
		}
		if event.Type == "comment.deleted" && value.Comment != nil {
			return map[string]any{"rev": value.Rev, "comment_id": value.Comment.ID}
		}
		if value.Resource != nil {
			return map[string]any{"rev": value.Rev, "resource": resourcePayload(*value.Resource)}
		}
		if value.Comment != nil {
			return map[string]any{"rev": value.Rev, "comment": commentPayload(*value.Comment)}
		}
	case entity.ActivityEvent:
		return map[string]any{"activity": activityPayload(value)}
	}
	return event.Payload
}

func snapshotPayload(ws *entity.Workspace) map[string]any {
	resources := make([]any, 0, len(ws.Resources))
	for _, item := range ws.Resources {
		resources = append(resources, resourcePayload(item))
	}
	comments := make([]any, 0, len(ws.Comments))
	for _, item := range ws.Comments {
		comments = append(comments, commentPayload(item))
	}
	activity := make([]any, 0, len(ws.Activity))
	for i := len(ws.Activity) - 1; i >= 0; i-- {
		activity = append(activity, activityPayload(ws.Activity[i]))
	}
	collaborators := make([]any, 0, len(ws.Collaborators))
	for _, item := range ws.Collaborators {
		collaborators = append(collaborators, map[string]any{"id": item.ID, "name": item.Name, "role": item.Role, "color": item.Color})
	}
	return map[string]any{"rev": ws.Rev, "workspace_id": ws.ID, "resources": resources, "comments": comments, "activity": activity, "collaborators": collaborators, "server_time": time.Now().UTC()}
}

func resourcePayload(value entity.Resource) map[string]any {
	fields := make([]any, 0, len(value.Fields))
	for _, field := range value.Fields {
		fields = append(fields, map[string]any{"id": field.ID, "key": field.Key, "type": field.Type, "required": field.Required, "state": field.State, "change": field.Change, "description": field.Description, "value": field.Value})
	}
	return map[string]any{"id": value.ID, "name": value.Name, "kind": value.Kind, "method": value.Method, "path": value.Path, "state": value.State, "status": value.Status, "fields": fields, "updated_at": value.UpdatedAt, "updated_by": value.UpdatedBy}
}
func commentPayload(value entity.Comment) map[string]any {
	return map[string]any{"id": value.ID, "resource_id": value.ResourceID, "field_id": value.FieldID, "author": value.Author, "role": value.Role, "body": value.Body, "at": value.At}
}
func activityPayload(value entity.ActivityEvent) map[string]any {
	return map[string]any{"id": value.ID, "actor": value.Actor, "verb": value.Verb, "target": value.Target, "resource_id": value.ResourceID, "at": value.At}
}
