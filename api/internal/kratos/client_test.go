package kratos

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Whoami_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/whoami" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		// SDK forwards the bare token via X-Session-Token, not Authorization.
		if got := r.Header.Get("X-Session-Token"); got != "abc" {
			t.Errorf("X-Session-Token = %q, want %q", got, "abc")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// schema_id and schema_url are required fields on identity per the SDK.
		w.Write([]byte(`{
			"id": "sess_xyz",
			"active": true,
			"identity": {
				"id": "00000000-0000-0000-0000-000000000001",
				"schema_id": "default",
				"schema_url": "http://x/schemas/default",
				"traits": {"email": "alice@example.com"}
			}
		}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	sess, err := c.Whoami(context.Background(), "abc", "")
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if sess.ID != "sess_xyz" || !sess.Active {
		t.Fatalf("session = %+v", sess)
	}
	if sess.IdentityID != "00000000-0000-0000-0000-000000000001" {
		t.Errorf("IdentityID = %q", sess.IdentityID)
	}
}

func TestClient_Whoami_Unauthenticated(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		c := NewClient(srv.URL)
		_, err := c.Whoami(context.Background(), "bad", "")
		if !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("status %d: err = %v, want ErrUnauthenticated", status, err)
		}
		srv.Close()
	}
}

func TestClient_Whoami_OtherError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Whoami(context.Background(), "x", "")
	if err == nil || errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want non-unauthenticated error", err)
	}
}

func TestClient_Whoami_ForwardsCookie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Cookie"); got != "ory_kratos_session=abc" {
			t.Errorf("Cookie = %q", got)
		}
		if got := r.Header.Get("X-Session-Token"); got != "" {
			t.Errorf("X-Session-Token should be empty, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"id":"s","active":true,
			"identity":{"id":"i","schema_id":"default","schema_url":"http://x","traits":{"email":"e@x"}}
		}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if _, err := c.Whoami(context.Background(), "", "ory_kratos_session=abc"); err != nil {
		t.Fatalf("Whoami: %v", err)
	}
}
