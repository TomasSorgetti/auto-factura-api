// Package postgrestest creates throwaway databases for integration tests.
package postgrestest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	neturl "net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres"
)

// New returns a pool over a fresh, fully migrated database that is dropped
// when the test ends. The test is skipped without TEST_DATABASE_URL.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, _ := create(t, true)
	return pool
}

// NewEmpty is like New but leaves the database without migrations.
func NewEmpty(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, _ := create(t, false)
	return pool
}

// NewURL is like New and also returns the connection string of the new
// database.
func NewURL(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	return create(t, true)
}

func create(t *testing.T, migrate bool) (*pgxpool.Pool, string) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal("connect to maintenance database failed")
	}

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("random database name: %v", err)
	}
	name := "autofactura_test_" + hex.EncodeToString(suffix[:])
	quoted := pgx.Identifier{name}.Sanitize()

	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create test database: %v", err)
	}

	dropped := false
	drop := func() {
		if dropped {
			return
		}
		dropped = true
		dctx, dcancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dcancel()
		if _, err := admin.Exec(dctx, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Errorf("drop test database: %v", err)
		}
		if err := admin.Close(dctx); err != nil {
			t.Errorf("close maintenance connection: %v", err)
		}
	}

	u, err := neturl.Parse(url)
	if err != nil {
		drop()
		t.Fatal("parse TEST_DATABASE_URL failed")
	}
	u.Path = "/" + name
	dbURL := u.String()

	pool, err := postgres.New(ctx, postgres.Config{
		URL:              logx.Secret(dbURL),
		MaxConns:         2,
		StatementTimeout: 5 * time.Second,
		AppName:          "autofactura-test",
	})
	if err != nil {
		drop()
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		drop()
	})

	if migrate {
		m, err := postgres.NewMigrator(pool, false)
		if err != nil {
			t.Fatalf("new migrator: %v", err)
		}
		defer func() {
			if err := m.Close(); err != nil {
				t.Errorf("close migrator: %v", err)
			}
		}()
		if _, err := m.Up(ctx); err != nil {
			t.Fatalf("migrate test database: %v", err)
		}
	}
	return pool, dbURL
}
