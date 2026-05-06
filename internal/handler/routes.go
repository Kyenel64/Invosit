package handler

import "net/http"

// AddRoutes registers every API route on mux. Treat this file as the single
// map of the API surface — when adding a handler, add its route here.
func AddRoutes(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /api/v1/health", h.Health)

	mux.HandleFunc("POST /api/v1/auth/register", h.Register)
}
