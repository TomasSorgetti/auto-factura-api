package emails

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/resend/resend-go/v4"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
)

const defaultRetryAfter = 60 * time.Second

// ResendSender sends through the Resend API with its own bounded http.Client.
type ResendSender struct {
	client *resend.Client
	from   string
}

func NewResendSender(apiKey logx.Secret, from string) *ResendSender {
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       90 * time.Second,
		},
	}
	return &ResendSender{client: resend.NewCustomClient(httpClient, apiKey.Reveal()), from: from}
}

// Send maps provider failures to PermanentError and RetryAfterError; anything else is returned as a plain error.
func (s *ResendSender) Send(ctx context.Context, m Message) (string, error) {
	resp, err := s.client.Emails.SendWithOptions(ctx, &resend.SendEmailRequest{
		From:    s.from,
		To:      m.To,
		Cc:      m.Cc,
		Subject: m.Subject,
		Html:    m.HTML,
		Text:    m.Text,
	}, &resend.SendEmailOptions{IdempotencyKey: m.IdempotencyKey})
	if err != nil {
		return "", classifyResendError(err)
	}
	return resp.Id, nil
}

func classifyResendError(err error) error {
	var rl *resend.RateLimitError
	if errors.As(err, &rl) {
		after := defaultRetryAfter
		if secs, perr := strconv.Atoi(rl.RetryAfter); perr == nil && secs > 0 {
			after = time.Duration(secs) * time.Second
		}
		return &RetryAfterError{After: after, Err: err}
	}
	var re *resend.ResendError
	if errors.As(err, &re) {
		switch re.StatusCode {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity:
			return &PermanentError{Err: err}
		}
	}
	return fmt.Errorf("resend send: %w", err)
}

// NewSender returns the Resend sender, or a LogSender when apiKey is empty (allowed only outside production).
func NewSender(apiKey logx.Secret, from string, log *slog.Logger) Sender {
	if apiKey == "" {
		return LogSender{Log: log}
	}
	return NewResendSender(apiKey, from)
}
