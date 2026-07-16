package middleware

import (
	"github.com/gin-gonic/gin"
	"kingdom_manager/backend/internal/usecase"
	"net/http"
	"strings"
)

const APIKeyIDKey = "api_key_id"

type APIKeyAuth struct{ service *usecase.APIKeyService }

func NewAPIKeyAuth(service *usecase.APIKeyService) *APIKeyAuth { return &APIKeyAuth{service} }
func (a *APIKeyAuth) Require(scopes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
		if raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED"}})
			return
		}
		key, err := a.service.Authenticate(c.Request.Context(), raw, scopes...)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "INSUFFICIENT_SCOPE_OR_INVALID_KEY"}})
			return
		}
		c.Set(ProjectKey, key.ProjectID)
		c.Set(APIKeyIDKey, key.ID)
		c.Set(ScopesKey, key.Scopes)
		c.Next()
	}
}
