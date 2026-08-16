package httputil

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type countingReadCloser struct {
	reader io.Reader
	read   int
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += n
	return n, err
}

func (*countingReadCloser) Close() error { return nil }

func TestRequestLoggerMiddlewareDoesNotConsumeBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := &countingReadCloser{reader: strings.NewReader("secret-payload")}

	router := gin.New()
	router.Use(RequestLoggerMiddleware())
	router.POST("/archive", func(c *gin.Context) {
		got := make([]byte, 6)
		if _, err := io.ReadFull(c.Request.Body, got); err != nil {
			t.Fatalf("read handler body: %v", err)
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/archive", nil)
	req.Body = body
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if body.read != 6 {
		t.Fatalf("middleware consumed request body: read %d bytes, want 6", body.read)
	}
}
