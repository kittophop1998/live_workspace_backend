package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"kingdom_manager/backend/internal/adapter/http/middleware"
	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/usecase"
	"net/http"
)

type APISpecHandler struct{ service *usecase.APISpecService }

func (h *APISpecHandler) writeError(c *gin.Context, err error) {
	status, code, message := http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"
	var appErr *usecase.Error
	if errors.As(err, &appErr) {
		message = appErr.Message
	}
	if errors.Is(err, usecase.ErrValidation) {
		status, code = http.StatusUnprocessableEntity, "VALIDATION_ERROR"
	}
	if errors.Is(err, usecase.ErrNotFound) {
		status, code = http.StatusNotFound, "NOT_FOUND"
	}
	c.JSON(status, gin.H{"success": false, "message": message, "error": gin.H{"code": code}})
}

func NewAPISpecHandler(service *usecase.APISpecService) *APISpecHandler {
	return &APISpecHandler{service}
}
func revisionJSON(v *entity.APISpecRevision) gin.H {
	return gin.H{"id": v.ID, "number": v.Number, "status": v.Status, "contentHash": v.ContentHash, "sourceFilename": v.SourceFilename, "format": v.Format, "createdAt": v.CreatedAt}
}
func (h *APISpecHandler) Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"projectId": middleware.ProjectID(c), "workspaceId": middleware.WorkspaceID(c), "scopes": c.MustGet(middleware.ScopesKey)})
}

type publishAPISpecRequest struct {
	SourceFilename string `json:"sourceFilename" binding:"required"`
	Format         string `json:"format" binding:"required"`
	Content        string `json:"content" binding:"required"`
	ContentHash    string `json:"contentHash"`
	Message        string `json:"message"`
	Git            struct {
		Branch    string `json:"branch"`
		CommitSHA string `json:"commitSha"`
	} `json:"git"`
}

func (h *APISpecHandler) Publish(c *gin.Context) {
	if c.Param("projectId") != middleware.ProjectID(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	var r publishAPISpecRequest
	if !bind(c, &r) {
		return
	}
	value, unchanged, err := h.service.Publish(c.Request.Context(), middleware.ProjectID(c), usecase.PublishAPISpecInput{SourceFilename: r.SourceFilename, Format: r.Format, Content: r.Content, ContentHash: r.ContentHash, Message: r.Message, GitBranch: r.Git.Branch, GitCommitSHA: r.Git.CommitSHA, TokenID: middleware.CollaboratorID(c)})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"revision": revisionJSON(value), "unchanged": unchanged})
}
func (h *APISpecHandler) Current(c *gin.Context) { h.get(c, "") }
func (h *APISpecHandler) Get(c *gin.Context)     { h.get(c, c.Param("revisionId")) }
func (h *APISpecHandler) get(c *gin.Context, id string) {
	if c.Param("projectId") != middleware.ProjectID(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	var value *entity.APISpecRevision
	var err error
	if id == "" {
		value, err = h.service.Current(c.Request.Context(), middleware.ProjectID(c))
	} else {
		value, err = h.service.Get(c.Request.Context(), middleware.ProjectID(c), id)
	}
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revision": revisionJSON(value), "content": value.Content})
}
func (h *APISpecHandler) List(c *gin.Context) {
	if c.Param("projectId") != middleware.ProjectID(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	values, err := h.service.List(c.Request.Context(), middleware.ProjectID(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	out := make([]gin.H, 0, len(values))
	for i := range values {
		out = append(out, revisionJSON(&values[i]))
	}
	c.JSON(http.StatusOK, out)
}
