package handler

import (
	"net/http"

	"github.com/kyenel64/invosit-api/internal/middleware"
)

// AddRoutes registers every API route on mux. Treat this file as the single
// map of the API surface — when adding a handler, add its route here.
func AddRoutes(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /api/v1/health", h.Health)

	// Internal — gated by shared-secret header inside the handler. Reachable
	// from the docker network only (api host port is bound to 127.0.0.1).
	mux.HandleFunc("POST /internal/hooks/kratos/after-registration", h.AfterRegistration)

	authed := middleware.RequireKratosSession(h.kratos, h.db)
	mux.Handle("GET /api/v1/auth/me", authed(http.HandlerFunc(h.Me))) // authed injects user id to request context before calling h.Me
}
