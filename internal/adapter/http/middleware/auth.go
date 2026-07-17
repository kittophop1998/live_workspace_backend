package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const CollaboratorKey = "collaborator_id"
const WorkspaceKey = "workspace_id"
const ProjectKey = "project_id"
const ScopesKey = "scopes"

type Auth struct {
	secret   []byte
	tokenTTL time.Duration
}

func NewAuth(secret string, tokenTTL time.Duration) *Auth {
	return &Auth{secret: []byte(secret), tokenTTL: tokenTTL}
}

func (a *Auth) Issue(collaboratorID, workspaceID string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": collaboratorID, "workspace_id": workspaceID,
		"iat": now.Unix(), "exp": now.Add(a.tokenTTL).Unix(),
	}
	value, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secret)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return value, nil
}

func (a *Auth) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenText := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
		if tokenText == "" {
			tokenText = c.Query("token")
		}
		if tokenText == "" {
			abortUnauthorized(c)
			return
		}
		token, err := jwt.Parse(tokenText, func(token *jwt.Token) (any, error) {
			if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, jwt.ErrSignatureInvalid
			}
			return a.secret, nil
		}, jwt.WithExpirationRequired())
		if err != nil || !token.Valid {
			abortUnauthorized(c)
			return
		}
		subject, err := token.Claims.GetSubject()
		if err != nil || subject == "" {
			abortUnauthorized(c)
			return
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		workspaceID, okWorkspace := claims["workspace_id"].(string)
		if !ok || !okWorkspace || workspaceID == "" {
			abortUnauthorized(c)
			return
		}
		c.Set(CollaboratorKey, subject)
		c.Set(WorkspaceKey, workspaceID)
		projectID, _ := claims["project_id"].(string)
		if projectID == "" {
			projectID = workspaceID
		}
		c.Set(ProjectKey, projectID)
		c.Set(ScopesKey, claims["scopes"])
		c.Next()
	}
}

func ProjectID(c *gin.Context) string {
	value, _ := c.Get(ProjectKey)
	id, _ := value.(string)
	return id
}
func RequireScopes(required ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, _ := c.Get(ScopesKey)
		values, ok := raw.([]any)
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "INSUFFICIENT_SCOPE"}})
			return
		}
		have := map[string]bool{}
		for _, value := range values {
			if scope, ok := value.(string); ok {
				have[scope] = true
			}
		}
		for _, scope := range required {
			if !have[scope] {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "INSUFFICIENT_SCOPE"}})
				return
			}
		}
		c.Next()
	}
}

func WorkspaceID(c *gin.Context) string {
	value, _ := c.Get(WorkspaceKey)
	id, _ := value.(string)
	return id
}

func abortUnauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "message": "missing or invalid token", "data": nil, "error": gin.H{"code": "UNAUTHORIZED"}})
}

func CollaboratorID(c *gin.Context) string {
	value, _ := c.Get(CollaboratorKey)
	id, _ := value.(string)
	return id
}
