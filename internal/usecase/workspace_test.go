package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
)

type fakeRepository struct {
	workspace *entity.Workspace
	conflict  bool
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
func (f *fakeRepository) Save(_ context.Context, ws *entity.Workspace, expected int64) error {
	if f.conflict || f.workspace.Rev != expected {
		return port.ErrRevisionConflict
	}
	f.workspace = ws
	return nil
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
