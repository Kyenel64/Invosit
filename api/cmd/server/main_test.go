package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestRun_Health boots the full server via run, polls /api/v1/health,
// then triggers graceful shutdown via ctx cancellation. Exercises config
// loading, signal-aware ctx, migrations, routing, and shutdown end-to-end.
//
// Requires a reachable Postgres. Set INVOSIT_TEST_DATABASE_URL to enable;
// the test is skipped otherwise so unit-test runs stay infra-free.
func TestRun_Health(t *testing.T) {
	dbURL := os.Getenv("INVOSIT_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("INVOSIT_TEST_DATABASE_URL not set; skipping end-to-end test")
	}

	port := "18080"
	env := map[string]string{
		"PORT":           port,
		"DATABASE_URL":   dbURL,
		"MIGRATIONS_DIR": "../../migrations",
	}
	getenv := func(k string) string { return env[k] }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- run(ctx, nil, getenv, io.Discard, io.Discard)
	}()

	endpoint := "http://localhost:" + port + "/api/v1/health"
	if err := waitForReady(ctx, 10*time.Second, endpoint); err != nil {
		cancel()
		<-runErr
		t.Fatalf("server not ready: %v", err)
	}

	resp, err := http.Get(endpoint)
	if err != nil {
		t.Fatalf("GET %s: %v", endpoint, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("run returned error: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("run did not exit after ctx cancel within 20s")
	}
}

// waitForReady polls endpoint until it returns 200, ctx is cancelled, or
// timeout elapses.
func waitForReady(ctx context.Context, timeout time.Duration, endpoint string) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		resp, err := http.Get(endpoint)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return context.DeadlineExceeded
}
