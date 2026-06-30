package usecase

import (
	"context"
	"errors"
	"testing"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
)

type roomRepository struct {
	workspaces map[string]*entity.Workspace
}

func newRoomRepository() *roomRepository {
	return &roomRepository{workspaces: make(map[string]*entity.Workspace)}
}

func (r *roomRepository) Get(_ context.Context, id string) (*entity.Workspace, error) {
	workspace, ok := r.workspaces[id]
	if !ok {
		return nil, port.ErrWorkspaceNotFound
	}
	copy := *workspace
	copy.Collaborators = append([]entity.Collaborator(nil), workspace.Collaborators...)
	copy.Activity = append([]entity.ActivityEvent(nil), workspace.Activity...)
	return &copy, nil
}

func (r *roomRepository) Create(_ context.Context, workspace *entity.Workspace) error {
	if _, exists := r.workspaces[workspace.ID]; exists {
		return port.ErrWorkspaceExists
	}
	r.workspaces[workspace.ID] = workspace
	return nil
}

func (r *roomRepository) CreateIfAbsent(ctx context.Context, workspace *entity.Workspace) error {
	err := r.Create(ctx, workspace)
	if errors.Is(err, port.ErrWorkspaceExists) {
		return nil
	}
	return err
}

func (r *roomRepository) Save(_ context.Context, workspace *entity.Workspace, expectedRev int64) error {
	current, exists := r.workspaces[workspace.ID]
	if !exists || current.Rev != expectedRev {
		return port.ErrRevisionConflict
	}
	r.workspaces[workspace.ID] = workspace
	return nil
}

func TestCreateAndJoinRoomRestoresPersistedSession(t *testing.T) {
	repository := newRoomRepository()
	service := NewRoomService(repository)

	created, err := service.Create(context.Background(), "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Workspace.ID) != 6 || created.Collaborator.Name != "Alice" {
		t.Fatalf("unexpected created session: %#v", created)
	}

	joined, err := service.Join(context.Background(), created.Workspace.ID, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(joined.Workspace.Collaborators) != 2 {
		t.Fatalf("expected two collaborators, got %d", len(joined.Workspace.Collaborators))
	}

	restored, err := service.Join(context.Background(), created.Workspace.ID, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Collaborator.ID != joined.Collaborator.ID {
		t.Fatalf("expected restored collaborator %q, got %q", joined.Collaborator.ID, restored.Collaborator.ID)
	}
	if len(restored.Workspace.Collaborators) != 2 {
		t.Fatalf("re-login duplicated collaborator")
	}
}

func TestJoinRoomValidatesCode(t *testing.T) {
	service := NewRoomService(newRoomRepository())

	_, err := service.Join(context.Background(), "abc", "Alice")

	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}
