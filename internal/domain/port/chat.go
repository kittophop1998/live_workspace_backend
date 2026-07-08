package port

import (
	"context"

	"kingdom_manager/backend/internal/domain/entity"
)

// ChatRepository stores project-wide chat messages, append-only per workspace.
type ChatRepository interface {
	Append(ctx context.Context, workspaceID string, message entity.ChatMessage) error
	// List returns the most recent messages (up to limit), oldest first.
	List(ctx context.Context, workspaceID string, limit int) ([]entity.ChatMessage, error)
}
