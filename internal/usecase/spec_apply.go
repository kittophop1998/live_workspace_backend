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
	Removed int
}

// ApplySpecEndpoints upserts spec-derived endpoints into the workspace in one
// mutation: an endpoint matching an existing resource by method+path replaces
// that resource's name/fields/responses (keeping its id, status, and the
// ids/states of fields whose keys survive, so comments and workflow state are
// not orphaned); anything else is created like ImportResources.
//
// With prune (checkout), endpoints that left the spec are also deleted — but
// only spec-managed ones (last written by actorName). An endpoint a
// collaborator created or has edited since the last sync is theirs and stays.
//
// There is no collaborator behind a CLI sync, so activity is attributed to
// actorName directly and broadcast as `resource.imported` (+
// `resource.cleared` for pruned ids) and `activity.created`.
func (s *Service) ApplySpecEndpoints(ctx context.Context, actorName string, inputs []ImportEndpointInput, prune bool) (*SpecApplyResult, error) {
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

		removedIDs := []string{}
		if prune {
			routes := make(map[string]bool, len(built))
			for _, candidate := range built {
				routes[*candidate.Method+" "+*candidate.Path] = true
			}
			kept := make([]entity.Resource, 0, len(ws.Resources))
			for _, resource := range ws.Resources {
				if resource.Kind == entity.KindEndpoint && resource.Method != nil && resource.Path != nil &&
					!routes[strings.ToUpper(*resource.Method)+" "+*resource.Path] && resource.UpdatedBy == actorName {
					removedIDs = append(removedIDs, resource.ID)
					continue
				}
				kept = append(kept, resource)
			}
			ws.Resources = kept
			for _, id := range removedIDs {
				ws.Comments = removeResourceComments(ws.Comments, id)
			}
		}

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
		if len(touched) == 0 && len(removedIDs) == 0 {
			return &SpecApplyResult{Rev: ws.Rev}, nil
		}

		target := fmt.Sprintf("%d endpoints from the API spec", len(touched))
		if len(removedIDs) > 0 {
			target = fmt.Sprintf("%s (%d removed)", target, len(removedIDs))
		}
		event := entity.ActivityEvent{ID: "act_" + shortID(), Actor: actorName, Verb: "synced", Target: target, At: now}
		// The web store applies at most one frame per rev, so an import and a
		// prune in the same save need distinct revs to both land.
		importRev := oldRev + 1
		ws.Rev = importRev
		if len(touched) > 0 && len(removedIDs) > 0 {
			ws.Rev++
		}
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
			if len(touched) > 0 {
				s.publisher.Publish(Event{Type: "resource.imported", Payload: &ImportResult{Rev: importRev, Resources: touched}, WorkspaceID: s.workspaceID})
			}
			if len(removedIDs) > 0 {
				s.publisher.Publish(Event{Type: "resource.cleared", Payload: &ClearResult{Rev: ws.Rev, ResourceIDs: removedIDs}, WorkspaceID: s.workspaceID})
			}
			s.publisher.Publish(Event{Type: "activity.created", Payload: event, WorkspaceID: s.workspaceID})
		}
		return &SpecApplyResult{Rev: ws.Rev, Created: created, Updated: updated, Removed: len(removedIDs)}, nil
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
