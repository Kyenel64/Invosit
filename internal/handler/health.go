package handler

import (
	"net/http"

	"github.com/kyenel64/invosit-api/internal/httpx"
)

// Health returns 200 when the database is reachable, 503 otherwise.
// Used as a readiness probe by orchestrators and end-to-end tests.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	if err := h.db.PingContext(r.Context()); err != nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "error",
			"error":  "database unreachable",
		})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
