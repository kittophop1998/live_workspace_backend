package usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
)

type fakeRepository struct {
	workspace *entity.Workspace
	conflict  bool
	saveCount int
}

func (f *fakeRepository) Get(context.Context, string) (*entity.Workspace, error) {
	copy := *f.workspace
	copy.Resources = append([]entity.Resource(nil), f.workspace.Resources...)
	for i := range copy.Resources {
		copy.Resources[i].Fields = append([]entity.SchemaField(nil), f.workspace.Resources[i].Fields...)
	}
	copy.Comments = append([]entity.Comment(nil), f.workspace.Comments...)
	copy.Activity = append([]entity.ActivityEvent(nil), f.workspace.Activity...)
	return &copy, nil
}
func (f *fakeRepository) CreateIfAbsent(context.Context, *entity.Workspace) error { return nil }
func (f *fakeRepository) Create(context.Context, *entity.Workspace) error         { return nil }
func (f *fakeRepository) Save(_ context.Context, ws *entity.Workspace, expected int64) error {
	if f.conflict || f.workspace.Rev != expected {
		return port.ErrRevisionConflict
	}
	f.saveCount++
	f.workspace = ws
	return nil
}

type recordingPublisher struct {
	events []Event
}

func (p *recordingPublisher) Publish(event Event) {
	p.events = append(p.events, event)
}

func newTestService(fields ...entity.SchemaField) (*Service, *fakeRepository) {
	repository := &fakeRepository{workspace: &entity.Workspace{
		ID: "wsp_test", Rev: 3,
		Collaborators: []entity.Collaborator{{ID: "col_test", Name: "Tester", Role: entity.RoleBackend}},
		Resources:     []entity.Resource{{ID: "res_test", Name: "User", Kind: entity.KindModel, Fields: fields}},
	}}
	service := NewService(repository, "wsp_test", nil)
	service.now = func() time.Time { return time.Date(2026, 6, 30, 8, 0, 0, 0, time.UTC) }
	return service, repository
}

func TestAddFieldRejectsDuplicateKey(t *testing.T) {
	service, repository := newTestService(entity.SchemaField{ID: "fld_name", Key: "name", Type: "string", State: entity.StateReady, Change: entity.ChangeStable})

	_, err := service.AddField(context.Background(), "col_test", "res_test", nil, FieldInput{Key: "name", Type: "string"})

	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if repository.workspace.Rev != 3 {
		t.Fatalf("failed mutation changed revision to %d", repository.workspace.Rev)
	}
}

func TestUpdateFieldRollsUpBreakingAndMarksStableModified(t *testing.T) {
	service, repository := newTestService(entity.SchemaField{ID: "fld_name", Key: "name", Type: "string", State: entity.StateReady, Change: entity.ChangeStable})
	state := "breaking"

	result, err := service.UpdateField(context.Background(), "col_test", "res_test", "fld_name", nil, UpdateFieldInput{State: &state})

	if err != nil {
		t.Fatal(err)
	}
	field := repository.workspace.Resources[0].Fields[0]
	if field.Change != entity.ChangeModified || result.Resource.State != entity.StateBreaking {
		t.Fatalf("unexpected field change/state: %s/%s", field.Change, result.Resource.State)
	}
	if result.Rev != 4 {
		t.Fatalf("expected rev 4, got %d", result.Rev)
	}
}

func TestUpdateJSONFieldStoresValue(t *testing.T) {
	service, repository := newTestService(entity.SchemaField{ID: "fld_meta", Key: "meta", Type: "json", State: entity.StateReady, Change: entity.ChangeStable})
	value := any(map[string]any{"profile": map[string]any{"active": true}, "tags": []any{"new"}})

	_, err := service.UpdateField(context.Background(), "col_test", "res_test", "fld_meta", nil, UpdateFieldInput{Value: &value})

	if err != nil {
		t.Fatal(err)
	}
	field := repository.workspace.Resources[0].Fields[0]
	if !reflect.DeepEqual(field.Value, value) {
		t.Fatalf("unexpected JSON value: %#v", field.Value)
	}
	if field.Change != entity.ChangeModified {
		t.Fatalf("expected modified change, got %s", field.Change)
	}
}

func TestUpdateFieldRejectsValueForNonJSONType(t *testing.T) {
	service, repository := newTestService(entity.SchemaField{ID: "fld_name", Key: "name", Type: "string", State: entity.StateReady, Change: entity.ChangeStable})
	value := any(map[string]any{"invalid": true})

	_, err := service.UpdateField(context.Background(), "col_test", "res_test", "fld_name", nil, UpdateFieldInput{Value: &value})

	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if repository.workspace.Rev != 3 {
		t.Fatalf("failed mutation changed revision to %d", repository.workspace.Rev)
	}
}

