package usecase

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
)

var fieldTypes = map[string]bool{
	"string": true, "number": true, "boolean": true, "uuid": true,
	"timestamp": true, "json": true, "string[]": true, "number[]": true, "enum": true,
}

type Event struct {
	Type        string
	Payload     any
	WorkspaceID string
}

type Publisher interface {
	Publish(Event)
}

type Service struct {
	repo        port.WorkspaceRepository
	workspaceID string
	publisher   Publisher
	now         func() time.Time
}

func NewService(repo port.WorkspaceRepository, workspaceID string, publisher Publisher) *Service {
	return &Service{repo: repo, workspaceID: workspaceID, publisher: publisher, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) ForWorkspace(workspaceID string) *Service {
	return &Service{repo: s.repo, workspaceID: workspaceID, publisher: s.publisher, now: s.now}
}

func (s *Service) Snapshot(ctx context.Context) (*entity.Workspace, error) {
	return s.repo.Get(ctx, s.workspaceID)
}

func (s *Service) Me(ctx context.Context, collaboratorID string) (*entity.Collaborator, error) {
	ws, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	for i := range ws.Collaborators {
		if ws.Collaborators[i].ID == collaboratorID {
			return &ws.Collaborators[i], nil
		}
	}
	return nil, notFound("collaborator", collaboratorID)
}

type CreateResourceInput struct {
	Name, Kind, Method, Path string
}

type UpdateResourceInput struct {
	Name, Method, Path *string
}

type FieldInput struct {
	Key         string
	Type        string
	Required    bool
	State       string
	Description *string
}

type UpdateFieldInput struct {
	Key, Type, State *string
	Required         *bool
	Description      **string
}

type MutationResult struct {
	Rev      int64
	Resource *entity.Resource
	Comment  *entity.Comment
}

func (s *Service) Resources(ctx context.Context, kind string) ([]entity.Resource, error) {
	ws, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if kind == "" {
		return ws.Resources, nil
	}
	if !validKind(kind) {
		return nil, validation("invalid resource kind", map[string]any{"kind": kind})
	}
	out := make([]entity.Resource, 0)
	for _, resource := range ws.Resources {
		if string(resource.Kind) == kind {
			out = append(out, resource)
		}
	}
	return out, nil
}

func (s *Service) Resource(ctx context.Context, id string) (*entity.Resource, error) {
	ws, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	resource, _ := findResource(ws, id)
	if resource == nil {
		return nil, notFound("resource", id)
	}
	return resource, nil
}

func (s *Service) CreateResource(ctx context.Context, actorID string, expected *int64, in CreateResourceInput) (*MutationResult, error) {
	if strings.TrimSpace(in.Name) == "" || !validKind(in.Kind) {
		return nil, validation("name and valid kind are required", nil)
	}
	if in.Kind == string(entity.KindEndpoint) {
		if !validMethod(in.Method) || !strings.HasPrefix(in.Path, "/") {
			return nil, validation("endpoint requires a valid method and absolute path", nil)
		}
	} else if in.Method != "" || in.Path != "" {
		return nil, validation("method and path are only valid for endpoints", nil)
	}
	return s.mutateResource(ctx, actorID, expected, "resource.created", func(ws *entity.Workspace, actor entity.Collaborator) (*entity.Resource, entity.ActivityEvent, error) {
		now := s.now()
		resource := entity.Resource{
			ID: "res_" + shortID(), Name: strings.TrimSpace(in.Name), Kind: entity.ResourceKind(in.Kind),
			State: entity.StateDraft, Fields: []entity.SchemaField{{ID: "fld_" + shortID(), Key: "id", Type: "uuid", Required: true, State: entity.StateDraft, Change: entity.ChangeAdded}},
			UpdatedAt: now, UpdatedBy: actor.Name,
		}
		if in.Kind == string(entity.KindEndpoint) {
			method, path := strings.ToUpper(in.Method), in.Path
			resource.Method, resource.Path = &method, &path
		}
		ws.Resources = append(ws.Resources, resource)
		return &ws.Resources[len(ws.Resources)-1], activity(actor, "created", resource.Name, resource.ID, now), nil
	})
}

func (s *Service) UpdateResource(ctx context.Context, actorID, id string, expected *int64, in UpdateResourceInput) (*MutationResult, error) {
	return s.mutateResource(ctx, actorID, expected, "resource.updated", func(ws *entity.Workspace, actor entity.Collaborator) (*entity.Resource, entity.ActivityEvent, error) {
		resource, _ := findResource(ws, id)
		if resource == nil {
			return nil, entity.ActivityEvent{}, notFound("resource", id)
		}
		target := resource.Name
		if in.Name != nil {
			if strings.TrimSpace(*in.Name) == "" {
				return nil, entity.ActivityEvent{}, validation("name cannot be empty", nil)
			}
			target = resource.Name + " → " + strings.TrimSpace(*in.Name)
			resource.Name = strings.TrimSpace(*in.Name)
		}
		if in.Method != nil {
			if resource.Kind != entity.KindEndpoint || !validMethod(*in.Method) {
				return nil, entity.ActivityEvent{}, validation("invalid endpoint method", nil)
			}
			value := strings.ToUpper(*in.Method)
			resource.Method = &value
		}
		if in.Path != nil {
			if resource.Kind != entity.KindEndpoint || !strings.HasPrefix(*in.Path, "/") {
				return nil, entity.ActivityEvent{}, validation("invalid endpoint path", nil)
			}
			resource.Path = in.Path
		}
		resource.UpdatedAt, resource.UpdatedBy = s.now(), actor.Name
		return resource, activity(actor, "edited", target, id, resource.UpdatedAt), nil
	})
}

func (s *Service) DeleteResource(ctx context.Context, actorID, id string, expected *int64) (*MutationResult, error) {
	return s.mutateResource(ctx, actorID, expected, "resource.deleted", func(ws *entity.Workspace, actor entity.Collaborator) (*entity.Resource, entity.ActivityEvent, error) {
		resource, index := findResource(ws, id)
		if resource == nil {
			return nil, entity.ActivityEvent{}, notFound("resource", id)
		}
		deleted := *resource
		ws.Resources = append(ws.Resources[:index], ws.Resources[index+1:]...)
		ws.Comments = removeResourceComments(ws.Comments, id)
		return &deleted, activity(actor, "removed", deleted.Name, id, s.now()), nil
	})
}

func (s *Service) AddField(ctx context.Context, actorID, resourceID string, expected *int64, in FieldInput) (*MutationResult, error) {
	if err := validateField(in.Key, in.Type, in.State); err != nil {
		return nil, err
	}
	return s.mutateResource(ctx, actorID, expected, "field.created", func(ws *entity.Workspace, actor entity.Collaborator) (*entity.Resource, entity.ActivityEvent, error) {
		resource, _ := findResource(ws, resourceID)
		if resource == nil {
			return nil, entity.ActivityEvent{}, notFound("resource", resourceID)
		}
		if fieldKeyExists(resource, in.Key, "") {
			return nil, entity.ActivityEvent{}, validation("field key already exists on this resource", map[string]any{"key": in.Key})
		}
		state := entity.StateDraft
		if in.State != "" {
			state = entity.FieldState(in.State)
		}
		resource.Fields = append(resource.Fields, entity.SchemaField{ID: "fld_" + shortID(), Key: in.Key, Type: in.Type, Required: in.Required, State: state, Change: entity.ChangeAdded, Description: in.Description})
		resource.RollupState()
		resource.UpdatedAt, resource.UpdatedBy = s.now(), actor.Name
		return resource, activity(actor, "added", in.Key, resourceID, resource.UpdatedAt), nil
	})
}

func (s *Service) UpdateField(ctx context.Context, actorID, resourceID, fieldID string, expected *int64, in UpdateFieldInput) (*MutationResult, error) {
	return s.mutateResource(ctx, actorID, expected, "field.updated", func(ws *entity.Workspace, actor entity.Collaborator) (*entity.Resource, entity.ActivityEvent, error) {
		resource, _ := findResource(ws, resourceID)
		if resource == nil {
			return nil, entity.ActivityEvent{}, notFound("resource", resourceID)
		}
		field := findField(resource, fieldID)
		if field == nil {
			return nil, entity.ActivityEvent{}, notFound("field", fieldID)
		}
		oldKey := field.Key
		if in.Key != nil {
			if strings.TrimSpace(*in.Key) == "" || fieldKeyExists(resource, *in.Key, fieldID) {
				return nil, entity.ActivityEvent{}, validation("field key is empty or already exists", map[string]any{"key": *in.Key})
			}
			field.Key = strings.TrimSpace(*in.Key)
		}
		if in.Type != nil {
			if !fieldTypes[*in.Type] {
				return nil, entity.ActivityEvent{}, validation("invalid field type", map[string]any{"type": *in.Type})
			}
			field.Type = *in.Type
		}
		if in.State != nil {
			if !validState(*in.State) {
				return nil, entity.ActivityEvent{}, validation("invalid field state", nil)
			}
			field.State = entity.FieldState(*in.State)
		}
		if in.Required != nil {
			field.Required = *in.Required
		}
		if in.Description != nil {
			field.Description = *in.Description
		}
		if field.Change == entity.ChangeStable {
			field.Change = entity.ChangeModified
		}
		resource.RollupState()
		resource.UpdatedAt, resource.UpdatedBy = s.now(), actor.Name
		return resource, activity(actor, "edited", oldKey, resourceID, resource.UpdatedAt), nil
	})
}

func (s *Service) DeleteField(ctx context.Context, actorID, resourceID, fieldID string, expected *int64) (*MutationResult, error) {
	return s.mutateResource(ctx, actorID, expected, "field.removed", func(ws *entity.Workspace, actor entity.Collaborator) (*entity.Resource, entity.ActivityEvent, error) {
		resource, _ := findResource(ws, resourceID)
		if resource == nil {
			return nil, entity.ActivityEvent{}, notFound("resource", resourceID)
		}
		field, index := findFieldIndex(resource, fieldID)
		if field == nil {
			return nil, entity.ActivityEvent{}, notFound("field", fieldID)
		}
		key := field.Key
		if field.Change == entity.ChangeAdded {
			resource.Fields = append(resource.Fields[:index], resource.Fields[index+1:]...)
		} else {
			field.Change, field.State = entity.ChangeRemoved, entity.StateBreaking
		}
		resource.RollupState()
		resource.UpdatedAt, resource.UpdatedBy = s.now(), actor.Name
		return resource, activity(actor, "removed", key, resourceID, resource.UpdatedAt), nil
	})
}

func (s *Service) Comments(ctx context.Context, resourceID, fieldID string) ([]entity.Comment, error) {
	ws, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if resource, _ := findResource(ws, resourceID); resource == nil {
		return nil, notFound("resource", resourceID)
	}
	out := make([]entity.Comment, 0)
	for _, comment := range ws.Comments {
		if comment.ResourceID == resourceID && (fieldID == "" || comment.FieldID != nil && *comment.FieldID == fieldID) {
			out = append(out, comment)
		}
	}
	return out, nil
}

func (s *Service) AddComment(ctx context.Context, actorID, resourceID string, expected *int64, fieldID *string, body string) (*MutationResult, error) {
	if strings.TrimSpace(body) == "" {
		return nil, validation("comment body is required", nil)
	}
	var result *entity.Comment
	mutation, err := s.mutateResource(ctx, actorID, expected, "comment.created", func(ws *entity.Workspace, actor entity.Collaborator) (*entity.Resource, entity.ActivityEvent, error) {
		resource, _ := findResource(ws, resourceID)
		if resource == nil {
			return nil, entity.ActivityEvent{}, notFound("resource", resourceID)
		}
		if fieldID != nil && findField(resource, *fieldID) == nil {
			return nil, entity.ActivityEvent{}, notFound("field", *fieldID)
		}
		comment := entity.Comment{ID: "cmt_" + shortID(), ResourceID: resourceID, FieldID: fieldID, AuthorID: actor.ID, Author: actor.Name, Role: actor.Role, Body: strings.TrimSpace(body), At: s.now()}
		ws.Comments = append(ws.Comments, comment)
		result = &ws.Comments[len(ws.Comments)-1]
		return resource, activity(actor, "commented on", resource.Name, resourceID, comment.At), nil
	})
	if err != nil {
		return nil, err
	}
	mutation.Resource, mutation.Comment = nil, result
	return mutation, nil
}

func (s *Service) DeleteComment(ctx context.Context, actorID, commentID string, expected *int64) (*MutationResult, error) {
	var deleted *entity.Comment
	result, err := s.mutateResource(ctx, actorID, expected, "comment.deleted", func(ws *entity.Workspace, actor entity.Collaborator) (*entity.Resource, entity.ActivityEvent, error) {
		for i := range ws.Comments {
			if ws.Comments[i].ID != commentID {
				continue
			}
			if ws.Comments[i].AuthorID != actor.ID {
				return nil, entity.ActivityEvent{}, &Error{Kind: ErrForbidden, Message: "only the comment author may delete it"}
			}
			value := ws.Comments[i]
			deleted = &value
			ws.Comments = append(ws.Comments[:i], ws.Comments[i+1:]...)
			resource, _ := findResource(ws, value.ResourceID)
			return resource, activity(actor, "removed", "comment", value.ResourceID, s.now()), nil
		}
		return nil, entity.ActivityEvent{}, notFound("comment", commentID)
	})
	if err != nil {
		return nil, err
	}
	result.Resource, result.Comment = nil, deleted
	return result, nil
}

func (s *Service) Activity(ctx context.Context, resourceID string, page, limit int) ([]entity.ActivityEvent, int, error) {
	ws, err := s.Snapshot(ctx)
	if err != nil {
		return nil, 0, err
	}
	items := make([]entity.ActivityEvent, 0)
	for _, event := range ws.Activity {
		if resourceID == "" || event.ResourceID == resourceID {
			items = append(items, event)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].At.After(items[j].At) })
	total := len(items)
	start := (page - 1) * limit
	if start >= total {
		return []entity.ActivityEvent{}, total, nil
	}
	end := min(start+limit, total)
	return items[start:end], total, nil
}

