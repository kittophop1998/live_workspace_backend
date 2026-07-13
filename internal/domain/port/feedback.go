package port

import (
	"context"
	"errors"

	"kingdom_manager/backend/internal/domain/entity"
)

var ErrFeedbackNotFound = errors.New("feedback not found")

type FeedbackRepository interface {
	CreateFeedback(context.Context, *entity.Feedback) error
	ListFeedback(ctx context.Context, workspaceID string) ([]entity.Feedback, error)
	GetFeedback(ctx context.Context, id string) (*entity.Feedback, error)
	UpdateFeedback(context.Context, *entity.Feedback) error
	DeleteFeedback(ctx context.Context, workspaceID, id string) (bool, error)
}
