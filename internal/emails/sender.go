package emails

import (
	"context"
	"log/slog"
)

type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}

// LogSender logs that an email was skipped. It is the sender when RESEND_API_KEY is empty outside production.
type LogSender struct{ Log *slog.Logger }

func (s LogSender) Send(ctx context.Context, m Message) error {
	s.Log.InfoContext(ctx, "email skipped: no provider", "subject", m.Subject)
	return nil
}
