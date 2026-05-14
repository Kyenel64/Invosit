package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newServer(limit int64) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /echo", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write(body)
	})
	return BodyLimit(limit)(mux)
}

func TestBodyLimitUnderLimit(t *testing.T) {
	srv := newServer(100)

	body := strings.Repeat("a", 50)
	req := httptest.NewRequest("POST", "/echo", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Body.String() != body {
		t.Errorf("body roundtrip mismatch")
	}
}

func TestBodyLimitOverLimit(t *testing.T) {
	srv := newServer(100)

	body := strings.Repeat("a", 200)
	req := httptest.NewRequest("POST", "/echo", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", w.Code)
	}
}
