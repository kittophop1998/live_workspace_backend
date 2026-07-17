package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
)

// SpecApplyResult reports how a published API spec landed in the workspace.
type SpecApplyResult struct {
	Rev     int64
	Created int
	Updated int
}

// ApplySpecEndpoints upserts spec-derived endpoints into the workspace in one
// rev-bumping mutation: an endpoint matching an existing resource by
// method+path replaces that resource's name/fields/responses (keeping its id,
// status, and the ids/states of fields whose keys survive, so comments and
// workflow state are not orphaned); anything else is created like
// ImportResources. Resources the spec no longer mentions are left alone.
// There is no collaborator behind a CLI sync, so activity is attributed to
// actorName directly and broadcast as `resource.imported` + `activity.created`.
func (s *Service) ApplySpecEndpoints(ctx context.Context, actorName string, inputs []ImportEndpointInput) (*SpecApplyResult, error) {
	built := make([]entity.Resource, len(inputs))
	for i, in := range inputs {
		if strings.TrimSpace(in.Name) == "" {
			return nil, validation("endpoint name is required", map[string]any{"index": i})
		}
		method := strings.ToUpper(strings.TrimSpace(in.Method))
		if !validMethod(method) || !strings.HasPrefix(in.Path, "/") {
			return nil, validation("endpoint requires a valid method and absolute path", map[string]any{"index": i, "name": in.Name})
		}
		fields, err := importFields(in.Fields)
		if err != nil {
			return nil, err
		}
		responses, err := responseSchemas(in.Responses, responseSchemaOptions{
			generateMissingFieldIDs: true,
			defaultState:            entity.StateDraft,
			defaultChange:           entity.ChangeAdded,
		})
		if err != nil {
			return nil, err
		}
		path := in.Path
		status := entity.EndpointStatusDraft
		resource := entity.Resource{
			ID: "res_" + shortID(), Name: strings.TrimSpace(in.Name), Kind: entity.KindEndpoint,
			Method: &method, Path: &path, Status: &status, Fields: fields, Responses: responses,
		}
		resource.RollupState()
		built[i] = resource
	}

	for range 5 {
		ws, err := s.Snapshot(ctx)
		if err != nil {
			if errors.Is(err, port.ErrRevisionConflict) {
				continue
			}
			return nil, err
		}
		now := s.now()
		oldRev := ws.Rev
		created, updated := 0, 0
		touched := make([]entity.Resource, 0, len(built))
		for _, candidate := range built {
			existing := findEndpointByRoute(ws, *candidate.Method, *candidate.Path)
			if existing != nil {
				mergeSpecEndpoint(existing, candidate)
				existing.RollupState()
				existing.UpdatedAt, existing.UpdatedBy = now, actorName
				updated++
				touched = append(touched, *existing)
				continue
			}
			candidate.UpdatedAt, candidate.UpdatedBy = now, actorName
			ws.Resources = append(ws.Resources, candidate)
			created++
			touched = append(touched, candidate)
		}
		if len(touched) == 0 {
			return &SpecApplyResult{Rev: ws.Rev}, nil
		}
		event := entity.ActivityEvent{
			ID: "act_" + shortID(), Actor: actorName, Verb: "synced",
			Target: fmt.Sprintf("%d endpoints from the API spec", len(touched)), At: now,
		}
		ws.Rev++
		ws.Activity = append(ws.Activity, event)
		if len(ws.Activity) > 1000 {
			ws.Activity = ws.Activity[len(ws.Activity)-1000:]
		}
		if err := s.repo.Save(ctx, ws, oldRev); err != nil {
			if errors.Is(err, port.ErrRevisionConflict) {
				continue
			}
			return nil, fmt.Errorf("save workspace: %w", err)
		}
		if s.publisher != nil {
			s.publisher.Publish(Event{Type: "resource.imported", Payload: &ImportResult{Rev: ws.Rev, Resources: touched}, WorkspaceID: s.workspaceID})
			s.publisher.Publish(Event{Type: "activity.created", Payload: event, WorkspaceID: s.workspaceID})
		}
		return &SpecApplyResult{Rev: ws.Rev, Created: created, Updated: updated}, nil
	}
	return nil, &Error{Kind: ErrRevConflict, Message: "workspace was changed by another client"}
}

func findEndpointByRoute(ws *entity.Workspace, method, path string) *entity.Resource {
	for i := range ws.Resources {
		resource := &ws.Resources[i]
		if resource.Kind != entity.KindEndpoint || resource.Method == nil || resource.Path == nil {
			continue
		}
		if strings.EqualFold(*resource.Method, method) && *resource.Path == path {
			return resource
		}
	}
	return nil
}

// mergeSpecEndpoint overwrites an endpoint with its spec-derived counterpart.
// The spec is the source of truth for name/fields/responses, but ids carry the
// workspace's history: the resource id (comments reference it), each surviving
// field's id + review state, and the endpoint's workflow status all stay.
func mergeSpecEndpoint(existing *entity.Resource, incoming entity.Resource) {
	existing.Name = incoming.Name
	previous := make(map[string]entity.SchemaField, len(existing.Fields))
	for _, field := range existing.Fields {
		previous[field.Key] = field
	}
	fields := make([]entity.SchemaField, len(incoming.Fields))
	for i, field := range incoming.Fields {
		if old, ok := previous[field.Key]; ok {
			field.ID = old.ID
			field.State = old.State
			if old.Type == field.Type && old.Required == field.Required {
				field.Change = old.Change
			} else {
				field.Change = entity.ChangeModified
			}
		}
		fields[i] = field
	}
	existing.Fields = fields
	existing.Responses = incoming.Responses
}
