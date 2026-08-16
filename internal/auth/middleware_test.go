package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMiddlewareRequiresAndPropagatesIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newOIDCFixture(t)
	verifier := f.verifier(t)
	router := gin.New()
	router.Use(Middleware(verifier))
	router.GET("/_ping", func(c *gin.Context) {
		identity, ok := IdentityFromContext(c)
		if !ok {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.String(http.StatusOK, identity.Subject)
	})

	t.Run("valid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/_ping", nil)
		req.Header.Set("Authorization", "Bearer "+f.token(t, nil))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != http.StatusOK || response.Body.String() != testSubject {
			t.Fatalf("unexpected response: code=%d body=%q", response.Code, response.Body.String())
		}
	})

	for _, test := range []struct {
		name   string
		header []string
	}{
		{name: "missing"},
		{name: "wrong scheme", header: []string{"Basic abc"}},
		{name: "multiple", header: []string{"Bearer one", "Bearer two"}},
		{name: "invalid token", header: []string{"Bearer invalid"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/_ping", nil)
			for _, value := range test.header {
				req.Header.Add("Authorization", value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", response.Code)
			}
			if response.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("missing WWW-Authenticate header")
			}
		})
	}
}
