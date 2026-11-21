package clients

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/TomasSorgetti/auto-factura-api/internal/cuit"
	"github.com/TomasSorgetti/auto-factura-api/internal/db/sqlc"
)

const (
	pgUniqueViolation  = "23505"
	constraintCUIT     = "clients_cuit_key"
	constraintContacts = "client_contacts_client_id_email_hmac_key"
)

func parseID(id string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(id); err != nil || !u.Valid {
		return pgtype.UUID{}, ErrNotFound
	}
	return u, nil
}

// mapErr translates pgx and constraint errors to domain errors; anything else passes through unchanged.
func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		switch pgErr.ConstraintName {
		case constraintCUIT:
			return ErrCUITTaken
		case constraintContacts:
			return ErrEmailTaken
		}
	}
	return err
}

func optTime(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func (s *Service) clientFromRow(r sqlc.Client) (Client, error) {
	id := r.ID.String()
	notes := ""
	if r.NotesEnc != nil {
		var err error
		if notes, err = s.codec.open(fieldNotes, id, r.NotesEnc); err != nil {
			return Client{}, err
		}
	}
	return Client{
		ID: id,
		ClientFields: ClientFields{
			LegalName:    r.LegalName,
			CUIT:         r.Cuit,
			TaxCondition: TaxCondition(r.TaxCondition),
			Address:      r.Address,
			Notes:        notes,
		},
		ArchivedAt: optTime(r.ArchivedAt),
		CreatedAt:  r.CreatedAt.Time,
		UpdatedAt:  r.UpdatedAt.Time,
	}, nil
}

func (s *Service) contactFromRow(r sqlc.ClientContact) (Contact, error) {
	id := r.ID.String()
	name, err := s.codec.open(fieldContactName, id, r.NameEnc)
	if err != nil {
		return Contact{}, err
	}
	email, err := s.codec.open(fieldContactEmail, id, r.EmailEnc)
	if err != nil {
		return Contact{}, err
	}
	return Contact{
		ID:       id,
		ClientID: r.ClientID.String(),
		ContactFields: ContactFields{
			Name: name, Email: email, Role: Role(r.Role), PortalAccess: r.PortalAccess,
		},
		CreatedAt: r.CreatedAt.Time,
		UpdatedAt: r.UpdatedAt.Time,
	}, nil
}

func (s *Service) detail(ctx context.Context, q *sqlc.Queries, row sqlc.Client) (Detail, error) {
	c, err := s.clientFromRow(row)
	if err != nil {
		return Detail{}, err
	}
	rows, err := q.ListContacts(ctx, row.ID)
	if err != nil {
		return Detail{}, fmt.Errorf("list contacts %s: %w", c.ID, err)
	}
	contacts := make([]Contact, 0, len(rows))
	for _, r := range rows {
		ct, err := s.contactFromRow(r)
		if err != nil {
			return Detail{}, err
		}
		contacts = append(contacts, ct)
	}
	return Detail{Client: c, Contacts: contacts}, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// queryFilter sends digit-only queries (up to 11, dashes ignored) to the CUIT prefix and the rest to the name.
func queryFilter(q string) (cuitPrefix, namePattern *string) {
	if q == "" {
		return nil, nil
	}
	if d := cuit.Normalize(q); d != "" && len(d) <= 11 && strings.Trim(d, "0123456789") == "" {
		return &d, nil
	}
	p := "%" + escapeLike(q) + "%"
	return nil, &p
}
