package kratos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

// LoopbackPort is the fixed local port the CLI listens on during browser
// login. Kratos's URL-pattern matcher doesn't accept port wildcards in
// allowed_return_urls, so the port has to be stable across runs.
const LoopbackPort = 33405

// LoopbackCallbackPath is the path the frontend redirects to with the
// session token after a successful browser login.
const LoopbackCallbackPath = "/callback"

// BrowserLoginOpts configures a browser-based login.
type BrowserLoginOpts struct {
	// UIBaseURL is the frontend origin where the user signs in,
	// e.g. http://127.0.0.1:5173. Required.
	UIBaseURL string

	// Timeout bounds how long we wait for the browser handoff.
	// Zero means use DefaultBrowserLoginTimeout.
	Timeout time.Duration

	// OpenBrowser opens the given URL in the user's default browser.
	// nil means use the platform default. Set NoOpen=true to skip
	// launching altogether and rely on the printed URL.
	OpenBrowser func(url string) error

	// NoOpen prints the URL to Stderr instead of launching a browser.
	NoOpen bool

	// Stderr receives the "open this URL" fallback message.
	Stderr io.Writer

	// Listener, if non-nil, is used in place of the default fixed-port
	// loopback. Tests inject a :0 listener so they don't collide with
	// each other or with a running CLI. The callback URL the frontend
	// is told to redirect to is derived from this listener's address.
	Listener net.Listener
}

// DefaultBrowserLoginTimeout is the default wait time for the browser
// handoff. Two minutes is enough for a user to sign in without being
// annoyingly tight, and short enough to fail fast on stuck flows.
const DefaultBrowserLoginTimeout = 2 * time.Minute

// ErrBrowserLoginTimeout is returned when the user doesn't complete
// login before the configured timeout.
var ErrBrowserLoginTimeout = errors.New("browser login timed out")

// BrowserLogin runs a browser-based login: starts a loopback HTTP
// listener, opens a browser at the frontend login page, and waits for
// the frontend to hand back a Kratos session_token via a redirect with
// ?token=<...>.
//
// This intentionally does NOT use Kratos's return_session_token_exchange_code
// mechanism. As of Kratos v1.3.1 that path doesn't generate a return-to
// code for the password method, so we get the session_token from the
// frontend (which runs an API flow on the browser side) and hand it to
// the CLI directly. When OIDC/passkey lands we'll revisit and use the
// Ory-native exchange-code path for those methods.
func (c *Client) BrowserLogin(ctx context.Context, opts BrowserLoginOpts) (string, error) {
	if opts.UIBaseURL == "" {
		return "", errors.New("BrowserLogin: UIBaseURL is required")
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultBrowserLoginTimeout
	}

	ln := opts.Listener
	if ln == nil {
		addr := fmt.Sprintf("127.0.0.1:%d", LoopbackPort)
		var err error
		var lc net.ListenConfig
		ln, err = lc.Listen(ctx, "tcp", addr)
		if err != nil {
			return "", fmt.Errorf("failed to bind loopback %s (is another invosit login already running?): %w", addr, err)
		}
	}
	callbackURL := fmt.Sprintf("http://%s%s", ln.Addr().String(), LoopbackCallbackPath)

	type result struct {
		token string
		err   error
	}
	done := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(LoopbackCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		// The frontend redirects the browser here with ?token=<session_token>
		// after a successful API-flow login. Capture the token, redirect
		// the user's browser to the success page, then signal the CLI to
		// continue.
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			select {
			case done <- result{err: errors.New("loopback callback received without token")}:
			default:
			}
			return
		}
		http.Redirect(w, r, opts.UIBaseURL+"/cli-success", http.StatusFound)
		select {
		case done <- result{token: token}:
		default:
		}
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	defer shutdownServer(ctx, srv)

	loginURL := fmt.Sprintf("%s/login?cli_callback=%s", opts.UIBaseURL, callbackURL)
	out := stderrOr(opts.Stderr)
	if opts.NoOpen {
		_, _ = fmt.Fprintf(out, "Open this URL in your browser to sign in:\n  %s\n", loginURL)
	} else {
		open := opts.OpenBrowser
		if open == nil {
			open = func(u string) error { return defaultOpenBrowser(ctx, u) }
		}
		_, _ = fmt.Fprintf(out, "Opening browser to sign in. If it doesn't open, visit:\n  %s\n", loginURL)
		if err := open(loginURL); err != nil {
			// Don't fail — fall back to the printed URL.
			_, _ = fmt.Fprintf(out, "(failed to open browser automatically: %v)\n", err)
		}
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(timeout):
		return "", ErrBrowserLoginTimeout
	case r := <-done:
		return r.token, r.err
	}
}

// defaultOpenBrowser shells out to the platform's URL handler. It uses
// Start (non-blocking) so a long-running browser process doesn't hang
// the CLI.
//
// The URL is built internally from configured base URLs plus a fixed
// query layout; it isn't user-supplied shell input, and exec.CommandContext
// doesn't invoke a shell. gosec's variable-arg heuristic doesn't know that.
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

// shutdownServer drains the loopback HTTP server with a small grace
// period. The grace window must outlive the caller's ctx (which may
// already be cancelled), so we deliberately derive it from Background.
func shutdownServer(_ context.Context, srv *http.Server) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx) //nolint:contextcheck // shutdown must survive parent ctx cancellation
}

func stderrOr(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}
