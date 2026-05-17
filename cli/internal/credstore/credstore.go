// Package credstore persists CLI session credentials to disk at 0600.
package credstore

import (
	"errors"
	"time"
)

const SchemaVersion = 1

type Credentials struct {
	Version      int       `json:"version"`
	Email        string    `json:"email"`
	UserID       string    `json:"user_id"`
	SessionToken string    `json:"session_token"`
	KratosURL    string    `json:"kratos_url"`
	APIURL       string    `json:"api_url"`
	SavedAt      time.Time `json:"saved_at"`
}

type Store interface {
	Load() (Credentials, error)
	Save(Credentials) error
	Clear() error
}

var (
	ErrNotFound      = errors.New("credentials not found")
	ErrInsecurePerms = errors.New("credentials file has insecure permissions")
)
