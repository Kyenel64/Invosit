package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"

	"github.com/kyenel64/invosit-api/internal/auth"
	"github.com/kyenel64/invosit-api/internal/httpx"
	"github.com/kyenel64/invosit-api/internal/ids"
)

type registerRequest struct {
	Email    string `json:"email"    binding:"required,email,max=255"`
	Password string `json:"password" binding:"required,min=8,max=72"`
}

type userView struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func (h *Handler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.RespondError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid email or password")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrPasswordTooLong) {
			httpx.RespondError(c, http.StatusBadRequest, "INVALID_REQUEST", "password too long")
			return
		}
		httpx.InternalError(c, err)
		return
	}

	id := ids.User()
	if _, err := h.db.ExecContext(c.Request.Context(),
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3)`,
		id, email, hash,
	); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			httpx.RespondError(c, http.StatusConflict, "REGISTRATION_FAILED", "could not create account")
			return
		}
		httpx.InternalError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"user": userView{ID: id, Email: email},
	})
}