func TestChangingJSONFieldTypeClearsValue(t *testing.T) {
	service, repository := newTestService(entity.SchemaField{
		ID: "fld_meta", Key: "meta", Type: "json", State: entity.StateReady,
		Change: entity.ChangeStable, Value: map[string]any{"active": true},
	})
	fieldType := "string"

	_, err := service.UpdateField(context.Background(), "col_test", "res_test", "fld_meta", nil, UpdateFieldInput{Type: &fieldType})

	if err != nil {
		t.Fatal(err)
	}
	if repository.workspace.Resources[0].Fields[0].Value != nil {
		t.Fatalf("expected value to be cleared, got %#v", repository.workspace.Resources[0].Fields[0].Value)
	}
}

func TestDeleteFieldUsesSoftDeleteForShippedField(t *testing.T) {
	service, repository := newTestService(entity.SchemaField{ID: "fld_name", Key: "name", Type: "string", State: entity.StateReady, Change: entity.ChangeStable})

	_, err := service.DeleteField(context.Background(), "col_test", "res_test", "fld_name", nil)

	if err != nil {
		t.Fatal(err)
	}
	field := repository.workspace.Resources[0].Fields[0]
	if field.Change != entity.ChangeRemoved || field.State != entity.StateBreaking {
		t.Fatalf("expected removed/breaking, got %s/%s", field.Change, field.State)
	}
}

func TestMutationRejectsStaleRevision(t *testing.T) {
	service, repository := newTestService()
	stale := int64(2)

	_, err := service.AddField(context.Background(), "col_test", "res_test", &stale, FieldInput{Key: "name", Type: "string"})

	if !errors.Is(err, ErrRevConflict) {
		t.Fatalf("expected revision conflict, got %v", err)
	}
	if repository.workspace.Rev != 3 {
		t.Fatalf("conflict changed revision to %d", repository.workspace.Rev)
	}
}

func TestCreateEndpointUsesEditableDefaults(t *testing.T) {
	service, repository := newTestService()

	result, err := service.CreateResource(context.Background(), "col_test", nil, CreateResourceInput{
		Name: "New endpoint",
		Kind: string(entity.KindEndpoint),
	})

	if err != nil {
		t.Fatal(err)
	}
	if result.Resource.Method == nil || *result.Resource.Method != "GET" {
		t.Fatalf("expected default method GET, got %v", result.Resource.Method)
	}
	if result.Resource.Path == nil || *result.Resource.Path != "/api/v1/new" {
		t.Fatalf("expected default path /api/v1/new, got %v", result.Resource.Path)
	}
	if result.Resource.Status == nil || *result.Resource.Status != entity.EndpointStatusDraft {
		t.Fatalf("expected default status draft, got %v", result.Resource.Status)
	}
	if repository.workspace.Resources[1].ID != result.Resource.ID {
		t.Fatal("created endpoint was not persisted")
	}
}

func TestUpdateEndpointStatusAndFilterResources(t *testing.T) {
	service, repository := newTestService()
	draft := entity.EndpointStatusDraft
	repository.workspace.Resources = append(repository.workspace.Resources,
		entity.Resource{ID: "res_draft", Name: "Draft endpoint", Kind: entity.KindEndpoint, Status: &draft},
		entity.Resource{ID: "res_done", Name: "Done endpoint", Kind: entity.KindEndpoint, Status: &draft},
	)
	done := "done"

	if _, err := service.UpdateResource(context.Background(), "col_test", "res_done", nil, UpdateResourceInput{Status: &done}); err != nil {
		t.Fatal(err)
	}
	items, err := service.Resources(context.Background(), "", "done")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "res_done" {
		t.Fatalf("unexpected filtered resources: %+v", items)
	}
}

func TestUpdateResourceRejectsStatusForNonEndpoint(t *testing.T) {
	service, repository := newTestService()
	done := "done"

	_, err := service.UpdateResource(context.Background(), "col_test", "res_test", nil, UpdateResourceInput{Status: &done})

	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if repository.workspace.Rev != 3 {
		t.Fatalf("failed mutation changed revision to %d", repository.workspace.Rev)
	}
}

