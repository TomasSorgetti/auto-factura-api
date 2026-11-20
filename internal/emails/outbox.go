package emails

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const (
	maxRecipients = 50
	maxBodyBytes  = 512 * 1024
	maxSubject    = 998
)

var ErrInvalidMessage = errors.New("invalid email message")

type jobInserter interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// Outbox enqueues emails inside the caller's transaction.
type Outbox struct{ inserter jobInserter }

func NewOutbox(c jobInserter) *Outbox { return &Outbox{inserter: c} }

// Enqueue validates m, inserts the delivery and its email.send job in tx, and returns the delivery id.
func (o *Outbox) Enqueue(ctx context.Context, tx pgx.Tx, m Message) (string, error) {
	if err := m.validate(); err != nil {
		return "", err
	}
	id, err := insertDelivery(ctx, tx, m)
	if err != nil {
		return "", err
	}
	if _, err := o.inserter.InsertTx(ctx, tx, SendArgs{DeliveryID: id}, nil); err != nil {
		return "", fmt.Errorf("insert email.send job: %w", err)
	}
	return id, nil
}

func (m Message) validate() error {
	switch m.Kind {
	case KindDeviceAlert:
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidMessage, m.Kind)
	}
	if len(m.To) < 1 || len(m.To) > maxRecipients {
		return fmt.Errorf("%w: to must have 1 to %d addresses", ErrInvalidMessage, maxRecipients)
	}
	if len(m.Cc) > maxRecipients {
		return fmt.Errorf("%w: cc must have at most %d addresses", ErrInvalidMessage, maxRecipients)
	}
	for _, a := range append(append([]string(nil), m.To...), m.Cc...) {
		parsed, err := mail.ParseAddress(a)
		if err != nil || parsed.Name != "" || parsed.Address != a {
			return fmt.Errorf("%w: address must be a bare email", ErrInvalidMessage)
		}
	}
	if m.Subject == "" || len(m.Subject) > maxSubject || strings.ContainsAny(m.Subject, "\r\n") {
		return fmt.Errorf("%w: subject must be 1 to %d bytes without line breaks", ErrInvalidMessage, maxSubject)
	}
	if len(m.HTML) > maxBodyBytes || len(m.Text) > maxBodyBytes {
		return fmt.Errorf("%w: body exceeds %d bytes", ErrInvalidMessage, maxBodyBytes)
	}
	if m.IdempotencyKey != "" {
		return fmt.Errorf("%w: idempotency key is set by the worker", ErrInvalidMessage)
	}
	return nil
}
