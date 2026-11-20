package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres/postgrestest"
)

func TestMigrationsUpDownUp(t *testing.T) {
	t.Parallel()
	pool := postgrestest.NewEmpty(t)
	ctx := context.Background()

	err := postgres.CheckSchema(ctx, pool)
	if err == nil || !strings.Contains(err.Error(), "has no schema") {
		t.Fatalf("CheckSchema on empty database = %v", err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.goose_db_version') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("CheckSchema created the version table: exists=%v err=%v", exists, err)
	}

	m, err := postgres.NewMigrator(pool, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := m.Close(); err != nil {
			t.Errorf("close migrator: %v", err)
		}
	}()

	applied, err := m.Up(ctx)
	if err != nil || len(applied) == 0 {
		t.Fatalf("up = %d migrations, %v", len(applied), err)
	}
	if err := postgres.CheckSchema(ctx, pool); err != nil {
		t.Fatalf("CheckSchema after up: %v", err)
	}

	for range applied {
		if _, err := m.Down(ctx); err != nil {
			t.Fatalf("down: %v", err)
		}
	}
	if err := postgres.CheckSchema(ctx, pool); err == nil || !strings.Contains(err.Error(), "pending migrations") {
		t.Fatalf("CheckSchema after down to zero = %v", err)
	}

	again, err := m.Up(ctx)
	if err != nil || len(again) != len(applied) {
		t.Fatalf("second up = %d migrations, %v", len(again), err)
	}
}
