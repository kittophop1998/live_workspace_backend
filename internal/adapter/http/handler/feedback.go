package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"kingdom_manager/backend/internal/adapter/http/middleware"
	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/usecase"
)

type createFeedbackRequest struct {
	Category string `json:"category"`
	Body     string `json:"body" binding:"required"`
}

type setFeedbackStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

type feedbackResponse struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Category    string    `json:"category"`
	Body        string    `json:"body"`
	Author      string    `json:"author"`
	AuthorRole  string    `json:"author_role"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	UpdatedBy   string    `json:"updated_by"`
}

func feedbackDTO(f *entity.Feedback) feedbackResponse {
	return feedbackResponse{
		ID: f.ID, WorkspaceID: f.WorkspaceID, Category: string(f.Category), Body: f.Body,
		Author: f.Author, AuthorRole: string(f.AuthorRole), Status: string(f.Status),
		CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt, UpdatedBy: f.UpdatedBy,
	}
}

func (h *Handler) CreateFeedback(c *gin.Context) {
	var request createFeedbackRequest
	if !bind(c, &request) {
		return
	}
	actor, err := h.currentCollaborator(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	feedback, err := h.feedbackService.Create(c.Request.Context(), middleware.WorkspaceID(c), entity.Collaborator{ID: actor.ID, Name: actor.Name, Role: entity.CollaboratorRole(actor.Role), Color: actor.Color}, usecase.FeedbackCreateInput{
		Category: request.Category, Body: request.Body,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusCreated, feedbackDTO(feedback))
}

func (h *Handler) ListFeedback(c *gin.Context) {
	items, err := h.feedbackService.List(c.Request.Context(), middleware.WorkspaceID(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	out := make([]feedbackResponse, 0, len(items))
	for i := range items {
		out = append(out, feedbackDTO(&items[i]))
	}
	success(c, http.StatusOK, out)
}

func (h *Handler) SetFeedbackStatus(c *gin.Context) {
	var request setFeedbackStatusRequest
	if !bind(c, &request) {
		return
	}
	actor, err := h.currentCollaborator(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	feedback, err := h.feedbackService.SetStatus(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id"), actor.Name, entity.FeedbackStatus(request.Status))
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, feedbackDTO(feedback))
}

func (h *Handler) DeleteFeedback(c *gin.Context) {
	if err := h.feedbackService.Delete(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id")); err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, gin.H{"feedback_id": c.Param("id")})
}
