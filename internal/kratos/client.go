// Package kratos wraps Ory's official Go SDK (github.com/ory/client-go),
// exposing only the surface invosit-api uses: validating sessions via
// /sessions/whoami. The rest of the codebase imports our Session type, not
// the SDK directly — this keeps the SDK dependency contained and the
// boundary easy to mock in tests.
package kratos

import (
	"context"
	"errors"
	"net/http"

	ory "github.com/ory/client-go"
)

var ErrUnauthenticated = errors.New("kratos: unauthenticated")

type Client struct {
	sdk *ory.APIClient
}

func NewClient(baseURL string) *Client {
	cfg := ory.NewConfiguration()
	cfg.Servers = ory.ServerConfigurations{{URL: baseURL}}
	return &Client{sdk: ory.NewAPIClient(cfg)}
}

// Whoami calls /sessions/whoami. Pass the bare session token (no "Bearer "
// prefix — caller strips that) and/or the raw Cookie header value. At
// least one must be non-empty.
//
// Returns ErrUnauthenticated for 401/403; other transport / decode errors
// propagate wrapped.
func (c *Client) Whoami(ctx context.Context, sessionToken, cookie string) (*Session, error) {
	req := c.sdk.FrontendAPI.ToSession(ctx)
	if sessionToken != "" {
		req = req.XSessionToken(sessionToken)
	}
	if cookie != "" {
		req = req.Cookie(cookie)
	}

	sess, resp, err := req.Execute()
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return nil, ErrUnauthenticated
		}
		return nil, err
	}

	out := &Session{ID: sess.Id}
	if sess.Active != nil {
		out.Active = *sess.Active
	}
	if sess.Identity != nil {
		out.IdentityID = sess.Identity.Id
	}
	return out, nil
}
