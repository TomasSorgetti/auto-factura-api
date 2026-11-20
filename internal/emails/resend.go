package emails

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/resend/resend-go/v4"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
)

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

func (s *ResendSender) Send(ctx context.Context, m Message) error {
	_, err := s.client.Emails.SendWithContext(ctx, &resend.SendEmailRequest{
		From:    s.from,
		To:      []string{m.To},
		Subject: m.Subject,
		Html:    m.HTML,
		Text:    m.Text,
	})
	if err != nil {
		return fmt.Errorf("resend send: %w", err)
	}
	return nil
}