func TestResourcesRejectsInvalidStatus(t *testing.T) {
	service, _ := newTestService()

	_, err := service.Resources(context.Background(), "", "blocked")

	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestDeleteAllResourcesClearsResourcesAndComments(t *testing.T) {
	service, repository := newTestService()
	repository.workspace.Resources = append(repository.workspace.Resources,
		entity.Resource{ID: "res_second", Name: "Order", Kind: entity.KindModel},
	)
	repository.workspace.Comments = []entity.Comment{
		{ID: "cmt_first", ResourceID: "res_test"},
		{ID: "cmt_second", ResourceID: "res_second"},
	}
	publisher := &recordingPublisher{}
	service.publisher = publisher

	result, err := service.DeleteAllResources(context.Background(), "col_test", nil)

	if err != nil {
		t.Fatal(err)
	}
	if result.Rev != 4 || !reflect.DeepEqual(result.ResourceIDs, []string{"res_test", "res_second"}) {
		t.Fatalf("unexpected clear result: %+v", result)
	}
	if len(repository.workspace.Resources) != 0 || len(repository.workspace.Comments) != 0 {
		t.Fatalf("workspace was not cleared: resources=%d comments=%d", len(repository.workspace.Resources), len(repository.workspace.Comments))
	}
	if repository.saveCount != 1 {
		t.Fatalf("expected one save, got %d", repository.saveCount)
	}
	if len(repository.workspace.Activity) != 1 || repository.workspace.Activity[0].Verb != "cleared" || repository.workspace.Activity[0].Target != "all resources" {
		t.Fatalf("unexpected activity: %+v", repository.workspace.Activity)
	}
	if len(publisher.events) != 2 || publisher.events[0].Type != "resource.cleared" || publisher.events[1].Type != "activity.created" {
		t.Fatalf("unexpected published events: %+v", publisher.events)
	}
}

func TestDeleteAllResourcesEmptyWorkspaceIsNoOp(t *testing.T) {
	service, repository := newTestService()
	repository.workspace.Resources = []entity.Resource{}
	publisher := &recordingPublisher{}
	service.publisher = publisher

	result, err := service.DeleteAllResources(context.Background(), "col_test", nil)

	if err != nil {
		t.Fatal(err)
	}
	if result.Rev != 3 || len(result.ResourceIDs) != 0 {
		t.Fatalf("unexpected no-op result: %+v", result)
	}
	if repository.saveCount != 0 {
		t.Fatalf("empty clear saved workspace %d times", repository.saveCount)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("empty clear published events: %+v", publisher.events)
	}
}

func TestReplaceResponsesPersistsAndPublishesResourceUpdate(t *testing.T) {
	service, repository := newTestService()
	draft := entity.EndpointStatusDraft
	repository.workspace.Resources[0].Kind = entity.KindEndpoint
	repository.workspace.Resources[0].Status = &draft
	publisher := &recordingPublisher{}
	service.publisher = publisher
	description := "OK"

	result, err := service.ReplaceResponses(context.Background(), "col_test", "res_test", nil, []ResponseSchemaInput{{
		Status: 200, Description: &description,
		Fields: []ResponseFieldInput{{
			ID: "fld_user_id", Key: "id", Type: "uuid", Required: true,
			State: "ready", Change: "added",
		}},
	}})

	if err != nil {
		t.Fatal(err)
	}
	if result.Rev != 4 || len(result.Resource.Responses) != 1 || result.Resource.Responses[0].Status != 200 {
		t.Fatalf("unexpected mutation result: %+v", result)
	}
	if result.Resource.UpdatedBy != "Tester" {
		t.Fatalf("updated_by = %q", result.Resource.UpdatedBy)
	}
	if len(repository.workspace.Activity) != 1 || repository.workspace.Activity[0].Target != "responses" {
		t.Fatalf("unexpected activity: %+v", repository.workspace.Activity)
	}
	if len(publisher.events) != 2 || publisher.events[0].Type != "resource.updated" {
		t.Fatalf("unexpected events: %+v", publisher.events)
	}
}

func TestReplaceResponsesAllowsEmptyArray(t *testing.T) {
	service, repository := newTestService()
	repository.workspace.Resources[0].Kind = entity.KindEndpoint
	repository.workspace.Resources[0].Responses = []entity.ResponseSchema{{Status: 200}}

	result, err := service.ReplaceResponses(context.Background(), "col_test", "res_test", nil, []ResponseSchemaInput{})

	if err != nil {
		t.Fatal(err)
	}
	if result.Resource.Responses == nil || len(result.Resource.Responses) != 0 {
		t.Fatalf("expected non-nil empty responses, got %#v", result.Resource.Responses)
	}
}

func TestReplaceResponsesRejectsNonEndpointAndDuplicateStatus(t *testing.T) {
	service, repository := newTestService()

	_, err := service.ReplaceResponses(context.Background(), "col_test", "res_test", nil, []ResponseSchemaInput{})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected non-endpoint validation error, got %v", err)
	}

	repository.workspace.Resources[0].Kind = entity.KindEndpoint
	_, err = service.ReplaceResponses(context.Background(), "col_test", "res_test", nil, []ResponseSchemaInput{{Status: 200}, {Status: 200}})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected duplicate status validation error, got %v", err)
	}
	if repository.workspace.Rev != 3 {
		t.Fatalf("failed mutations changed revision to %d", repository.workspace.Rev)
	}
}
