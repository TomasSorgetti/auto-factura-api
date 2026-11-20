package queue_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres/postgrestest"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/queue"
)

func TestQueues(t *testing.T) {
	t.Parallel()

	want := map[string]int{queue.QueueARCA: 1, queue.QueuePDF: 1, queue.QueueEmail: 2, queue.QueueDefault: 2}
	got := queue.Queues()
	if len(got) != len(want) {
		t.Fatalf("queues = %v", got)
	}
	for name, n := range want {
		if got[name].MaxWorkers != n {
			t.Fatalf("queue %s = %d, want %d", name, got[name].MaxWorkers, n)
		}
	}
}

func riverTables(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('public.river_job') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func TestCheckSchema(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := postgrestest.NewEmpty(t)

	if err := queue.CheckSchema(ctx, pool); err == nil {
		t.Fatal("want error without river schema")
	}
	if riverTables(t, pool) {
		t.Fatal("CheckSchema created tables")
	}
	for i := range 2 {
		if err := queue.Migrate(ctx, pool); err != nil {
			t.Fatalf("migrate #%d: %v", i, err)
		}
	}
	if err := queue.CheckSchema(ctx, pool); err != nil {
		t.Fatalf("check after migrate: %v", err)
	}
	if v, err := queue.Version(ctx, pool); err != nil || v == 0 {
		t.Fatalf("version = %d err=%v", v, err)
	}
}

type noopArgs struct{}

func (noopArgs) Kind() string { return "queue.noop" }

func TestInsertTxRollback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := postgrestest.New(t)
	client, err := queue.NewInsertOnly(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	errRollback := context.Canceled
	err = postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := client.InsertTx(ctx, tx, noopArgs{}, nil); err != nil {
			return err
		}
		return errRollback
	})
	if err == nil || count() != 0 {
		t.Fatalf("rollback: err=%v rows=%d", err, count())
	}
	err = postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := client.InsertTx(ctx, tx, noopArgs{}, nil)
		return err
	})
	if err != nil || count() != 1 {
		t.Fatalf("commit: err=%v rows=%d", err, count())
	}
}
