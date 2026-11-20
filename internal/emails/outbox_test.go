package emails_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TomasSorgetti/auto-factura-api/internal/emails"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres/postgrestest"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/queue"
)

func validMessage() emails.Message {
	return emails.Message{
		Kind:    emails.KindDeviceAlert,
		To:      []string{"admin@example.com"},
		Subject: "Hola",
		HTML:    "<p>hola</p>",
		Text:    "hola",
	}
}

func count(t *testing.T, pool *pgxpool.Pool, query string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEnqueue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := postgrestest.New(t)
	client, err := queue.NewInsertOnly(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	outbox := emails.NewOutbox(client)

	var id string
	err = postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
		var err error
		id, err = outbox.Enqueue(ctx, tx, validMessage())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM email_deliveries WHERE status = 'pending'`); n != 1 {
		t.Fatalf("deliveries = %d", n)
	}
	var argID string
	if err := pool.QueryRow(ctx, `SELECT args->>'deliveryId' FROM river_job WHERE kind = 'email.send' AND queue = 'email'`).Scan(&argID); err != nil {
		t.Fatal(err)
	}
	if argID != id {
		t.Fatalf("job delivery id = %s, want %s", argID, id)
	}

	rollback := errors.New("rollback")
	err = postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := outbox.Enqueue(ctx, tx, validMessage()); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("err = %v", err)
	}
	if n := count(t, pool, `SELECT count(*) FROM email_deliveries`); n != 1 {
		t.Fatalf("deliveries after rollback = %d", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM river_job WHERE kind = 'email.send'`); n != 1 {
		t.Fatalf("jobs after rollback = %d", n)
	}
}
