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

// Returns session token
func (c *Client) Login(ctx context.Context, email string, password string) (string, error) {
	// Ory requires a call to begin a login flow before submitting login credentials.
	// Read more: https://www.ory.com/docs/kratos/self-service/flows/user-login

	// Initialize Flow
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/self-service/login/api", nil)
	if err != nil {
		return "", fmt.Errorf("build login flow init request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send login flow init request: %w", err)
	}
	defer res.Body.Close()

	var flow loginFlowResponse
	if err := json.NewDecoder(res.Body).Decode(&flow); err != nil {
		return "", fmt.Errorf("decode login flow response: %w", err)
	}

	// Submit credentials
	body, err := json.Marshal(loginSubmitRequest{
		Method:     "password",
		Identifier: email,
		Password:   password,
	})
	if err != nil {
		return "", fmt.Errorf("encode login submit body: %w", err)
	}

	loginReq, err := http.NewRequestWithContext(ctx, http.MethodPost, flow.UI.Action, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build login submit request: %w", err)
	}
	loginReq.Header.Set("Accept", "application/json")
	loginReq.Header.Set("Content-Type", "application/json")

	loginRes, err := c.httpClient.Do(loginReq)
	if err != nil {
		return "", fmt.Errorf("send login submit request: %w", err)
	}
	defer loginRes.Body.Close()

	switch loginRes.StatusCode {
	case http.StatusOK:
		var out loginSubmitResponse
		if err := json.NewDecoder(loginRes.Body).Decode(&out); err != nil {
			return "", fmt.Errorf("decode login submit response: %w", err)
		}
		return out.SessionToken, nil
	case http.StatusBadRequest:
		return "", ErrInvalidCredentials
	default:
		return "", fmt.Errorf("login submit failed: status %d", loginRes.StatusCode)
	}
}
