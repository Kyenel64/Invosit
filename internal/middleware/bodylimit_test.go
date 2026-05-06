package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newRouter(limit int64) *gin.Engine {
	r := gin.New()
	r.Use(BodyLimit(limit))
	r.POST("/echo", func(c *gin.Context) {
		// Force a body read so MaxBytesReader actually fires when oversized.
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatus(http.StatusRequestEntityTooLarge)
			return
		}
		c.Data(http.StatusOK, "text/plain", body)
	})
	return r
}

func TestBodyLimitUnderLimit(t *testing.T) {
	r := newRouter(100)

	body := strings.Repeat("a", 50)
	req := httptest.NewRequest("POST", "/echo", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Body.String() != body {
		t.Errorf("body roundtrip mismatch")
	}
}

func TestBodyLimitOverLimit(t *testing.T) {
	r := newRouter(100)

	body := strings.Repeat("a", 200)
	req := httptest.NewRequest("POST", "/echo", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", w.Code)
	}
}
