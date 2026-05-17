package kratos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

var ErrInvalidCredentials = errors.New("invalid credentials")

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type loginFlowResponse struct {
	UI struct {
		Action string `json:"action"`
	} `json:"ui"`
}

type loginSubmitRequest struct {
	Method     string `json:"method"`
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

type loginSubmitResponse struct {
	SessionToken string `json:"session_token"`
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// Login runs the kratos login flow. Returns session token
func (c *Client) Login(ctx context.Context, email string, password string) (string, error) {
	flow, err := c.initLoginFlow(ctx)
	if err != nil {
		return "", err
	}

	return c.submitCredentials(ctx, flow.UI.Action, email, password)
}

// initLoginFlow creates a new login flow from kratos
// Read more about login flows: https://www.ory.com/docs/kratos/self-service/flows/user-login
func (c *Client) initLoginFlow(ctx context.Context) (loginFlowResponse, error) {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/self-service/login/api", nil)
	if err != nil {
		return loginFlowResponse{}, fmt.Errorf("failed to create login flow request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return loginFlowResponse{}, fmt.Errorf("failed request to login flow: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	var flow loginFlowResponse
	if err := json.NewDecoder(res.Body).Decode(&flow); err != nil {
		return loginFlowResponse{}, fmt.Errorf("failed to decode login flow response: %w", err)
	}

	return flow, nil
}

// submitCredentials calls the login using the flow action. Returns session token
func (c *Client) submitCredentials(ctx context.Context, action string, email string, password string) (string, error) {
	body, err := json.Marshal(loginSubmitRequest{
		Method:     "password",
		Identifier: email,
		Password:   password,
	})
	if err != nil {
		return "", fmt.Errorf("failed to encode login submit body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, action, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create login request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed request to login: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	switch res.StatusCode {
	case http.StatusOK:
		var out loginSubmitResponse
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			return "", fmt.Errorf("failed to decode login submit response: %w", err)
		}
		return out.SessionToken, nil
	case http.StatusBadRequest:
		return "", ErrInvalidCredentials
	default:
		return "", fmt.Errorf("login submit failed: status %d", res.StatusCode)
	}
}
