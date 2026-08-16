package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const identityContextKey = "testtender.auth.identity"

// Middleware requires one Bearer token and stores its verified identity in the
// Gin context for later ownership authorization.
func Middleware(verifier *Verifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := bearerToken(c.Request)
		if err != nil {
			unauthorized(c)
			return
		}
		identity, err := verifier.Verify(c.Request.Context(), raw)
		if err != nil {
			unauthorized(c)
			return
		}
		c.Set(identityContextKey, identity)
		c.Next()
	}
}

// IdentityFromContext returns the authenticated caller for authorization code.
func IdentityFromContext(c *gin.Context) (Identity, bool) {
	value, exists := c.Get(identityContextKey)
	if !exists {
		return Identity{}, false
	}
	identity, ok := value.(Identity)
	return identity, ok
}

func bearerToken(r *http.Request) (string, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "", http.ErrNoCookie
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", http.ErrNoCookie
	}
	return parts[1], nil
}

func unauthorized(c *gin.Context) {
	c.Header("WWW-Authenticate", `Bearer realm="testtender"`)
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
}
