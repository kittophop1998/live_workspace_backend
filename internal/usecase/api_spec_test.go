package usecase

import (
	"context"
	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
	"strings"
	"testing"
)

type apiSpecRepo struct {
	current *entity.APISpecRevision
	all     map[string]*entity.APISpecRevision
}

func (r *apiSpecRepo) Publish(_ context.Context, value *entity.APISpecRevision) (*entity.APISpecRevision, bool, error) {
	if r.all == nil {
		r.all = map[string]*entity.APISpecRevision{}
	}
	for _, v := range r.all {
		if v.ContentHash == value.ContentHash {
			return v, true, nil
		}
	}
	value.Number = int64(len(r.all) + 1)
	if r.current != nil {
		r.current.Status = "superseded"
		value.PreviousRevisionID = r.current.ID
	}
	r.all[value.ID] = value
	r.current = value
	return value, false, nil
}
func (r *apiSpecRepo) Current(context.Context, string) (*entity.APISpecRevision, error) {
	if r.current == nil {
		return nil, port.ErrAPISpecNotFound
	}
	return r.current, nil
}
func (r *apiSpecRepo) Get(_ context.Context, _ string, id string) (*entity.APISpecRevision, error) {
	v := r.all[id]
	if v == nil {
		return nil, port.ErrAPISpecNotFound
	}
	return v, nil
}
func (r *apiSpecRepo) List(context.Context, string) ([]entity.APISpecRevision, error) {
	out := []entity.APISpecRevision{}
	for _, v := range r.all {
		out = append(out, *v)
	}
	return out, nil
}
type capturePublisher struct{ events []Event }

func (p *capturePublisher) Publish(e Event) { p.events = append(p.events, e) }

func TestAPISpecPublishValidatesAndPreventsDuplicates(t *testing.T) {
	repo := &apiSpecRepo{}
	publisher := &capturePublisher{}
	service := NewAPISpecService(repo, publisher, nil)
	input := PublishAPISpecInput{SourceFilename: "openapi.yaml", Format: "yaml", Content: "openapi: 3.1.0\ninfo: {title: Test, version: v1}\npaths: {}\n"}
	first, err := service.Publish(context.Background(), "prj_test", input)
	if err != nil || first.Unchanged || first.Revision.Number != 1 {
		t.Fatalf("first publish = %#v err=%v", first, err)
	}
	if len(publisher.events) != 1 || publisher.events[0].Type != "api_spec.published" || publisher.events[0].WorkspaceID != "prj_test" {
		t.Fatalf("expected one api_spec.published event for the workspace, got %#v", publisher.events)
	}
	second, err := service.Publish(context.Background(), "prj_test", input)
	if err != nil || !second.Unchanged || second.Revision.ID != first.Revision.ID {
		t.Fatalf("duplicate publish = %#v err=%v", second, err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("duplicate publish must not broadcast, got %d events", len(publisher.events))
	}
	if second.Workspace != nil {
		t.Fatalf("duplicate publish must not re-apply to the workspace, got %#v", second.Workspace)
	}
}
func TestAPISpecRejectsNonOpenAPI(t *testing.T) {
	_, err := NewAPISpecService(&apiSpecRepo{}, nil, nil).Publish(context.Background(), "prj_test", PublishAPISpecInput{SourceFilename: "x.yaml", Format: "yaml", Content: "title: not-openapi"})
	if err == nil {
		t.Fatal("expected OpenAPI validation error")
	}
}

const publishSpecWithEndpoints = `openapi: 3.1.0
info: {title: Test, version: v1}
paths:
  /api/v1/users:
    post:
      operationId: createUser
      requestBody:
        content:
          application/json:
            schema:
              type: object
              required: [email]
              properties:
                email: {type: string}
                age: {type: integer}
      responses:
        "201":
          description: Created
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: {type: string, format: uuid}
`

// Publishing a new revision must land the parsed endpoints in the workspace
// (created on first sync, updated in place on the next) and broadcast
// `resource.imported` so open web clients follow along.
func TestAPISpecPublishAppliesEndpointsToWorkspace(t *testing.T) {
	workspaceRepo := &fakeWorkspaceRepository{workspace: &entity.Workspace{ID: "prj_test"}}
	publisher := &capturePublisher{}
	workspace := NewService(workspaceRepo, nil, nil, "prj_test", publisher)
	service := NewAPISpecService(&apiSpecRepo{}, publisher, workspace)

	published, err := service.Publish(context.Background(), "prj_test", PublishAPISpecInput{SourceFilename: "openapi.yaml", Format: "yaml", Content: publishSpecWithEndpoints})
	if err != nil {
		t.Fatalf("publish returned error: %v", err)
	}
	if published.Workspace == nil || !published.Workspace.Applied || published.Workspace.Created != 1 || published.Workspace.Updated != 0 {
		t.Fatalf("workspace summary = %#v", published.Workspace)
	}
	ws := workspaceRepo.workspace
	if len(ws.Resources) != 1 || ws.Resources[0].Name != "createUser" || *ws.Resources[0].Method != "POST" || *ws.Resources[0].Path != "/api/v1/users" {
		t.Fatalf("resources = %#v", ws.Resources)
	}
	if len(ws.Resources[0].Fields) != 2 || len(ws.Resources[0].Responses) != 1 {
		t.Fatalf("resource shape = %#v", ws.Resources[0])
	}
	types := map[string]string{"api_spec.published": "", "resource.imported": "", "activity.created": ""}
	for _, event := range publisher.events {
		if _, ok := types[event.Type]; ok {
			delete(types, event.Type)
		}
	}
	if len(types) != 0 {
		t.Fatalf("missing broadcasts %v in %#v", types, publisher.events)
	}

	// Second revision with the same route updates the endpoint in place,
	// keeping its resource id and field ids for surviving keys.
	originalID := ws.Resources[0].ID
	emailID := ws.Resources[0].Fields[0].ID
	updatedSpec := strings.Replace(publishSpecWithEndpoints, "age: {type: integer}", "age: {type: integer}\n                nickname: {type: string}", 1)
	second, err := service.Publish(context.Background(), "prj_test", PublishAPISpecInput{SourceFilename: "openapi.yaml", Format: "yaml", Content: updatedSpec})
	if err != nil {
		t.Fatalf("second publish returned error: %v", err)
	}
	if second.Workspace == nil || second.Workspace.Created != 0 || second.Workspace.Updated != 1 {
		t.Fatalf("second workspace summary = %#v", second.Workspace)
	}
	ws = workspaceRepo.workspace
	if len(ws.Resources) != 1 || ws.Resources[0].ID != originalID {
		t.Fatalf("update must keep the resource id, got %#v", ws.Resources)
	}
	if len(ws.Resources[0].Fields) != 3 || ws.Resources[0].Fields[0].Key != "email" || ws.Resources[0].Fields[0].ID != emailID {
		t.Fatalf("update must keep surviving field ids, got %#v", ws.Resources[0].Fields)
	}
}
