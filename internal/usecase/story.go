package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
)

type StoryService struct {
	repo port.StoryRepository
	now  func() time.Time
}

func NewStoryService(repo port.StoryRepository) *StoryService {
	return &StoryService{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

type StoryInput struct {
	Name  string
	Steps []StoryStepInput
}

type StoryUpdateInput struct {
	Name  *string
	Steps *[]StoryStepInput
}

type StoryStepInput struct {
	ID         string
	Type       string
	ResourceID string
	Text       string
}

func (s *StoryService) Create(ctx context.Context, workspaceID, actorName string, in StoryInput) (*entity.Story, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, validation("story name is required", nil)
	}
	steps, err := storySteps(in.Steps)
	if err != nil {
		return nil, err
	}
	now := s.now()
	story := &entity.Story{
		ID: "sty_" + shortID(), WorkspaceID: workspaceID, Name: name, Steps: steps,
		CreatedAt: now, CreatedBy: actorName, UpdatedAt: now, UpdatedBy: actorName,
	}
	if err := s.repo.CreateStory(ctx, story); err != nil {
		return nil, fmt.Errorf("create story: %w", err)
	}
	return story, nil
}

func (s *StoryService) List(ctx context.Context, workspaceID string) ([]entity.Story, error) {
	return s.repo.ListStories(ctx, workspaceID)
}

func (s *StoryService) Get(ctx context.Context, workspaceID, id string) (*entity.Story, error) {
	story, err := s.repo.GetStory(ctx, id)
	if err != nil {
		if errors.Is(err, port.ErrStoryNotFound) {
			return nil, notFound("story", id)
		}
		return nil, err
	}
	if story.WorkspaceID != workspaceID {
		return nil, notFound("story", id)
	}
	return story, nil
}

func (s *StoryService) Update(ctx context.Context, workspaceID, id, actorName string, in StoryUpdateInput) (*entity.Story, error) {
	story, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, validation("story name cannot be empty", nil)
		}
		story.Name = name
	}
	if in.Steps != nil {
		steps, err := storySteps(*in.Steps)
		if err != nil {
			return nil, err
		}
		story.Steps = steps
	}
	story.UpdatedAt = s.now()
	story.UpdatedBy = actorName
	if err := s.repo.UpdateStory(ctx, story); err != nil {
		if errors.Is(err, port.ErrStoryNotFound) {
			return nil, notFound("story", id)
		}
		return nil, fmt.Errorf("update story: %w", err)
	}
	return story, nil
}

func (s *StoryService) Delete(ctx context.Context, workspaceID, id string) error {
	deleted, err := s.repo.DeleteStory(ctx, workspaceID, id)
	if err != nil {
		return fmt.Errorf("delete story: %w", err)
	}
	if !deleted {
		return notFound("story", id)
	}
	return nil
}

func storySteps(inputs []StoryStepInput) ([]entity.StoryStep, error) {
	steps := make([]entity.StoryStep, 0, len(inputs))
	ids := map[string]struct{}{}
	for index, input := range inputs {
		stepType := entity.StoryStepType(strings.TrimSpace(input.Type))
		if !validStoryStepType(stepType) {
			return nil, validation("invalid story step type", map[string]any{"index": index, "type": input.Type})
		}
		resourceID := strings.TrimSpace(input.ResourceID)
		if stepType == entity.StoryStepEndpoint {
			if resourceID == "" {
				return nil, validation("endpoint story steps require resource_id", map[string]any{"index": index})
			}
		} else if resourceID != "" {
			return nil, validation("note and section story steps must not set resource_id", map[string]any{"index": index})
		}
		id := strings.TrimSpace(input.ID)
		if id == "" {
			id = "sst_" + shortID()
		}
		if _, exists := ids[id]; exists {
			return nil, validation("duplicate story step id", map[string]any{"id": id})
		}
		ids[id] = struct{}{}
		steps = append(steps, entity.StoryStep{ID: id, Type: stepType, ResourceID: resourceID, Text: input.Text})
	}
	return steps, nil
}

func validStoryStepType(value entity.StoryStepType) bool {
	switch value {
	case entity.StoryStepEndpoint, entity.StoryStepNote, entity.StoryStepSection:
		return true
	default:
		return false
	}
}
