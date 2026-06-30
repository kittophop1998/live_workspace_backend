package seed

import (
	"context"
	"time"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
)

func Workspace(ctx context.Context, repo port.WorkspaceRepository, workspaceID string) error {
	now := time.Now().UTC()
	return repo.CreateIfAbsent(ctx, &entity.Workspace{
		ID:  workspaceID,
		Rev: 1,
		Collaborators: []entity.Collaborator{
			{ID: "col_demo", Name: "Demo User", Role: entity.RoleBackend, Color: "#2563EB"},
			{ID: "col_frontend", Name: "Frontend User", Role: entity.RoleFrontend, Color: "#16A34A"},
		},
		Resources: []entity.Resource{},
		Comments:  []entity.Comment{},
		Activity: []entity.ActivityEvent{
			{ID: "act_seed", Actor: "System", Verb: "created", Target: "workspace", ResourceID: "", At: now},
		},
	})
}
