package kratos_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kyenel64/invosit/cli/internal/kratos"
)

// listenLoopback binds a random port on 127.0.0.1 for tests.
func listenLoopback(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return ln
}

// fakeBrowser simulates the browser hitting the loopback callback URL
// after the user signs in. It returns the response body so tests can
// assert the redirect target.
func fakeBrowser(t *testing.T, hitToken string) func(string) error {
	t.Helper()
	return func(loginURL string) error {
		u, err := url.Parse(loginURL)
		if err != nil {
			return err
		}
		cb := u.Query().Get("cli_callback")
		if cb == "" {
			t.Errorf("login URL missing cli_callback query: %s", loginURL)
			return errors.New("no cli_callback")
		}
		go func() {
			// Don't follow the loopback's redirect (a 302 to /cli-success
			// would dial the frontend, which doesn't exist in tests).
			c := &http.Client{
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
				Timeout: 5 * time.Second,
			}
			req, _ := http.NewRequest(http.MethodGet, cb+"?token="+hitToken, nil)
			resp, err := c.Do(req)
			if err != nil {
				t.Logf("fake browser GET: %v", err)
				return
			}
			_ = resp.Body.Close()
		}()
		return nil
	}
}

func TestBrowserLoginHappyPath(t *testing.T) {
	ln := listenLoopback(t)
	c := kratos.NewClient("http://kratos.invalid") // unused in browser login
	var stderr bytes.Buffer

	tok, err := c.BrowserLogin(context.Background(), kratos.BrowserLoginOpts{
		UIBaseURL:   "http://ui.test",
		Timeout:     5 * time.Second,
		OpenBrowser: fakeBrowser(t, "ory_st_browser_happy"),
		Stderr:      &stderr,
		Listener:    ln,
	})
	if err != nil {
		t.Fatalf("BrowserLogin: %v", err)
	}
	if tok != "ory_st_browser_happy" {
		t.Errorf("token = %q, want ory_st_browser_happy", tok)
	}
	if !strings.Contains(stderr.String(), "ui.test/login?cli_callback=") {
		t.Errorf("stderr should mention login URL; got %q", stderr.String())
	}
}

func TestBrowserLoginRedirectsToCliSuccess(t *testing.T) {
	ln := listenLoopback(t)
	c := kratos.NewClient("http://kratos.invalid")

	var redirectLocation string
	open := func(loginURL string) error {
		u, _ := url.Parse(loginURL)
		cb := u.Query().Get("cli_callback")
		go func() {
			client := &http.Client{
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
				Timeout: 5 * time.Second,
			}
			req, _ := http.NewRequest(http.MethodGet, cb+"?token=tok123", nil)
			resp, err := client.Do(req)
			if err != nil {
				t.Logf("fake browser: %v", err)
				return
			}
			redirectLocation = resp.Header.Get("Location")
			_ = resp.Body.Close()
		}()
		return nil
	}

	_, err := c.BrowserLogin(context.Background(), kratos.BrowserLoginOpts{
		UIBaseURL:   "http://ui.test",
		Timeout:     5 * time.Second,
		OpenBrowser: open,
		Stderr:      io.Discard,
		Listener:    ln,
	})
	if err != nil {
		t.Fatalf("BrowserLogin: %v", err)
	}
	// Give the goroutine a beat to record the redirect header. The
	// callback hits the loopback before BrowserLogin returns, but the
	// goroutine that records Location may not have finished writing
	// to the captured variable.
	deadline := time.Now().Add(time.Second)
	for redirectLocation == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if redirectLocation != "http://ui.test/cli-success" {
		t.Errorf("redirect Location = %q, want http://ui.test/cli-success", redirectLocation)
	}
}

func TestBrowserLoginTimeout(t *testing.T) {
	ln := listenLoopback(t)
	c := kratos.NewClient("http://kratos.invalid")

	openCalls := 0
	open := func(string) error { openCalls++; return nil } // never call back

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
	ln := listenLoopback(t)
	c := kratos.NewClient("http://kratos.invalid")
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

func TestBrowserLoginMissingToken(t *testing.T) {
	ln := listenLoopback(t)
	c := kratos.NewClient("http://kratos.invalid")

	open := func(loginURL string) error {
		u, _ := url.Parse(loginURL)
		cb := u.Query().Get("cli_callback")
		go func() {
			client := &http.Client{
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
				Timeout: 5 * time.Second,
			}
			req, _ := http.NewRequest(http.MethodGet, cb, nil) // no ?token=
			resp, err := client.Do(req)
			if err != nil {
				t.Logf("fake browser: %v", err)
				return
			}
			_ = resp.Body.Close()
		}()
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
		t.Fatal("want error when callback hit without token, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error %v should mention missing token", err)
	}
}

func TestBrowserLoginNoOpenPrintsURL(t *testing.T) {
	ln := listenLoopback(t)
	c := kratos.NewClient("http://kratos.invalid")
	var stderr bytes.Buffer

	// Trigger the callback after BrowserLogin has been allowed to
	// read the listener address and print the URL.
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if !strings.Contains(stderr.String(), "cli_callback=") {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			cb := "http://" + ln.Addr().String() + "/callback?token=tok_noopen"
			client := &http.Client{
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
				Timeout: 2 * time.Second,
			}
			req, _ := http.NewRequest(http.MethodGet, cb, nil)
			if resp, err := client.Do(req); err == nil {
				_ = resp.Body.Close()
			}
			return
		}
	}()

	tok, err := c.BrowserLogin(context.Background(), kratos.BrowserLoginOpts{
		UIBaseURL: "http://ui.test",
		Timeout:   5 * time.Second,
		NoOpen:    true,
		Stderr:    &stderr,
		Listener:  ln,
	})
	if err != nil {
		t.Fatalf("BrowserLogin: %v", err)
	}
	if tok != "tok_noopen" {
		t.Errorf("token = %q, want tok_noopen", tok)
	}
	if !strings.Contains(stderr.String(), "Open this URL") {
		t.Errorf("stderr should prompt the user; got %q", stderr.String())
	}
}

func TestBrowserLoginRequiresUIBaseURL(t *testing.T) {
	c := kratos.NewClient("http://kratos.invalid")
	_, err := c.BrowserLogin(context.Background(), kratos.BrowserLoginOpts{})
	if err == nil {
		t.Fatal("want error when UIBaseURL empty, got nil")
	}
}
