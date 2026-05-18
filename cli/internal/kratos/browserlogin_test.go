package kratos_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kyenel64/invosit/cli/internal/kratos"
)

// listenLoopback binds a random port on 127.0.0.1 for tests so they
// don't collide with each other or with the production fixed port.
func listenLoopback(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return ln
}

// newExchangeKratos returns a mock Kratos that supports the exchange-code
// dance: returns a flow with an init_code from
// /self-service/login/api?return_session_token_exchange_code=true and
// trades (init_code, return_to_code) for a session_token at
// /sessions/token-exchange.
func newExchangeKratos(t *testing.T, initCode string, returnTokenFor map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Mock responses include every field the Ory SDK's generated structs
	// mark `required` (LoginFlow: expires_at, id, issued_at, request_url,
	// state, type, ui; UiContainer: action, method, nodes;
	// SuccessfulNativeLogin: session; Session: id). Omitting any of these
	// makes the SDK fail with "no value given for required property X" at
	// decode time.
	mux.HandleFunc("GET /self-service/login/api", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("return_session_token_exchange_code"); got != "true" {
			t.Errorf("return_session_token_exchange_code = %q, want true", got)
		}
		if r.URL.Query().Get("return_to") == "" {
			t.Errorf("return_to missing from init flow query")
		}
		w.Header().Set("Content-Type", "application/json")
		now := time.Now().UTC().Format(time.RFC3339Nano)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                          "flow-xyz",
			"type":                        "api",
			"expires_at":                  now,
			"issued_at":                   now,
			"request_url":                 srv.URL + "/self-service/login/api",
			"state":                       "choose_method",
			"session_token_exchange_code": initCode,
			"ui": map[string]any{
				"action": srv.URL + "/self-service/login?flow=flow-xyz",
				"method": "POST",
				"nodes":  []any{},
			},
		})
	})

	mux.HandleFunc("GET /sessions/token-exchange", func(w http.ResponseWriter, r *http.Request) {
		got := r.URL.Query().Get("init_code")
		retCode := r.URL.Query().Get("return_to_code")
		tok, ok := returnTokenFor[got+"|"+retCode]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session":       map[string]any{"id": "sess-1"},
			"session_token": tok,
		})
	})

	return srv
}

// hitLoopback GETs the loopback /callback?code=... — used by tests
// to simulate Kratos's final browser redirect.
func hitLoopback(t *testing.T, addr, code string) {
	t.Helper()
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 5 * time.Second,
	}
	u := "http://" + addr + "/callback"
	if code != "" {
		u += "?code=" + url.QueryEscape(code)
	}
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Logf("loopback hit failed: %v", err)
		return
	}
	_ = resp.Body.Close()
}

func TestBrowserLoginHappyPath(t *testing.T) {
	ks := newExchangeKratos(t, "INIT123", map[string]string{
		"INIT123|RT456": "ory_st_browser_ok",
	})
	c := kratos.NewClient(ks.URL)

	ln := listenLoopback(t)
	addr := ln.Addr().String()

	open := func(string) error {
		go hitLoopback(t, addr, "RT456")
		return nil
	}

	var stderr bytes.Buffer
	tok, err := c.BrowserLogin(context.Background(), kratos.BrowserLoginOpts{
		UIBaseURL:   "http://ui.test",
		Timeout:     5 * time.Second,
		OpenBrowser: open,
		Stderr:      &stderr,
		Listener:    ln,
	})
	if err != nil {
		t.Fatalf("BrowserLogin: %v", err)
	}
	if tok != "ory_st_browser_ok" {
		t.Errorf("token = %q, want ory_st_browser_ok", tok)
	}
	if !strings.Contains(stderr.String(), "ui.test/login?flow=") {
		t.Errorf("stderr should mention login URL with flow id; got %q", stderr.String())
	}
}

