package emails

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TomasSorgetti/auto-factura-api/internal/db/sqlc"
)

const maxLastError = 500

type deliveryStatus string

const (
	statusPending deliveryStatus = "pending"
	statusSent    deliveryStatus = "sent"
	statusFailed  deliveryStatus = "failed"
)

var errDeliveryNotFound = errors.New("email delivery not found")

type delivery struct {
	ID       string
	Status   deliveryStatus
	Message  Message
	Attempts int32
}

type store struct {
	q *sqlc.Queries
}

func newStore(pool *pgxpool.Pool) *store { return &store{q: sqlc.New(pool)} }

func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, fmt.Errorf("parse uuid: %w", err)
	}
	return u, nil
}

func insertDelivery(ctx context.Context, tx pgx.Tx, m Message) (string, error) {
	cc := m.Cc
	if cc == nil {
		cc = []string{}
	}
	id, err := sqlc.New(tx).InsertEmailDelivery(ctx, sqlc.InsertEmailDeliveryParams{
		Kind:     string(m.Kind),
		ToAddrs:  m.To,
		CcAddrs:  cc,
		Subject:  m.Subject,
		HtmlBody: m.HTML,
		TextBody: m.Text,
	})
	if err != nil {
		return "", fmt.Errorf("insert email delivery: %w", err)
	}
	return id.String(), nil
}

func (s *store) get(ctx context.Context, id string) (delivery, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return delivery{}, errDeliveryNotFound
	}
	row, err := s.q.GetEmailDelivery(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return delivery{}, errDeliveryNotFound
	}
	if err != nil {
		return delivery{}, fmt.Errorf("get email delivery: %w", err)
	}
	return delivery{
		ID:       row.ID.String(),
		Status:   deliveryStatus(row.Status),
		Attempts: row.Attempts,
		Message: Message{
			Kind: Kind(row.Kind), To: row.ToAddrs, Cc: row.CcAddrs,
			Subject: row.Subject, HTML: row.HtmlBody, Text: row.TextBody,
		},
	}, nil
}

func (s *store) incrementAttempts(ctx context.Context, id string) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	if err := s.q.IncrementEmailDeliveryAttempts(ctx, uid); err != nil {
		return fmt.Errorf("increment email delivery attempts: %w", err)
	}
	return nil
}

func (s *store) markSent(ctx context.Context, id, providerID string) (bool, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return false, err
	}
	n, err := s.q.MarkEmailDeliverySent(ctx, sqlc.MarkEmailDeliverySentParams{ID: uid, ProviderMessageID: &providerID})
	if err != nil {
		return false, fmt.Errorf("mark email delivery sent: %w", err)
	}
	return n > 0, nil
}

func (s *store) markFailed(ctx context.Context, id string, cause error) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	msg := truncateError(cause)
	if _, err := s.q.MarkEmailDeliveryFailed(ctx, sqlc.MarkEmailDeliveryFailedParams{ID: uid, LastError: &msg}); err != nil {
		return fmt.Errorf("mark email delivery failed: %w", err)
	}
	return nil
}

func (s *store) setError(ctx context.Context, id string, cause error) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	msg := truncateError(cause)
	if err := s.q.SetEmailDeliveryError(ctx, sqlc.SetEmailDeliveryErrorParams{ID: uid, LastError: &msg}); err != nil {
		return fmt.Errorf("set email delivery error: %w", err)
	}
	return nil
}

func truncateError(err error) string {
	s := strings.ToValidUTF8(err.Error(), "")
	if len(s) <= maxLastError {
		return s
	}
	s = s[:maxLastError]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
