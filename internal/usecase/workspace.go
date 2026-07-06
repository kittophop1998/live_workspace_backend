package usecase

import (
	"context"
	"encoding/json"
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
	Name, Method, Path, Status *string
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
	Value            *any
}

type ResponseFieldInput struct {
	ID          string
	Key         string
	Type        string
	Required    bool
	State       string
	Change      string
	Description *string
	Value       any
}

type ResponseSchemaInput struct {
	Status      int
	Description *string
	Fields      []ResponseFieldInput
}

type MutationResult struct {
	Rev      int64
	Resource *entity.Resource
	Comment  *entity.Comment
}

func (s *Service) Resources(ctx context.Context, kind, status string) ([]entity.Resource, error) {
	ws, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if kind != "" && !validKind(kind) {
		return nil, validation("invalid resource kind", map[string]any{"kind": kind})
	}
	if status != "" && !validEndpointStatus(status) {
		return nil, validation("invalid endpoint status", map[string]any{"status": status})
	}
	out := make([]entity.Resource, 0)
	for _, resource := range ws.Resources {
		if kind != "" && string(resource.Kind) != kind {
			continue
		}
		if status != "" && (resource.Kind != entity.KindEndpoint || resource.Status == nil || string(*resource.Status) != status) {
			continue
		}
		out = append(out, resource)
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
		if in.Method == "" {
			in.Method = "GET"
		}
		if in.Path == "" {
			in.Path = "/api/v1/new"
		}
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
			State: entity.StateDraft, Fields: []entity.SchemaField{},
			UpdatedAt: now, UpdatedBy: actor.Name,
		}
		if in.Kind == string(entity.KindEndpoint) {
			method, path := strings.ToUpper(in.Method), in.Path
			resource.Method, resource.Path = &method, &path
			status := entity.EndpointStatusDraft
			resource.Status = &status
			resource.Responses = []entity.ResponseSchema{}
		} else {
			resource.Fields = append(resource.Fields, entity.SchemaField{
				ID: "fld_" + shortID(), Key: "id", Type: "uuid", Required: true,
				State: entity.StateDraft, Change: entity.ChangeAdded,
			})
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
		if in.Status != nil {
			if resource.Kind != entity.KindEndpoint || !validEndpointStatus(*in.Status) {
				return nil, entity.ActivityEvent{}, validation("invalid endpoint status", map[string]any{"status": *in.Status})
			}
			value := entity.EndpointStatus(*in.Status)
			resource.Status = &value
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

func (s *Service) ReplaceResponses(ctx context.Context, actorID, resourceID string, expected *int64, inputs []ResponseSchemaInput) (*MutationResult, error) {
	responses, err := responseSchemas(inputs)
	if err != nil {
		return nil, err
	}
	return s.mutateResource(ctx, actorID, expected, "resource.updated", func(ws *entity.Workspace, actor entity.Collaborator) (*entity.Resource, entity.ActivityEvent, error) {
		resource, _ := findResource(ws, resourceID)
		if resource == nil {
			return nil, entity.ActivityEvent{}, notFound("resource", resourceID)
		}
		if resource.Kind != entity.KindEndpoint {
			return nil, entity.ActivityEvent{}, validation("response schemas are only valid for endpoints", nil)
		}
		resource.Responses = responses
		resource.UpdatedAt, resource.UpdatedBy = s.now(), actor.Name
		return resource, activity(actor, "edited", "responses", resourceID, resource.UpdatedAt), nil
	})
}

// ClearResult reports a bulk "delete all resources" mutation.
type ClearResult struct {
	Rev         int64
	ResourceIDs []string
}

// DeleteAllResources removes every resource (and its comments) in the room in a
// single rev-bumping mutation. Used by the "Import API" flow, which wipes the
// workspace before recreating endpoints from a spec. A no-op on an empty
// workspace (returns the current rev without bumping or broadcasting).
func (s *Service) DeleteAllResources(ctx context.Context, actorID string, expected *int64) (*ClearResult, error) {
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
	ids := make([]string, 0, len(ws.Resources))
	for _, resource := range ws.Resources {
		ids = append(ids, resource.ID)
	}
	if len(ids) == 0 {
		return &ClearResult{Rev: ws.Rev, ResourceIDs: ids}, nil
	}
	oldRev := ws.Rev
	ws.Resources = []entity.Resource{}
	ws.Comments = []entity.Comment{}
	event := activity(actor, "cleared", "all resources", "", s.now())
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
	result := &ClearResult{Rev: ws.Rev, ResourceIDs: ids}
	if s.publisher != nil {
		s.publisher.Publish(Event{Type: "resource.cleared", Payload: result, WorkspaceID: s.workspaceID})
		s.publisher.Publish(Event{Type: "activity.created", Payload: event, WorkspaceID: s.workspaceID})
	}
	return result, nil
}

// ImportEndpointInput is one endpoint from a parsed OpenAPI/Postman spec: its
// request body fields and per-status response schemas, ready to persist.
type ImportEndpointInput struct {
	Name      string
	Method    string
	Path      string
	Fields    []ImportFieldInput
	Responses []ResponseSchemaInput
}

// ImportFieldInput is a request-body field on an imported endpoint. Unlike
// AddField the backend assigns the id; `Value` is kept for json fields.
type ImportFieldInput struct {
	Key         string
	Type        string
	Required    bool
	Description *string
	Value       any
}

// ImportResult reports a bulk endpoint import (one rev-bumping mutation).
type ImportResult struct {
	Rev       int64
	Resources []entity.Resource
}

// ImportResources creates many endpoints from a spec in a SINGLE rev-bumping
// mutation (one save, one broadcast). It replaces the old client-side loop of
// create-then-N-field-calls, which dropped endpoints on any transient failure
// mid-batch. Everything is validated and built up front, so a bad item fails the
// whole import atomically — nothing is persisted. Endpoints are created WITHOUT
// the default seeded `id` field so they mirror the spec exactly.
func (s *Service) ImportResources(ctx context.Context, actorID string, expected *int64, inputs []ImportEndpointInput) (*ImportResult, error) {
	if len(inputs) == 0 {
		return nil, validation("no endpoints to import", nil)
	}
	now := s.now()
	built := make([]entity.Resource, len(inputs))
	for i, in := range inputs {
		if strings.TrimSpace(in.Name) == "" {
			return nil, validation("endpoint name is required", map[string]any{"index": i})
		}
		method := strings.ToUpper(strings.TrimSpace(in.Method))
		if method == "" {
			method = "GET"
		}
		path := in.Path
		if path == "" {
			path = "/api/v1/new"
		}
		if !validMethod(method) || !strings.HasPrefix(path, "/") {
			return nil, validation("endpoint requires a valid method and absolute path", map[string]any{"index": i, "name": in.Name})
		}
		fields, err := importFields(in.Fields)
		if err != nil {
			return nil, err
		}
		responses, err := responseSchemas(in.Responses)
		if err != nil {
			return nil, err
		}
		status := entity.EndpointStatusDraft
		resource := entity.Resource{
			ID: "res_" + shortID(), Name: strings.TrimSpace(in.Name), Kind: entity.KindEndpoint,
			Method: &method, Path: &path, Status: &status, Fields: fields, Responses: responses, UpdatedAt: now,
		}
		resource.RollupState()
		built[i] = resource
	}

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
	for i := range built {
		built[i].UpdatedBy = actor.Name
	}
	oldRev := ws.Rev
	ws.Resources = append(ws.Resources, built...)
	event := activity(actor, "imported", fmt.Sprintf("%d endpoints", len(built)), "", now)
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
	result := &ImportResult{Rev: ws.Rev, Resources: append([]entity.Resource(nil), built...)}
	if s.publisher != nil {
		s.publisher.Publish(Event{Type: "resource.imported", Payload: result, WorkspaceID: s.workspaceID})
		s.publisher.Publish(Event{Type: "activity.created", Payload: event, WorkspaceID: s.workspaceID})
	}
	return result, nil
}

// importFields validates + builds request-body fields for an imported endpoint.
// Mirrors AddField (state draft, change added) but assigns ids and keeps json
// values. Rejects duplicate keys within the same endpoint.
func importFields(inputs []ImportFieldInput) ([]entity.SchemaField, error) {
	fields := make([]entity.SchemaField, 0, len(inputs))
	keys := make(map[string]struct{}, len(inputs))
	for _, in := range inputs {
		key := strings.TrimSpace(in.Key)
		if key == "" || !fieldTypes[in.Type] {
			return nil, validation("import field requires a key and valid type", map[string]any{"key": in.Key})
		}
		if _, dup := keys[key]; dup {
			return nil, validation("duplicate field key", map[string]any{"key": key})
		}
		keys[key] = struct{}{}
		if in.Type != "json" && in.Value != nil {
			return nil, validation("value is only valid for json fields", map[string]any{"key": key})
		}
		if _, err := json.Marshal(in.Value); err != nil {
			return nil, validation("value must be valid JSON", map[string]any{"key": key})
		}
		fields = append(fields, entity.SchemaField{
			ID: "fld_" + shortID(), Key: key, Type: in.Type, Required: in.Required,
			State: entity.StateDraft, Change: entity.ChangeAdded, Description: in.Description, Value: in.Value,
		})
	}
	return fields, nil
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
		}
		resultingType := field.Type
		if in.Type != nil {
			resultingType = *in.Type
		}
		if in.Value != nil {
			if resultingType != "json" {
				return nil, entity.ActivityEvent{}, validation("value is only valid for json fields", nil)
			}
			if _, err := json.Marshal(*in.Value); err != nil {
				return nil, entity.ActivityEvent{}, validation("value must be valid JSON", nil)
			}
		}
		field.Type = resultingType
		if field.Type != "json" {
			field.Value = nil
		} else if in.Value != nil {
			field.Value = *in.Value
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

func responseSchemas(inputs []ResponseSchemaInput) ([]entity.ResponseSchema, error) {
	out := make([]entity.ResponseSchema, len(inputs))
	statuses := make(map[int]struct{}, len(inputs))
	for i, input := range inputs {
		if input.Status != 0 && (input.Status < 100 || input.Status > 599) {
			return nil, validation("invalid response status", map[string]any{"status": input.Status})
		}
		if _, exists := statuses[input.Status]; exists {
			return nil, validation("duplicate response status", map[string]any{"status": input.Status})
		}
		statuses[input.Status] = struct{}{}
		fields := make([]entity.SchemaField, len(input.Fields))
		keys := make(map[string]struct{}, len(input.Fields))
		for j, field := range input.Fields {
			key := strings.TrimSpace(field.Key)
			if strings.TrimSpace(field.ID) == "" || key == "" || !fieldTypes[field.Type] || !validState(field.State) || !validChange(field.Change) {
				return nil, validation("invalid response field", map[string]any{"status": input.Status, "key": field.Key})
			}
			if _, exists := keys[key]; exists {
				return nil, validation("duplicate response field key", map[string]any{"status": input.Status, "key": key})
			}
			keys[key] = struct{}{}
			if field.Type != "json" && field.Value != nil {
				return nil, validation("value is only valid for json fields", map[string]any{"status": input.Status, "key": key})
			}
			if _, err := json.Marshal(field.Value); err != nil {
				return nil, validation("value must be valid JSON", map[string]any{"status": input.Status, "key": key})
			}
			fields[j] = entity.SchemaField{
				ID: strings.TrimSpace(field.ID), Key: key, Type: field.Type, Required: field.Required,
				State: entity.FieldState(field.State), Change: entity.FieldChange(field.Change),
				Description: field.Description, Value: field.Value,
			}
		}
		out[i] = entity.ResponseSchema{Status: input.Status, Description: input.Description, Fields: fields}
	}
	return out, nil
}

func validKind(v string) bool  { return v == "endpoint" || v == "database" || v == "model" }
func validState(v string) bool { return v == "draft" || v == "ready" || v == "breaking" }
func validChange(v string) bool {
	return v == "stable" || v == "added" || v == "modified" || v == "removed"
}
func validEndpointStatus(v string) bool {
	switch v {
	case "draft", "inprogress", "testing", "done":
		return true
	default:
		return false
	}
}
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