func (s *Service) mutateResource(ctx context.Context, actorID string, expected *int64, eventType string, fn func(*entity.Workspace, entity.Collaborator) (*entity.Resource, entity.ActivityEvent, error)) (*MutationResult, error) {
	ws, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if expected != nil && *expected != ws.Rev {
		return nil, &Error{Kind: ErrRevConflict, Message: "workspace revision is stale", Details: map[string]any{"current_rev": ws.Rev}}
	}
	actor, ok := collaborator(ws, actorID)
	if !ok {
		return nil, notFound("collaborator", actorID)
	}
	oldRev := ws.Rev
	resource, event, err := fn(ws, actor)
	if err != nil {
		return nil, err
	}
	ws.Rev++
	ws.Activity = append(ws.Activity, event)
	if len(ws.Activity) > 1000 {
		ws.Activity = ws.Activity[len(ws.Activity)-1000:]
	}
	if err := s.repo.Save(ctx, ws, oldRev); err != nil {
		if errors.Is(err, port.ErrRevisionConflict) {
			return nil, &Error{Kind: ErrRevConflict, Message: "workspace was changed by another client"}
		}
		return nil, fmt.Errorf("save workspace: %w", err)
	}
	result := &MutationResult{Rev: ws.Rev, Resource: resource}
	if s.publisher != nil {
		s.publisher.Publish(Event{Type: eventType, Payload: result, WorkspaceID: s.workspaceID})
		s.publisher.Publish(Event{Type: "activity.created", Payload: event, WorkspaceID: s.workspaceID})
	}
	return result, nil
}

