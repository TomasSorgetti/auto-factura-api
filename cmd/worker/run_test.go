package main

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres/postgrestest"
)

type startedWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	started chan struct{}
	once    sync.Once
}

func (w *startedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buf.Write(p)
	if bytes.Contains(w.buf.Bytes(), []byte("worker started")) {
		w.once.Do(func() { close(w.started) })
	}
	return n, err
}

func TestRunStopsOnSignal(t *testing.T) {
	t.Parallel()
	_, url := postgrestest.NewURL(t)
	env := map[string]string{"APP_ENV": "test", "DATABASE_URL": url}
	out := &startedWriter{started: make(chan struct{})}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, func(k string) string { return env[k] }, out) }()

	select {
	case <-out.started:
	case err := <-done:
		t.Fatalf("run ended before starting: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("worker did not start in 15s")
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop in 5s")
	}
}
