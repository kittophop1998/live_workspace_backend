package handler

import (
	"github.com/gin-gonic/gin"
	"kingdom_manager/backend/internal/adapter/http/middleware"
	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/usecase"
	"net/http"
	"time"
)

type APIKeyHandler struct{ service *usecase.APIKeyService }

func NewAPIKeyHandler(service *usecase.APIKeyService) *APIKeyHandler { return &APIKeyHandler{service} }

type createAPIKeyRequest struct {
	Name      string     `json:"name" binding:"required"`
	Scopes    []string   `json:"scopes" binding:"required"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

func keyJSON(v *entity.APIKey) gin.H {
	return gin.H{"id": v.ID, "projectId": v.ProjectID, "prefix": v.Prefix, "name": v.Name, "scopes": v.Scopes, "createdAt": v.CreatedAt, "expiresAt": v.ExpiresAt, "revokedAt": v.RevokedAt, "lastUsedAt": v.LastUsedAt}
}
func (h *APIKeyHandler) Create(c *gin.Context) {
	var r createAPIKeyRequest
	if !bind(c, &r) {
		return
	}
	v, secret, err := h.service.Create(c.Request.Context(), middleware.WorkspaceID(c), r.Name, middleware.CollaboratorID(c), r.Scopes, r.ExpiresAt)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"code": "VALIDATION_ERROR"}, "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"apiKey": keyJSON(v), "secret": secret})
}
func (h *APIKeyHandler) List(c *gin.Context) {
	values, err := h.service.List(c.Request.Context(), middleware.WorkspaceID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR"}})
		return
	}
	out := make([]gin.H, 0, len(values))
	for i := range values {
		out = append(out, keyJSON(&values[i]))
	}
	c.JSON(http.StatusOK, out)
}
func (h *APIKeyHandler) Revoke(c *gin.Context) {
	if err := h.service.Revoke(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id")); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Status(http.StatusNoContent)
}
