package handler

import (
	"time"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/usecase"
)

type storyStepBody struct {
	ID         string `json:"id,omitempty"`
	Type       string `json:"type" binding:"required"`
	ResourceID string `json:"resource_id"`
	Text       string `json:"text"`
}

type createStoryRequest struct {
	Name  string          `json:"name" binding:"required"`
	Steps []storyStepBody `json:"steps"`
}

type updateStoryRequest struct {
	Name  *string          `json:"name"`
	Steps *[]storyStepBody `json:"steps"`
}

type storyStepResponse struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	ResourceID string `json:"resource_id"`
	Text       string `json:"text"`
}

type storyResponse struct {
	ID          string              `json:"id"`
	WorkspaceID string              `json:"workspace_id"`
	Name        string              `json:"name"`
	Steps       []storyStepResponse `json:"steps"`
	CreatedAt   time.Time           `json:"created_at"`
	CreatedBy   string              `json:"created_by"`
	UpdatedAt   time.Time           `json:"updated_at"`
	UpdatedBy   string              `json:"updated_by"`
}

func storyDTO(story *entity.Story) storyResponse {
	out := storyResponse{
		ID: story.ID, WorkspaceID: story.WorkspaceID, Name: story.Name,
		CreatedAt: story.CreatedAt, CreatedBy: story.CreatedBy,
		UpdatedAt: story.UpdatedAt, UpdatedBy: story.UpdatedBy,
		Steps: []storyStepResponse{},
	}
	for _, step := range story.Steps {
		out.Steps = append(out.Steps, storyStepResponse{
			ID: step.ID, Type: string(step.Type), ResourceID: step.ResourceID, Text: step.Text,
		})
	}
	return out
}

func storyStepInputs(steps []storyStepBody) []usecase.StoryStepInput {
	out := make([]usecase.StoryStepInput, len(steps))
	for i, step := range steps {
		out[i] = usecase.StoryStepInput{ID: step.ID, Type: step.Type, ResourceID: step.ResourceID, Text: step.Text}
	}
	return out
}
