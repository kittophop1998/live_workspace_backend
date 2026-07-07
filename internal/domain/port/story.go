package port

import (
	"context"
	"errors"

	"kingdom_manager/backend/internal/domain/entity"
)

var ErrStoryNotFound = errors.New("story not found")

type StoryRepository interface {
	CreateStory(context.Context, *entity.Story) error
	ListStories(ctx context.Context, workspaceID string) ([]entity.Story, error)
	GetStory(ctx context.Context, id string) (*entity.Story, error)
	UpdateStory(context.Context, *entity.Story) error
	DeleteStory(ctx context.Context, workspaceID, id string) (bool, error)
}
