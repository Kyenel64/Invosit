// Package storage is the blob-storage boundary for the API.
// Do not expose provider sdk outside of this package.
// Blobs handed to these functions are already encrypted by the CLI.
// No plaintext should be read, logged, or buffered
package storage

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const MaxSignedURLExpiry = 15 * time.Minute

var ErrExpiryTooLong = errors.New("storage: signed URL expiry exceeds maximum")
var ErrUnknownProvider = errors.New("storage: unknown provider")

// Storage is the blob storage interface.
// Uploads and downloads happen directly between the CLI and the provider
// via signed URLs.
type Storage interface {
	// Returns short-lived URL for the CLI to call a PUT request for blobs
	SignedPutURL(ctx context.Context, key string, expiry time.Duration) (string, error)

	// Returns short-lived URL for the CLI to call a GET request for a blob
	SignedGetURL(ctx context.Context, key string, expiry time.Duration) (string, error)

	// Removes blob at key.
	// Never called from a CLI-driven user request directly
	Delete(ctx context.Context, key string) error
}

type Config struct {
	Provider     string // "r2" | "s3"
	Bucket       string
	Endpoint     string // empty for AWS, required for R2
	AccessKey    string
	SecretKey    string
	Region       string // real AWS region for s3, "auto" for r2
	UsePathStyle bool   // true for R2, false for S3
}

func New(cfg Config) (Storage, error) {
	if cfg.Provider == "" {
		cfg.Provider = "r2"
	}

	switch cfg.Provider {
	case "r2":
		if cfg.Endpoint == "" {
			return nil, errors.New("storage: STORAGE_ENDPOINT is required for r2")
		}
		if cfg.Region == "" {
			cfg.Region = "auto"
		}
		cfg.UsePathStyle = true
		return newS3Storage(cfg)

	case "s3":
		cfg.UsePathStyle = false
		return newS3Storage(cfg)

	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, cfg.Provider)
	}
}

func validateExpiry(d time.Duration) error {
	if d <= 0 {
		return errors.New("storage: expiry must be positive")
	}
	if d > MaxSignedURLExpiry {
		return ErrExpiryTooLong
	}
	return nil
}
