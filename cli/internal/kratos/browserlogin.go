package kratos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"
)

const LoopbackPort = 33405
const LoopbackCallbackPath = "/callback"
const DefaultBrowserLoginTimeout = 2 * time.Minute

var ErrBrowserLoginTimeout = errors.New("browser login timed out")

type BrowserLoginOpts struct {
	UIBaseURL string
	Timeout   time.Duration
	NoOpen    bool
	Stderr    io.Writer

	OpenBrowser func(url string) error
	Listener    net.Listener
}

type callbackResult struct {
	code string
	err  error
}

// BrowserLogin runs an Ory-native browser-based login using the
// `return_session_token_exchange_code` mechanism.
// Docs: https://www.ory.com/docs/kratos/social-signin/native-apps
func (c *Client) BrowserLogin(ctx context.Context, opts BrowserLoginOpts) (string, error) {
	if opts.UIBaseURL == "" {
		return "", errors.New("BrowserLogin: UIBaseURL is required")
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultBrowserLoginTimeout
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}

	callbackURL, codes, shutdown, err := setupLoopback(ctx, opts)
	if err != nil {
		return "", err
	}
	defer shutdown()

	flowID, initCode, err := c.initFlowWithExchangeCode(ctx, callbackURL)
	if err != nil {
		return "", err
	}

	loginURL := fmt.Sprintf("%s/login?flow=%s", opts.UIBaseURL, flowID)
	promptUserToSignIn(ctx, opts, loginURL)

	// Bound only the wait, not the whole login
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	returnToCode, err := waitForCode(waitCtx, codes)
	if err != nil {
		return "", err
	}
	return c.exchangeSessionToken(ctx, initCode, returnToCode)
}

// setupLoopback starts a local HTTP listener to catch Kratos's
// return_to_code redirect after login.
func setupLoopback(ctx context.Context, opts BrowserLoginOpts) (string, <-chan callbackResult, func(), error) {
	listener := opts.Listener
	if listener == nil {
		addr := fmt.Sprintf("127.0.0.1:%d", LoopbackPort)
		var listenConfig net.ListenConfig
		var err error
		listener, err = listenConfig.Listen(ctx, "tcp", addr)
		if err != nil {
			return "", nil, nil, fmt.Errorf("failed to bind loopback %s (is another invosit login already running?): %w", addr, err)
		}
	}
	callbackURL := fmt.Sprintf("http://%s%s", listener.Addr().String(), LoopbackCallbackPath)

	codes := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(LoopbackCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			select {
			case codes <- callbackResult{err: errors.New("loopback callback received without code")}:
			default:
			}
			return
		}
		http.Redirect(w, r, opts.UIBaseURL+"/", http.StatusFound)
		select {
		case codes <- callbackResult{code: code}:
		default:
		}
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(listener) }()

	return callbackURL, codes, func() { shutdownServer(ctx, srv) }, nil
}

func promptUserToSignIn(ctx context.Context, opts BrowserLoginOpts, loginURL string) {
	if opts.NoOpen {
		_, _ = fmt.Fprintf(opts.Stderr, "Open this URL in your browser to sign in:\n  %s\n", loginURL)
		return
	}

	open := opts.OpenBrowser
	if open == nil {
		open = func(u string) error { return defaultOpenBrowser(ctx, u) }
	}
	_, _ = fmt.Fprintf(opts.Stderr, "Opening browser to sign in. If it doesn't open, visit:\n  %s\n", loginURL)
	if err := open(loginURL); err != nil {
		_, _ = fmt.Fprintf(opts.Stderr, "(failed to open browser automatically: %v)\n", err)
	}
}

func waitForCode(ctx context.Context, codes <-chan callbackResult) (string, error) {
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", ErrBrowserLoginTimeout
		}
		return "", ctx.Err()
	case r := <-codes:
		if r.err != nil {
			return "", r.err
		}
		return r.code, nil
	}
}

// initFlowWithExchangeCode initializes a Kratos API login flow with
// return_to pointing at our loopback and return_session_token_exchange_code turned on.
func (c *Client) initFlowWithExchangeCode(ctx context.Context, returnTo string) (flowID, initCode string, err error) {
	u := fmt.Sprintf(
		"%s/self-service/login/api?return_session_token_exchange_code=true&return_to=%s",
		c.baseURL,
		url.QueryEscape(returnTo),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", "", fmt.Errorf("create init flow request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("init flow request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("init flow: status %d", res.StatusCode)
	}
	var body struct {
		ID                       string `json:"id"`
		SessionTokenExchangeCode string `json:"session_token_exchange_code"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return "", "", fmt.Errorf("decode init flow response: %w", err)
	}
	if body.ID == "" || body.SessionTokenExchangeCode == "" {
		return "", "", errors.New("init flow response missing id or exchange code")
	}
	return body.ID, body.SessionTokenExchangeCode, nil
}

// exchangeSessionToken uses the initCode and returnToCode
// to retrieve a session token from Kratos.
func (c *Client) exchangeSessionToken(ctx context.Context, initCode, returnToCode string) (string, error) {
	u := fmt.Sprintf(
		"%s/sessions/token-exchange?init_code=%s&return_to_code=%s",
		c.baseURL,
		url.QueryEscape(initCode),
		url.QueryEscape(returnToCode),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("create exchange request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchange request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange: status %d", res.StatusCode)
	}
	var body struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode exchange response: %w", err)
	}
	if body.SessionToken == "" {
		return "", errors.New("exchange response missing session_token")
	}
	return body.SessionToken, nil
}

func defaultOpenBrowser(ctx context.Context, url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url) //nolint:gosec // G204
	case "linux":
		cmd = exec.CommandContext(ctx, "xdg-open", url) //nolint:gosec // G204
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url) //nolint:gosec // G204
	default:
		return fmt.Errorf("unsupported platform %q for browser-open", runtime.GOOS)
	}
	return cmd.Start()
}

func shutdownServer(_ context.Context, srv *http.Server) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx) //nolint:contextcheck // shutdown must survive parent ctx cancellation
}
