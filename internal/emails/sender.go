package emails

import (
	"context"
	"log/slog"
	"time"
)

type Kind string

const KindDeviceAlert Kind = "device_alert"

type Message struct {
	Kind    Kind
	To      []string
	Cc      []string
	Subject string
	HTML    string
	Text    string
	// IdempotencyKey is set by the worker, never by callers.
	IdempotencyKey string
}

// Sender sends a message and returns the provider message id.
type Sender interface {
	Send(ctx context.Context, m Message) (string, error)
}

// PermanentError means retrying cannot succeed (invalid address, rejected content).
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return "permanent email error: " + e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// RetryAfterError asks to retry after the given delay (provider rate limit).
type RetryAfterError struct {
	After time.Duration
	Err   error
}

func (e *RetryAfterError) Error() string {
	return "email retry after " + e.After.String() + ": " + e.Err.Error()
}
func (e *RetryAfterError) Unwrap() error { return e.Err }

// LogSender logs that an email was skipped. It is the sender when RESEND_API_KEY is empty outside production.
type LogSender struct{ Log *slog.Logger }

func (s LogSender) Send(ctx context.Context, m Message) (string, error) {
	s.Log.InfoContext(ctx, "email skipped: no provider", "kind", string(m.Kind))
	return "", nil
}