func validateField(key, fieldType, state string) error {
	if strings.TrimSpace(key) == "" || !fieldTypes[fieldType] {
		return validation("key and valid field type are required", nil)
	}
	if state != "" && !validState(state) {
		return validation("invalid field state", nil)
	}
	return nil
}

func validKind(v string) bool  { return v == "endpoint" || v == "database" || v == "model" }
func validState(v string) bool { return v == "draft" || v == "ready" || v == "breaking" }
func validMethod(v string) bool {
	switch strings.ToUpper(v) {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}
func shortID() string { return strings.ReplaceAll(uuid.NewString()[:8], "-", "") }
func collaborator(ws *entity.Workspace, id string) (entity.Collaborator, bool) {
	for _, c := range ws.Collaborators {
		if c.ID == id {
			return c, true
		}
	}
	return entity.Collaborator{}, false
}
func findResource(ws *entity.Workspace, id string) (*entity.Resource, int) {
	for i := range ws.Resources {
		if ws.Resources[i].ID == id {
			return &ws.Resources[i], i
		}
	}
	return nil, -1
}
func findField(r *entity.Resource, id string) *entity.SchemaField {
	field, _ := findFieldIndex(r, id)
	return field
}
func findFieldIndex(r *entity.Resource, id string) (*entity.SchemaField, int) {
	for i := range r.Fields {
		if r.Fields[i].ID == id {
			return &r.Fields[i], i
		}
	}
	return nil, -1
}
func fieldKeyExists(r *entity.Resource, key, exceptID string) bool {
	for _, field := range r.Fields {
		if field.ID != exceptID && field.Change != entity.ChangeRemoved && field.Key == strings.TrimSpace(key) {
			return true
		}
	}
	return false
}
func activity(actor entity.Collaborator, verb, target, resourceID string, at time.Time) entity.ActivityEvent {
	return entity.ActivityEvent{ID: "act_" + shortID(), Actor: actor.Name, Verb: verb, Target: target, ResourceID: resourceID, At: at}
}
func removeResourceComments(items []entity.Comment, resourceID string) []entity.Comment {
	out := items[:0]
	for _, item := range items {
		if item.ResourceID != resourceID {
			out = append(out, item)
		}
	}
	return out
}
