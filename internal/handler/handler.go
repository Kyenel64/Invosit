package handler

import (
	"database/sql"

	"github.com/kyenel64/invosit-api/internal/kratos"
)

type Handler struct {
	db         *sql.DB
	kratos     *kratos.Client
	webhookKey string
}

func New(db *sql.DB, kc *kratos.Client, webhookKey string) *Handler {
	return &Handler{db: db, kratos: kc, webhookKey: webhookKey}
}
