package kratos_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kyenel64/invosit/cli/internal/kratos"
)

// newMockKratos returns a test server that mimics kratos's login flow.
// It responds to GET /self-service/login/api with an action URL that
// points back at itself at /self-service/login, and serves POST to that
// path with the provided submit handler.
func newMockKratos(t *testing.T, submit http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("GET /self-service/login/api", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("init: Accept = %q, want application/json", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ui": map[string]any{
				"action": srv.URL + "/self-service/login?flow=test-flow",
			},
		})
	})
	mux.HandleFunc("POST /self-service/login", submit)

	return srv
}

func TestLoginSuccess(t *testing.T) {
	var gotMethod, gotIdentifier, gotPassword string
	var gotContentType, gotAccept string

	srv := newMockKratos(t, func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotAccept = r.Header.Get("Accept")

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("submit decode: %v", err)
		}
		gotMethod = body["method"]
		gotIdentifier = body["identifier"]
		gotPassword = body["password"]

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"session_token":"ory_st_success"}`))
	})

	c := kratos.NewClient(srv.URL)
	tok, err := c.Login(context.Background(), "alice@example.com", "hunter2")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok != "ory_st_success" {
		t.Errorf("token = %q, want ory_st_success", tok)
	}
	if gotMethod != "password" {
		t.Errorf("method = %q, want password", gotMethod)
	}
	if gotIdentifier != "alice@example.com" {
		t.Errorf("identifier = %q, want alice@example.com", gotIdentifier)
	}
	if gotPassword != "hunter2" {
		t.Errorf("password = %q, want hunter2", gotPassword)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	srv := newMockKratos(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad creds"}`))
	})

	c := kratos.NewClient(srv.URL)
	_, err := c.Login(context.Background(), "alice@example.com", "wrong")
	if !errors.Is(err, kratos.ErrInvalidCredentials) {
		t.Errorf("want ErrInvalidCredentials, got %v", err)
	}
}

func TestLoginUnexpectedSubmitStatus(t *testing.T) {
	srv := newMockKratos(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	c := kratos.NewClient(srv.URL)
	_, err := c.Login(context.Background(), "alice@example.com", "hunter2")
	if err == nil {
		t.Fatal("want error on 500, got nil")
	}
	if errors.Is(err, kratos.ErrInvalidCredentials) {
		t.Errorf("500 should not map to ErrInvalidCredentials")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code, got %v", err)
	}
}

func TestLoginSubmitInvalidJSON(t *testing.T) {
	srv := newMockKratos(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	})

	c := kratos.NewClient(srv.URL)
	_, err := c.Login(context.Background(), "alice@example.com", "hunter2")
	if err == nil {
		t.Fatal("want decode error, got nil")
	}
}

func TestLoginInitFlowInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := kratos.NewClient(srv.URL)
	_, err := c.Login(context.Background(), "alice@example.com", "hunter2")
	if err == nil {
		t.Fatal("want decode error on init flow, got nil")
	}
}

func TestLoginPostsToActionURL(t *testing.T) {
	var submitHit bool
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("GET /self-service/login/api", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ui": map[string]any{
				"action": srv.URL + "/custom/submit/path?flow=xyz",
			},
		})
	})
	mux.HandleFunc("POST /custom/submit/path", func(w http.ResponseWriter, r *http.Request) {
		submitHit = true
		if got := r.URL.Query().Get("flow"); got != "xyz" {
			t.Errorf("flow query = %q, want xyz", got)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"session_token":"ory_st_action"}`))
	})

	c := kratos.NewClient(srv.URL)
	tok, err := c.Login(context.Background(), "alice@example.com", "hunter2")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if !submitHit {
		t.Error("submit handler at action URL was never hit")
	}
	if tok != "ory_st_action" {
		t.Errorf("token = %q, want ory_st_action", tok)
	}
}

func TestLoginInitTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	c := kratos.NewClient(srv.URL)
	_, err := c.Login(context.Background(), "alice@example.com", "hunter2")
	if err == nil {
		t.Fatal("want transport error, got nil")
	}
}

func TestLoginContextCancel(t *testing.T) {
	srv := newMockKratos(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := kratos.NewClient(srv.URL)
	_, err := c.Login(ctx, "alice@example.com", "hunter2")
	if err == nil {
		t.Fatal("want context error, got nil")
	}
}
