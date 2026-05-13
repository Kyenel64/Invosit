package handler

import (
	"database/sql"

	"github.com/kyenel64/invosit-api/internal/kratos"
	"github.com/kyenel64/invosit-api/internal/storage"
)

type Handler struct {
	db         *sql.DB
	kratos     *kratos.Client
	blobs      storage.Storage
	webhookKey string
}

func New(db *sql.DB, kc *kratos.Client, blobs storage.Storage, webhookKey string) *Handler {
	return &Handler{db: db, kratos: kc, blobs: blobs, webhookKey: webhookKey}
}
