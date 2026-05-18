// Package kratos wraps Ory's official Go SDK (github.com/ory/client-go)
// for the CLI's native-app browser-login dance. Mirrors the wrapper
// pattern used by the API server at api/internal/kratos/client.go so the
// SDK dependency stays contained.
package kratos

import ory "github.com/ory/client-go"

type Client struct {
	sdk *ory.APIClient
}

func NewClient(baseURL string) *Client {
	cfg := ory.NewConfiguration()
	cfg.Servers = ory.ServerConfigurations{{URL: baseURL}}
	return &Client{sdk: ory.NewAPIClient(cfg)}
}
