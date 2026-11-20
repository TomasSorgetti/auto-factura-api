package redisx

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
)

func TestOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		wantDB  int
		wantErr bool
	}{
		{name: "valid", url: "redis://localhost:6379/0", wantDB: 0},
		{name: "db from path", url: "redis://localhost:6379/15", wantDB: 15},
		{name: "invalid url", url: "http://not-redis:6379/0", wantErr: true},
		{name: "invalid with password", url: "redis://:s3cr3tpass@localhost:6379/notadb", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts, err := options(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if strings.Contains(err.Error(), tt.url) || strings.Contains(err.Error(), "s3cr3tpass") {
					t.Fatalf("error leaks url: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("options: %v", err)
			}
			if opts.DB != tt.wantDB {
				t.Errorf("DB = %d, want %d", opts.DB, tt.wantDB)
			}
			if opts.PoolSize != 5 || opts.MinIdleConns != 0 {
				t.Errorf("pool = %d/%d", opts.PoolSize, opts.MinIdleConns)
			}
			if opts.DialTimeout != 2*time.Second || opts.ReadTimeout != time.Second ||
				opts.WriteTimeout != time.Second || opts.PoolTimeout != 2*time.Second ||
				opts.ConnMaxIdleTime != 5*time.Minute || !opts.ContextTimeoutEnabled {
				t.Errorf("unexpected timeouts: %+v", opts)
			}
		})
	}
}

func TestNewPing(t *testing.T) {
	t.Parallel()

	t.Run("reachable", func(t *testing.T) {
		t.Parallel()
		url := os.Getenv("TEST_REDIS_URL")
		if url == "" {
			t.Skip("TEST_REDIS_URL is not set")
		}
		client, err := New(context.Background(), logx.Secret(url))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer client.Close()
		if err := client.Ping(context.Background()).Err(); err != nil {
			t.Fatalf("Ping: %v", err)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		t.Parallel()
		start := time.Now()
		client, err := New(context.Background(), "redis://127.0.0.1:1/0")
		if err == nil {
			client.Close()
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "ping redis") {
			t.Fatalf("error = %v", err)
		}
		if time.Since(start) > 5*time.Second {
			t.Fatalf("took %s", time.Since(start))
		}
	})
}
