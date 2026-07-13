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

type FeedbackService struct {
	repo port.FeedbackRepository
	now  func() time.Time
}

func NewFeedbackService(repo port.FeedbackRepository) *FeedbackService {
	return &FeedbackService{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

type FeedbackCreateInput struct {
	Category string
	Body     string
}

var feedbackCategories = map[entity.FeedbackCategory]bool{
	entity.FeedbackCategoryComplaint:   true,
	entity.FeedbackCategoryImprovement: true,
	entity.FeedbackCategoryBug:         true,
	entity.FeedbackCategoryOther:       true,
}

var feedbackStatuses = map[entity.FeedbackStatus]bool{
	entity.FeedbackStatusOpen:       true,
	entity.FeedbackStatusInProgress: true,
	entity.FeedbackStatusResolved:   true,
	entity.FeedbackStatusDismissed:  true,
}

func (s *FeedbackService) Create(ctx context.Context, workspaceID string, actor entity.Collaborator, in FeedbackCreateInput) (*entity.Feedback, error) {
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return nil, validation("feedback body is required", nil)
	}
	category := entity.FeedbackCategory(strings.TrimSpace(in.Category))
	if category == "" {
		category = entity.FeedbackCategoryOther
	}
	if !feedbackCategories[category] {
		return nil, validation("invalid feedback category", map[string]any{"category": in.Category})
	}
	now := s.now()
	feedback := &entity.Feedback{
		ID: "fbk_" + shortID(), WorkspaceID: workspaceID, Category: category, Body: body,
		Author: actor.Name, AuthorRole: actor.Role, Status: entity.FeedbackStatusOpen,
		CreatedAt: now, UpdatedAt: now, UpdatedBy: actor.Name,
	}
	if err := s.repo.CreateFeedback(ctx, feedback); err != nil {
		return nil, fmt.Errorf("create feedback: %w", err)
	}
	return feedback, nil
}

func (s *FeedbackService) List(ctx context.Context, workspaceID string) ([]entity.Feedback, error) {
	return s.repo.ListFeedback(ctx, workspaceID)
}

func (s *FeedbackService) Get(ctx context.Context, workspaceID, id string) (*entity.Feedback, error) {
	feedback, err := s.repo.GetFeedback(ctx, id)
	if err != nil {
		if errors.Is(err, port.ErrFeedbackNotFound) {
			return nil, notFound("feedback", id)
		}
		return nil, err
	}
	if feedback.WorkspaceID != workspaceID {
		return nil, notFound("feedback", id)
	}
	return feedback, nil
}

func (s *FeedbackService) SetStatus(ctx context.Context, workspaceID, id, actorName string, status entity.FeedbackStatus) (*entity.Feedback, error) {
	if !feedbackStatuses[status] {
		return nil, validation("invalid feedback status", map[string]any{"status": status})
	}
	feedback, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	feedback.Status = status
	feedback.UpdatedAt = s.now()
	feedback.UpdatedBy = actorName
	if err := s.repo.UpdateFeedback(ctx, feedback); err != nil {
		if errors.Is(err, port.ErrFeedbackNotFound) {
			return nil, notFound("feedback", id)
		}
		return nil, fmt.Errorf("update feedback: %w", err)
	}
	return feedback, nil
}

func (s *FeedbackService) Delete(ctx context.Context, workspaceID, id string) error {
	deleted, err := s.repo.DeleteFeedback(ctx, workspaceID, id)
	if err != nil {
		return fmt.Errorf("delete feedback: %w", err)
	}
	if !deleted {
		return notFound("feedback", id)
	}
	return nil
}
