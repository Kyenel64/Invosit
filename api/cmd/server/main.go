package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kyenel64/invosit/api/internal/db"
	"github.com/kyenel64/invosit/api/internal/handler"
	"github.com/kyenel64/invosit/api/internal/kratos"
	"github.com/kyenel64/invosit/api/internal/middleware"
	"github.com/kyenel64/invosit/api/internal/storage"
)

func main() {
	if err := run(context.Background(), os.Args, os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Starts the HTTP server, and blocks until ctx is cancelled (signal) or the server returns an error.
func run(
	ctx context.Context,
	args []string,
	getenv func(string) string,
	stdout, stderr io.Writer,
) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	port := getenv("PORT")
	if port == "" {
		port = "8080"
	}

	databaseURL := getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}

	migrationsDir := getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "migrations"
	}

	kratosURL := getenv("KRATOS_PUBLIC_URL")
	if kratosURL == "" {
		return errors.New("KRATOS_PUBLIC_URL is required")
	}

	kratosWebhookSecret := getenv("KRATOS_WEBHOOK_SECRET")
	if kratosWebhookSecret == "" {
		return errors.New("KRATOS_WEBHOOK_SECRET is required")
	}

	var corsOrigins []string
	if raw := getenv("CORS_ALLOWED_ORIGINS"); raw != "" {
		corsOrigins = strings.Split(raw, ",")
	}

	database, err := db.Open(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			_, _ = fmt.Fprintf(stderr, "closing db: %v\n", err)
		}
	}()

	if err := db.Migrate(ctx, database, migrationsDir); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	kc := kratos.NewClient(kratosURL)

	storageCfg := storage.Config{
		Provider:  getenv("STORAGE_PROVIDER"),
		Bucket:    getenv("STORAGE_BUCKET"),
		Endpoint:  getenv("STORAGE_ENDPOINT"),
		AccessKey: getenv("STORAGE_ACCESS_KEY"),
		SecretKey: getenv("STORAGE_SECRET_KEY"),
		Region:    getenv("STORAGE_REGION"),
	}
	blobs, err := storage.New(storageCfg)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           NewServer(database, kc, blobs, kratosWebhookSecret, corsOrigins),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		_, _ = fmt.Fprintf(stdout, "starting server on: %s\n", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	case <-ctx.Done():
		_, _ = fmt.Fprintln(stdout, "shutdown signal received")
		// Parent ctx is already cancelled — derive shutdown deadline from a fresh root.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil { //nolint:contextcheck // fresh ctx is intentional during shutdown
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return nil
	}
}

// NewServer builds the application's http.Handler — mux, routes, and the
// global middleware stack. Returned as a single http.Handler so callers
// (and tests) only see one composed handler.
func NewServer(database *sql.DB, kc *kratos.Client, blobs storage.Storage, webhookSecret string, corsOrigins []string) http.Handler {
	mux := http.NewServeMux()
	h := handler.New(database, kc, blobs, webhookSecret)
	handler.AddRoutes(mux, h)

	// request -> Recovery -> CORS -> Logger -> BodyLimit -> mux -> handler
	// CORS sits ahead of Logger so preflight short-circuits don't fill logs
	// with OPTIONS noise; behind Recovery so a panic during preflight still
	// returns a clean 500.
	chain := middleware.Chain(
		middleware.Recovery,
		middleware.CORS(middleware.CORSConfig{AllowedOrigins: corsOrigins}),
		middleware.Logger,
		middleware.BodyLimit(10<<20),
	)
	return chain(mux)
}
