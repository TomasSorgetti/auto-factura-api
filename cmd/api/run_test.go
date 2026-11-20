package main

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres/postgrestest"
)

func TestRunShutsDown(t *testing.T) {
	t.Parallel()

	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("TEST_REDIS_URL is not set")
	}
	_, dbURL := postgrestest.NewURL(t)
	env := map[string]string{"APP_ENV": "test", "HTTP_ADDR": "127.0.0.1:0", "DATABASE_URL": dbURL, "REDIS_URL": redisURL}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, func(k string) string { return env[k] }, io.Discard)
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after cancel")
	}
}