func TestBrowserLoginInitFlowError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /self-service/login/api", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := kratos.NewClient(srv.URL)
	ln := listenLoopback(t)

	_, err := c.BrowserLogin(context.Background(), kratos.BrowserLoginOpts{
		UIBaseURL: "http://ui.test",
		Timeout:   1 * time.Second,
		OpenBrowser: func(string) error {
			t.Error("OpenBrowser should not be called when init flow fails")
			return nil
		},
		Stderr:   io.Discard,
		Listener: ln,
	})
	if err == nil {
		t.Fatal("want init flow error, got nil")
	}
	if !strings.Contains(err.Error(), "init flow") && !strings.Contains(err.Error(), "400") {
		t.Errorf("error %v should mention init flow failure", err)
	}
}

func TestBrowserLoginTimeout(t *testing.T) {
	ks := newExchangeKratos(t, "INIT_T", map[string]string{})
	c := kratos.NewClient(ks.URL)
	ln := listenLoopback(t)

	openCalls := 0
	open := func(string) error { openCalls++; return nil } // never hit loopback

	_, err := c.BrowserLogin(context.Background(), kratos.BrowserLoginOpts{
		UIBaseURL:   "http://ui.test",
		Timeout:     100 * time.Millisecond,
		OpenBrowser: open,
		Stderr:      io.Discard,
		Listener:    ln,
	})
	if !errors.Is(err, kratos.ErrBrowserLoginTimeout) {
		t.Errorf("err = %v, want ErrBrowserLoginTimeout", err)
	}
	if openCalls != 1 {
		t.Errorf("OpenBrowser called %d times, want 1", openCalls)
	}
}

func TestBrowserLoginContextCanceled(t *testing.T) {
	ks := newExchangeKratos(t, "INIT_C", map[string]string{})
	c := kratos.NewClient(ks.URL)
	ln := listenLoopback(t)
	ctx, cancel := context.WithCancel(context.Background())

	open := func(string) error {
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()
		return nil
	}

	_, err := c.BrowserLogin(ctx, kratos.BrowserLoginOpts{
		UIBaseURL:   "http://ui.test",
		Timeout:     5 * time.Second,
		OpenBrowser: open,
		Stderr:      io.Discard,
		Listener:    ln,
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestBrowserLoginMissingCode(t *testing.T) {
	ks := newExchangeKratos(t, "INIT_M", map[string]string{})
	c := kratos.NewClient(ks.URL)
	ln := listenLoopback(t)
	addr := ln.Addr().String()

	open := func(string) error {
		go hitLoopback(t, addr, "")
		return nil
	}

	_, err := c.BrowserLogin(context.Background(), kratos.BrowserLoginOpts{
		UIBaseURL:   "http://ui.test",
		Timeout:     2 * time.Second,
		OpenBrowser: open,
		Stderr:      io.Discard,
		Listener:    ln,
	})
	if err == nil {
		t.Fatal("want error when callback hit without code, got nil")
	}
	if !strings.Contains(err.Error(), "code") {
		t.Errorf("error %v should mention missing code", err)
	}
}

func TestBrowserLoginExchangeError(t *testing.T) {
	// Kratos returns init_code OK but rejects the exchange.
	ks := newExchangeKratos(t, "INIT_E", map[string]string{}) // no token mapping ⇒ 400
	c := kratos.NewClient(ks.URL)
	ln := listenLoopback(t)
	addr := ln.Addr().String()

	open := func(string) error {
		go hitLoopback(t, addr, "RT-bad")
		return nil
	}

	_, err := c.BrowserLogin(context.Background(), kratos.BrowserLoginOpts{
		UIBaseURL:   "http://ui.test",
		Timeout:     2 * time.Second,
		OpenBrowser: open,
		Stderr:      io.Discard,
		Listener:    ln,
	})
	if err == nil {
		t.Fatal("want exchange error, got nil")
	}
	if !strings.Contains(err.Error(), "exchange") {
		t.Errorf("error %v should mention token exchange failure", err)
	}
}

func TestBrowserLoginRequiresUIBaseURL(t *testing.T) {
	c := kratos.NewClient("http://kratos.invalid")
	_, err := c.BrowserLogin(context.Background(), kratos.BrowserLoginOpts{})
	if err == nil {
		t.Fatal("want error when UIBaseURL empty, got nil")
	}
}
