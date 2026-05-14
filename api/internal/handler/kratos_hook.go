package handler

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/kyenel64/invosit/api/internal/httpx"
	"github.com/kyenel64/invosit/api/internal/ids"
)

type afterRegistrationRequest struct {
	IdentityID string    `json:"identity_id" validate:"required,uuid"`
	Email      string    `json:"email"       validate:"required,email,max=320"`
	CreatedAt  time.Time `json:"created_at"`
}

// AfterRegistration receives Kratos's after-registration webhook and creates
// the local users row. Idempotent — re-deliveries land on ON CONFLICT DO
// NOTHING. Authenticated by a shared secret in the X-Kratos-Webhook-Secret
// header (constant-time compared).
//
// The endpoint is mounted at /internal/hooks/... and the API only publishes
// 127.0.0.1 on the host, so the public attack surface is limited to anyone
// with docker-network access plus the secret.
func (h *Handler) AfterRegistration(w http.ResponseWriter, r *http.Request) {
	provided := r.Header.Get("X-Kratos-Webhook-Secret")
	if h.webhookKey == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(h.webhookKey)) != 1 {
		httpx.RespondError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "invalid webhook secret")
		return
	}

	var req afterRegistrationRequest
	if err := httpx.Bind(r, &req); err != nil {
		httpx.RespondError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid webhook payload")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	createdAt := req.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	if _, err := h.db.ExecContext(r.Context(),
		`INSERT INTO users (id, email, kratos_identity_id, created_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (kratos_identity_id) DO NOTHING`,
		ids.User(), email, req.IdentityID, createdAt,
	); err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{})
}
