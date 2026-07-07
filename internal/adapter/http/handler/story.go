package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"kingdom_manager/backend/internal/adapter/http/middleware"
	"kingdom_manager/backend/internal/usecase"
)

func (h *Handler) CreateStory(c *gin.Context) {
	var request createStoryRequest
	if !bind(c, &request) {
		return
	}
	actor, err := h.currentCollaborator(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	story, err := h.storyService.Create(c.Request.Context(), middleware.WorkspaceID(c), actor.Name, usecase.StoryInput{
		Name: request.Name, Steps: storyStepInputs(request.Steps),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusCreated, storyDTO(story))
}

func (h *Handler) ListStories(c *gin.Context) {
	stories, err := h.storyService.List(c.Request.Context(), middleware.WorkspaceID(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	out := make([]storyResponse, 0, len(stories))
	for i := range stories {
		out = append(out, storyDTO(&stories[i]))
	}
	success(c, http.StatusOK, out)
}

func (h *Handler) GetStory(c *gin.Context) {
	story, err := h.storyService.Get(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, storyDTO(story))
}

func (h *Handler) UpdateStory(c *gin.Context) {
	var request updateStoryRequest
	if !bind(c, &request) {
		return
	}
	actor, err := h.currentCollaborator(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	var steps *[]usecase.StoryStepInput
	if request.Steps != nil {
		inputs := storyStepInputs(*request.Steps)
		steps = &inputs
	}
	story, err := h.storyService.Update(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id"), actor.Name, usecase.StoryUpdateInput{
		Name: request.Name, Steps: steps,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, storyDTO(story))
}

func (h *Handler) DeleteStory(c *gin.Context) {
	if err := h.storyService.Delete(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id")); err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, gin.H{"story_id": c.Param("id")})
}

func (h *Handler) currentCollaborator(c *gin.Context) (*collaboratorResponse, error) {
	value, err := h.serviceFor(c).Me(c.Request.Context(), middleware.CollaboratorID(c))
	if err != nil {
		return nil, err
	}
	dto := collaboratorDTO(*value)
	return &dto, nil
}
