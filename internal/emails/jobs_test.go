package emails_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/TomasSorgetti/auto-factura-api/internal/emails"
	"github.com/TomasSorgetti/auto-factura-api/internal/emails/emailstest"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres/postgrestest"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/queue"
)

type workerEnv struct {
	pool   *pgxpool.Pool
	outbox *emails.Outbox
	fake   *emailstest.Fake
	worker *emails.SendWorker
}

func newWorkerEnv(t *testing.T) workerEnv {
	t.Helper()
	pool := postgrestest.New(t)
	client, err := queue.NewInsertOnly(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &emailstest.Fake{}
	return workerEnv{
		pool:   pool,
		outbox: emails.NewOutbox(client),
		fake:   fake,
		worker: emails.NewSendWorker(pool, fake, slog.New(slog.NewTextHandler(io.Discard, nil))),
	}
}

func (e workerEnv) enqueue(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := postgres.WithTx(ctx, e.pool, func(tx pgx.Tx) error {
		var err error
		id, err = e.outbox.Enqueue(ctx, tx, validMessage())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func job(id string, attempt, maxAttempts int) *river.Job[emails.SendArgs] {
	return &river.Job[emails.SendArgs]{
		JobRow: &rivertype.JobRow{Attempt: attempt, MaxAttempts: maxAttempts},
		Args:   emails.SendArgs{DeliveryID: id},
	}
}

type deliveryRow struct {
	status, providerID, lastError string
	attempts                      int
}

func (e workerEnv) row(t *testing.T, id string) deliveryRow {
	t.Helper()
	var r deliveryRow
	err := e.pool.QueryRow(context.Background(),
		`SELECT status, coalesce(provider_message_id, ''), coalesce(last_error, ''), attempts FROM email_deliveries WHERE id = $1`, id,
	).Scan(&r.status, &r.providerID, &r.lastError, &r.attempts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSendWorker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("ok", func(t *testing.T) {
		t.Parallel()
		e := newWorkerEnv(t)
		id := e.enqueue(t)
		if err := e.worker.Work(ctx, job(id, 1, 5)); err != nil {
			t.Fatal(err)
		}
		r := e.row(t, id)
		if r.status != "sent" || r.providerID == "" || r.attempts != 1 {
			t.Fatalf("row = %+v", r)
		}
		sent := e.fake.Sent()
		if len(sent) != 1 || sent[0].IdempotencyKey != "email-delivery/"+id {
			t.Fatalf("sent = %+v", sent)
		}
	})

	t.Run("already sent", func(t *testing.T) {
		t.Parallel()
		e := newWorkerEnv(t)
		id := e.enqueue(t)
		if err := e.worker.Work(ctx, job(id, 1, 5)); err != nil {
			t.Fatal(err)
		}
		if err := e.worker.Work(ctx, job(id, 2, 5)); err != nil {
			t.Fatal(err)
		}
		if n := len(e.fake.Sent()); n != 1 {
			t.Fatalf("sent = %d", n)
		}
	})

	t.Run("permanent error", func(t *testing.T) {
		t.Parallel()
		e := newWorkerEnv(t)
		id := e.enqueue(t)
		e.fake.SetError(&emails.PermanentError{Err: errors.New("bad address")})
		err := e.worker.Work(ctx, job(id, 1, 5))
		var cancel *river.JobCancelError
		if !errors.As(err, &cancel) {
			t.Fatalf("err = %v, want JobCancel", err)
		}
		if r := e.row(t, id); r.status != "failed" || r.lastError == "" {
			t.Fatalf("row = %+v", r)
		}
	})

	t.Run("retry after", func(t *testing.T) {
		t.Parallel()
		e := newWorkerEnv(t)
		id := e.enqueue(t)
		e.fake.SetError(&emails.RetryAfterError{After: time.Second, Err: errors.New("rate limited")})
		err := e.worker.Work(ctx, job(id, 1, 5))
		var snooze *river.JobSnoozeError
		if !errors.As(err, &snooze) {
			t.Fatalf("err = %v, want JobSnooze", err)
		}
		if snooze.Duration != 10*time.Second {
			t.Fatalf("snooze = %v, want clamped to 10s", snooze.Duration)
		}
		if r := e.row(t, id); r.status != "pending" || r.lastError == "" {
			t.Fatalf("row = %+v", r)
		}
	})

	t.Run("plain error before last attempt", func(t *testing.T) {
		t.Parallel()
		e := newWorkerEnv(t)
		id := e.enqueue(t)
		e.fake.SetError(errors.New("timeout"))
		if err := e.worker.Work(ctx, job(id, 2, 5)); err == nil {
			t.Fatal("want error")
		}
		if r := e.row(t, id); r.status != "pending" || r.lastError == "" {
			t.Fatalf("row = %+v", r)
		}
	})

	t.Run("plain error on last attempt", func(t *testing.T) {
		t.Parallel()
		e := newWorkerEnv(t)
		id := e.enqueue(t)
		e.fake.SetError(errors.New("timeout"))
		if err := e.worker.Work(ctx, job(id, 5, 5)); err == nil {
			t.Fatal("want error")
		}
		if r := e.row(t, id); r.status != "failed" {
			t.Fatalf("row = %+v", r)
		}
	})

	t.Run("missing delivery", func(t *testing.T) {
		t.Parallel()
		e := newWorkerEnv(t)
		err := e.worker.Work(ctx, job("00000000-0000-4000-8000-000000000000", 1, 5))
		var cancel *river.JobCancelError
		if !errors.As(err, &cancel) {
			t.Fatalf("err = %v, want JobCancel", err)
		}
	})
}
