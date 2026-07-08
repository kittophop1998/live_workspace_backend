package usecase

import (
	"context"
	"strings"
	"testing"
	"time"

	"kingdom_manager/backend/internal/domain/entity"
)

type fakeWorkspaceRepository struct {
	workspace *entity.Workspace
}

func (f *fakeWorkspaceRepository) Get(context.Context, string) (*entity.Workspace, error) {
	return f.workspace, nil
}

func (f *fakeWorkspaceRepository) Create(context.Context, *entity.Workspace) error {
	return nil
}

func (f *fakeWorkspaceRepository) CreateIfAbsent(context.Context, *entity.Workspace) error {
	return nil
}

func (f *fakeWorkspaceRepository) Save(_ context.Context, workspace *entity.Workspace, _ int64) error {
	f.workspace = workspace
	return nil
}

func TestImportResourcesDefaultsResponseFieldMetadata(t *testing.T) {
	repo := &fakeWorkspaceRepository{workspace: &entity.Workspace{
		ID: "room_1",
		Collaborators: []entity.Collaborator{{
			ID:   "col_1",
			Name: "Ava",
			Role: entity.RoleBackend,
		}},
	}}
	service := NewService(repo, "room_1", nil)
	now := time.Date(2026, 7, 8, 9, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	result, err := service.ImportResources(context.Background(), "col_1", nil, []ImportEndpointInput{{
		Name:   "listUsers",
		Method: "GET",
		Path:   "/api/v1/users",
		Responses: []ResponseSchemaInput{{
			Status: 200,
			Fields: []ResponseFieldInput{{
				Key:  "users",
				Type: "json",
			}},
		}},
	}})
	if err != nil {
		t.Fatalf("ImportResources returned error: %v", err)
	}
	if result.Rev != 1 {
		t.Fatalf("Rev = %d, want 1", result.Rev)
	}
	field := result.Resources[0].Responses[0].Fields[0]
	if !strings.HasPrefix(field.ID, "fld_") {
		t.Fatalf("field ID = %q, want generated fld_ prefix", field.ID)
	}
	if field.State != entity.StateDraft {
		t.Fatalf("field state = %q, want %q", field.State, entity.StateDraft)
	}
	if field.Change != entity.ChangeAdded {
		t.Fatalf("field change = %q, want %q", field.Change, entity.ChangeAdded)
	}
}

func TestDeleteFieldHardDeletesExistingField(t *testing.T) {
	repo := &fakeWorkspaceRepository{workspace: &entity.Workspace{
		ID:  "room_1",
		Rev: 7,
		Collaborators: []entity.Collaborator{{
			ID:   "col_1",
			Name: "Ava",
			Role: entity.RoleBackend,
		}},
		Resources: []entity.Resource{{
			ID:    "res_1",
			Name:  "Users",
			Kind:  entity.KindEndpoint,
			State: entity.StateReady,
			Fields: []entity.SchemaField{
				{ID: "fld_keep", Key: "name", Type: "string", State: entity.StateReady, Change: entity.ChangeStable},
				{ID: "fld_delete", Key: "email", Type: "string", State: entity.StateReady, Change: entity.ChangeStable},
			},
		}},
	}}
	service := NewService(repo, "room_1", nil)
	now := time.Date(2026, 7, 8, 9, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	result, err := service.DeleteField(context.Background(), "col_1", "res_1", "fld_delete", nil)
	if err != nil {
		t.Fatalf("DeleteField returned error: %v", err)
	}
	if result.Rev != 8 {
		t.Fatalf("Rev = %d, want 8", result.Rev)
	}
	if got := len(repo.workspace.Resources[0].Fields); got != 1 {
		t.Fatalf("field count = %d, want 1", got)
	}
	field := repo.workspace.Resources[0].Fields[0]
	if field.ID != "fld_keep" {
		t.Fatalf("remaining field ID = %q, want fld_keep", field.ID)
	}
}
