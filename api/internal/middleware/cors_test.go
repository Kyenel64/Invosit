package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newCORSServer(origins ...string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return CORS(CORSConfig{AllowedOrigins: origins})(mux)
}

func TestCORSAllowedOriginEchoed(t *testing.T) {
	srv := newCORSServer("http://127.0.0.1:5173")

	req := httptest.NewRequest("GET", "/ping", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:5173" {
		t.Errorf("Allow-Origin = %q, want %q", got, "http://127.0.0.1:5173")
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, want true", got)
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want Origin", got)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (request still served)", w.Code)
	}
}

func TestCORSDisallowedOriginNoHeaders(t *testing.T) {
	srv := newCORSServer("http://127.0.0.1:5173")

	req := httptest.NewRequest("GET", "/ping", nil)
	req.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty (origin not in allowlist)", got)
	}
	// The downstream handler still runs — the browser, not the server, is
	// responsible for blocking on the missing header.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestCORSPreflightShortCircuit(t *testing.T) {
	srv := newCORSServer("http://127.0.0.1:5173")

	req := httptest.NewRequest("OPTIONS", "/ping", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	req.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("Allow-Methods missing on preflight")
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("Allow-Headers missing on preflight")
	}
}

func TestCORSPlainOptionsNotPreflight(t *testing.T) {
	// OPTIONS without Access-Control-Request-Method is a real OPTIONS
	// request, not a preflight — must pass through to downstream handler.
	mux := http.NewServeMux()
	mux.HandleFunc("OPTIONS /ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	srv := CORS(CORSConfig{AllowedOrigins: []string{"http://127.0.0.1:5173"}})(mux)

	req := httptest.NewRequest("OPTIONS", "/ping", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusTeapot {
		t.Errorf("status = %d, want 418 (handler should run, not preflight short-circuit)", w.Code)
	}
}

func TestCORSEmptyAllowlistRejectsAll(t *testing.T) {
	srv := newCORSServer() // no allowed origins

	req := httptest.NewRequest("GET", "/ping", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty", got)
	}
}
