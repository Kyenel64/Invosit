package handler

import (
	"net/http"
	"time"

	"github.com/kyenel64/invosit-api/internal/httpx"
)

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	uid := httpx.UserID(r.Context())
	if uid == "" {
		httpx.RespondError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}

	var (
		email     string
		createdAt time.Time
	)
	err := h.db.QueryRowContext(r.Context(),
		`SELECT email, created_at FROM users WHERE id = $1`, uid,
	).Scan(&email, &createdAt)
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"id":         uid,
		"email":      email,
		"created_at": createdAt,
	})
}
