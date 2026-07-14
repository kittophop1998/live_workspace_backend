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

type fakeTaskLogRepository struct {
	entries []entity.TaskLog
}

func (f *fakeTaskLogRepository) Append(_ context.Context, _ string, entry entity.TaskLog) error {
	f.entries = append(f.entries, entry)
	return nil
}

func (f *fakeTaskLogRepository) List(_ context.Context, _ string, limit int) ([]entity.TaskLog, error) {
	if len(f.entries) <= limit {
		return f.entries, nil
	}
	return f.entries[len(f.entries)-limit:], nil
}

func (f *fakeTaskLogRepository) ToggleLike(_ context.Context, _ string, entryID, collaboratorID string) (*entity.TaskLog, error) {
	for i := range f.entries {
		if f.entries[i].ID != entryID {
			continue
		}
		likes := f.entries[i].Likes[:0:0]
		found := false
		for _, id := range f.entries[i].Likes {
			if id == collaboratorID {
				found = true
				continue
			}
			likes = append(likes, id)
		}
		if !found {
			likes = append(likes, collaboratorID)
		}
		f.entries[i].Likes = likes
		out := f.entries[i]
		return &out, nil
	}
	return nil, nil
}

type capturingPublisher struct {
	events []Event
}

func (p *capturingPublisher) Publish(e Event) { p.events = append(p.events, e) }

func TestAddTaskLogPersistsBroadcastsAndDefaultsKind(t *testing.T) {
	repo := &fakeWorkspaceRepository{workspace: &entity.Workspace{
		ID: "room_1",
		Collaborators: []entity.Collaborator{{
			ID: "col_1", Name: "Ava", Role: entity.RoleBackend,
		}},
		Resources: []entity.Resource{{ID: "res_1", Name: "Users", Kind: entity.KindEndpoint}},
	}}
	taskLogs := &fakeTaskLogRepository{}
	publisher := &capturingPublisher{}
	service := NewService(repo, nil, taskLogs, "room_1", publisher)
	now := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	// Empty kind defaults to "note"; resource_id is validated and kept.
	entry, err := service.AddTaskLog(context.Background(), "col_1", "", "  bumped the pagination limit  ", "res_1")
	if err != nil {
		t.Fatalf("AddTaskLog returned error: %v", err)
	}
	if entry.Kind != entity.TaskLogNote {
		t.Fatalf("kind = %q, want %q", entry.Kind, entity.TaskLogNote)
	}
	if entry.Body != "bumped the pagination limit" {
		t.Fatalf("body = %q, want trimmed", entry.Body)
	}
	if entry.Author != "Ava" || entry.Role != entity.RoleBackend {
		t.Fatalf("author/role = %q/%q, want Ava/backend (server-derived)", entry.Author, entry.Role)
	}
	if entry.ResourceID != "res_1" {
		t.Fatalf("resource_id = %q, want res_1", entry.ResourceID)
	}
	if len(publisher.events) != 1 || publisher.events[0].Type != "task_log.created" {
		t.Fatalf("events = %+v, want one task_log.created", publisher.events)
	}

	// A typed entry round-trips through List.
	if _, err := service.AddTaskLog(context.Background(), "col_1", entity.TaskLogAdded, "added POST /orders", ""); err != nil {
		t.Fatalf("AddTaskLog (typed) returned error: %v", err)
	}
	list, err := service.TaskLogs(context.Background())
	if err != nil {
		t.Fatalf("TaskLogs returned error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("task log count = %d, want 2", len(list))
	}
}

func TestToggleTaskLogLikeTogglesAndBroadcasts(t *testing.T) {
	repo := &fakeWorkspaceRepository{workspace: &entity.Workspace{
		ID: "room_1",
		Collaborators: []entity.Collaborator{
			{ID: "col_1", Name: "Ava", Role: entity.RoleBackend},
			{ID: "col_2", Name: "Bo", Role: entity.RoleFrontend},
		},
	}}
	taskLogs := &fakeTaskLogRepository{}
	publisher := &capturingPublisher{}
	service := NewService(repo, nil, taskLogs, "room_1", publisher)

	entry, err := service.AddTaskLog(context.Background(), "col_1", "note", "shipped it", "")
	if err != nil {
		t.Fatalf("AddTaskLog returned error: %v", err)
	}

	liked, err := service.ToggleTaskLogLike(context.Background(), "col_2", entry.ID)
	if err != nil {
		t.Fatalf("ToggleTaskLogLike returned error: %v", err)
	}
	if len(liked.Likes) != 1 || liked.Likes[0] != "col_2" {
		t.Fatalf("likes = %v, want [col_2]", liked.Likes)
	}
	last := publisher.events[len(publisher.events)-1]
	if last.Type != "task_log.updated" {
		t.Fatalf("last event = %q, want task_log.updated", last.Type)
	}

	// Second toggle by the same collaborator removes the like.
	unliked, err := service.ToggleTaskLogLike(context.Background(), "col_2", entry.ID)
	if err != nil {
		t.Fatalf("ToggleTaskLogLike (unlike) returned error: %v", err)
	}
	if len(unliked.Likes) != 0 {
		t.Fatalf("likes after unlike = %v, want empty", unliked.Likes)
	}

	if _, err := service.ToggleTaskLogLike(context.Background(), "col_2", "tlg_missing"); err == nil {
		t.Fatal("missing entry: expected error, got nil")
	}
	if _, err := service.ToggleTaskLogLike(context.Background(), "col_missing", entry.ID); err == nil {
		t.Fatal("unknown collaborator: expected error, got nil")
	}
}

func TestAddTaskLogRejectsBadInput(t *testing.T) {
	repo := &fakeWorkspaceRepository{workspace: &entity.Workspace{
		ID:            "room_1",
		Collaborators: []entity.Collaborator{{ID: "col_1", Name: "Ava", Role: entity.RoleBackend}},
	}}
	service := NewService(repo, nil, &fakeTaskLogRepository{}, "room_1", &capturingPublisher{})

	if _, err := service.AddTaskLog(context.Background(), "col_1", "", "   ", ""); err == nil {
		t.Fatal("empty body: expected error, got nil")
	}
	if _, err := service.AddTaskLog(context.Background(), "col_1", "bogus", "hi", ""); err == nil {
		t.Fatal("bad kind: expected error, got nil")
	}
	if _, err := service.AddTaskLog(context.Background(), "col_1", "note", "hi", "res_missing"); err == nil {
		t.Fatal("unknown resource_id: expected error, got nil")
	}
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
	service := NewService(repo, nil, nil, "room_1", nil)
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
	service := NewService(repo, nil, nil, "room_1", nil)
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
