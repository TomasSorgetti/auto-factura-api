package postgres_test

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/TomasSorgetti/auto-factura-api/internal/db/sqlc"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres/postgrestest"
)

func TestNewPoolSettings(t *testing.T) {
	t.Parallel()
	pool := postgrestest.New(t)
	ctx := context.Background()

	if got := pool.Config().MaxConns; got != 2 {
		t.Errorf("MaxConns = %d, want 2", got)
	}
	for param, want := range map[string]string{
		"lock_timeout":                        "2s",
		"idle_in_transaction_session_timeout": "10s",
		"statement_timeout":                   "5s",
	} {
		var got string
		if err := pool.QueryRow(ctx, "SHOW "+param).Scan(&got); err != nil {
			t.Fatalf("show %s: %v", param, err)
		}
		if got != want {
			t.Errorf("%s = %q, want %q", param, got, want)
		}
	}
	ok, err := sqlc.New(pool).Ping(ctx)
	if err != nil || ok != 1 {
		t.Fatalf("ping = %d, %v", ok, err)
	}
}

func TestNewInvalidURL(t *testing.T) {
	t.Parallel()

	const url = "postgres://user:s3cretpw@localhost:notaport/db"
	_, err := postgres.New(context.Background(), postgres.Config{
		URL: logx.Secret(url), MaxConns: 1, StatementTimeout: time.Second, AppName: "test",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	for _, leak := range []string{"s3cretpw", url, "user"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("error leaks %q: %v", leak, err)
		}
	}
}

func TestWithTx(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")
	tests := []struct {
		name      string
		fn        func(tx pgx.Tx) error
		wantErr   error
		wantPanic bool
		wantRow   bool
	}{
		{name: "commit", fn: func(pgx.Tx) error { return nil }, wantRow: true},
		{name: "rollback on error", fn: func(pgx.Tx) error { return errBoom }, wantErr: errBoom},
		{name: "rollback on panic", fn: func(pgx.Tx) error { panic("kaboom") }, wantPanic: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pool := postgrestest.New(t)
			ctx := context.Background()
			if _, err := pool.Exec(ctx, "CREATE TABLE tx_probe (n int)"); err != nil {
				t.Fatal(err)
			}

			var err error
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				err = postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
					if _, e := tx.Exec(ctx, "INSERT INTO tx_probe VALUES (1)"); e != nil {
						return e
					}
					return tt.fn(tx)
				})
			}()

			if tt.wantPanic != (recovered != nil) {
				t.Fatalf("panic = %v, want panic %v", recovered, tt.wantPanic)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && !tt.wantPanic && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var n int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM tx_probe").Scan(&n); err != nil {
				t.Fatal(err)
			}
			if (n == 1) != tt.wantRow {
				t.Fatalf("rows = %d, want row %v", n, tt.wantRow)
			}
		})
	}
}

func TestWithTxGoexitReleasesConnection(t *testing.T) {
	t.Parallel()
	pool := postgrestest.New(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Go(func() {
		_ = postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SELECT 1"); err != nil {
				return err
			}
			runtime.Goexit()
			return nil
		})
	})
	wg.Wait()

	if n := pool.Stat().AcquiredConns(); n != 0 {
		t.Fatalf("AcquiredConns = %d, want 0", n)
	}
}
