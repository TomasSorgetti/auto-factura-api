package emails

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/queue"
)

const (
	sendTimeout    = 15 * time.Second
	minSnooze      = 10 * time.Second
	maxSnooze      = 10 * time.Minute
	idempotencyPfx = "email-delivery/"
)

type SendArgs struct {
	DeliveryID string `json:"deliveryId"`
}

func (SendArgs) Kind() string { return "email.send" }

func (SendArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: queue.QueueEmail, MaxAttempts: 5, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// SendWorker sends one queued delivery. It never holds a transaction while calling the provider.
type SendWorker struct {
	river.WorkerDefaults[SendArgs]
	store  *store
	sender Sender
	log    *slog.Logger
}

func NewSendWorker(pool *pgxpool.Pool, sender Sender, log *slog.Logger) *SendWorker {
	return &SendWorker{store: newStore(pool), sender: sender, log: log}
}

func (w *SendWorker) Timeout(*river.Job[SendArgs]) time.Duration { return 30 * time.Second }

func (w *SendWorker) Work(ctx context.Context, job *river.Job[SendArgs]) error {
	id := job.Args.DeliveryID
	d, err := w.store.get(ctx, id)
	if errors.Is(err, errDeliveryNotFound) {
		return river.JobCancel(err)
	}
	if err != nil {
		return err
	}
	if d.Status != statusPending {
		return nil
	}
	if err := w.store.incrementAttempts(ctx, id); err != nil {
		return err
	}

	msg := d.Message
	msg.IdempotencyKey = idempotencyPfx + id
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	providerID, sendErr := w.sender.Send(sendCtx, msg)
	cancel()
	log := w.log.With("delivery_id", id, "kind", string(d.Message.Kind), "attempt", job.Attempt)

	if sendErr == nil {
		ok, err := w.store.markSent(ctx, id, providerID)
		if err != nil {
			return err
		}
		if !ok {
			log.WarnContext(ctx, "email delivery was no longer pending after send")
		}
		return nil
	}

	var permanent *PermanentError
	var retry *RetryAfterError
	switch {
	case errors.As(sendErr, &permanent):
		if err := w.store.markFailed(ctx, id, sendErr); err != nil {
			return err
		}
		log.WarnContext(ctx, "email delivery failed permanently")
		return river.JobCancel(sendErr)
	case errors.As(sendErr, &retry):
		if err := w.store.setError(ctx, id, sendErr); err != nil {
			return err
		}
		return river.JobSnooze(min(max(retry.After, minSnooze), maxSnooze))
	}
	if job.Attempt >= job.MaxAttempts {
		if err := w.store.markFailed(ctx, id, sendErr); err != nil {
			return err
		}
		log.WarnContext(ctx, "email delivery failed after last attempt")
		return fmt.Errorf("send email: %w", sendErr)
	}
	if err := w.store.setError(ctx, id, sendErr); err != nil {
		return err
	}
	return fmt.Errorf("send email: %w", sendErr)
}
