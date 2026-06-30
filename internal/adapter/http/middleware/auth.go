package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const CollaboratorKey = "collaborator_id"

type Auth struct {
	secret      []byte
	devIdentity string
}

func NewAuth(secret, devIdentity string) *Auth {
	return &Auth{secret: []byte(secret), devIdentity: devIdentity}
}

func (a *Auth) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenText := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
		if tokenText == "" {
			tokenText = c.Query("token")
		}
		if len(a.secret) == 0 && tokenText == "" {
			c.Set(CollaboratorKey, a.devIdentity)
			c.Next()
			return
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
		c.Set(CollaboratorKey, subject)
		c.Next()
	}
}

func abortUnauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "message": "missing or invalid token", "data": nil, "error": gin.H{"code": "UNAUTHORIZED"}})
}

func CollaboratorID(c *gin.Context) string {
	value, _ := c.Get(CollaboratorKey)
	id, _ := value.(string)
	return id
}
