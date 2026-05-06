package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/lib/pq"

	"github.com/kyenel64/invosit-api/internal/auth"
	"github.com/kyenel64/invosit-api/internal/httpx"
	"github.com/kyenel64/invosit-api/internal/ids"
)

type registerRequest struct {
	Email    string `json:"email"    validate:"required,email,max=255"`
	Password string `json:"password" validate:"required,min=8,max=72"`
}

type userView struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := httpx.Bind(r, &req); err != nil {
		httpx.RespondError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid email or password")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrPasswordTooLong) {
			httpx.RespondError(w, http.StatusBadRequest, "INVALID_REQUEST", "password too long")
			return
		}
		httpx.InternalError(w, r, err)
		return
	}

	id := ids.User()
	if _, err := h.db.ExecContext(r.Context(),
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3)`,
		id, email, hash,
	); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			httpx.RespondError(w, http.StatusConflict, "REGISTRATION_FAILED", "could not create account")
			return
		}
		httpx.InternalError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"user": userView{ID: id, Email: email},
	})
}
