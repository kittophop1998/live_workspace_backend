package port

import (
	"context"

	"kingdom_manager/backend/internal/domain/entity"
)

// TaskLogRepository stores backend work-update log entries, append-only per
// workspace. Like ChatRepository it sits outside the versioned workspace
// aggregate: entries are immutable, so they need no per-revision copies.
type TaskLogRepository interface {
	Append(ctx context.Context, workspaceID string, entry entity.TaskLog) error
	// List returns the most recent entries (up to limit), oldest first.
	List(ctx context.Context, workspaceID string, limit int) ([]entity.TaskLog, error)
}
