package clients

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TomasSorgetti/auto-factura-api/internal/audit"
	"github.com/TomasSorgetti/auto-factura-api/internal/db/sqlc"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

const (
	entityClient  = "client"
	entityContact = "client_contact"
)

type Service struct {
	pool  *pgxpool.Pool
	codec *Codec
}

func NewService(pool *pgxpool.Pool, codec *Codec) *Service {
	return &Service{pool: pool, codec: codec}
}

// List never decrypts anything.
func (s *Service) List(ctx context.Context, p ListParams) (Page, error) {
	p, err := p.Validate()
	if err != nil {
		return Page{}, err
	}
	q := sqlc.New(s.pool)
	prefix, pattern := queryFilter(p.Query)
	total, err := q.CountClients(ctx, sqlc.CountClientsParams{Archived: p.Archived, CuitPrefix: prefix, NamePattern: pattern})
	if err != nil {
		return Page{}, fmt.Errorf("count clients: %w", err)
	}
	rows, err := q.ListClients(ctx, sqlc.ListClientsParams{
		Archived: p.Archived, CuitPrefix: prefix, NamePattern: pattern,
		RowOffset: int32(p.Offset), RowLimit: int32(p.Limit),
	})
	if err != nil {
		return Page{}, fmt.Errorf("list clients: %w", err)
	}
	items := make([]Summary, 0, len(rows))
	for _, r := range rows {
		items = append(items, Summary{
			ID: r.ID.String(), LegalName: r.LegalName, CUIT: r.Cuit, TaxCondition: TaxCondition(r.TaxCondition),
			ArchivedAt: optTime(r.ArchivedAt), ContactsCount: int(r.ContactsCount),
		})
	}
	return Page{Items: items, Total: int(total)}, nil
}

func (s *Service) Get(ctx context.Context, id string) (Detail, error) {
	uid, err := parseID(id)
	if err != nil {
		return Detail{}, err
	}
	q := sqlc.New(s.pool)
	row, err := q.GetClient(ctx, uid)
	if err != nil {
		if mapped := mapErr(err); mapped != err {
			return Detail{}, mapped
		}
		return Detail{}, fmt.Errorf("get client %s: %w", id, err)
	}
	return s.detail(ctx, q, row)
}

func (s *Service) sealNotes(id, notes string) ([]byte, error) {
	if notes == "" {
		return nil, nil
	}
	return s.codec.seal(fieldNotes, id, notes)
}

func wrapDB(err error, op string) error {
	if mapped := mapErr(err); mapped != err {
		return mapped
	}
	return fmt.Errorf("%s: %w", op, err)
}

func (s *Service) Create(ctx context.Context, actor audit.Actor, in ClientInput) (Detail, error) {
	f, err := in.Validate()
	if err != nil {
		return Detail{}, err
	}
	id, err := secrets.NewID()
	if err != nil {
		return Detail{}, fmt.Errorf("new client id: %w", err)
	}
	uid, err := parseID(id)
	if err != nil {
		return Detail{}, fmt.Errorf("parse new client id: %w", err)
	}
	enc, err := s.sealNotes(id, f.Notes)
	if err != nil {
		return Detail{}, err
	}
	var out Detail
	err = postgres.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		row, err := q.CreateClient(ctx, sqlc.CreateClientParams{
			ID: uid, LegalName: f.LegalName, Cuit: f.CUIT, TaxCondition: string(f.TaxCondition),
			Address: f.Address, NotesEnc: enc,
		})
		if err != nil {
			return wrapDB(err, "create client")
		}
		if out, err = s.detail(ctx, q, row); err != nil {
			return err
		}
		return recordEvent(ctx, tx, audit.Event{
			Actor: actor, Action: audit.ActionClientCreated, EntityType: entityClient, EntityID: id,
			Data: map[string]string{},
		})
	})
	if err != nil {
		return Detail{}, err
	}
	return out, nil
}

func recordEvent(ctx context.Context, tx pgx.Tx, e audit.Event) error {
	if err := audit.Record(ctx, tx, e); err != nil {
		return fmt.Errorf("audit %s: %w", e.Action, err)
	}
	return nil
}

func lockClient(ctx context.Context, q *sqlc.Queries, id pgtype.UUID) (sqlc.Client, error) {
	row, err := q.GetClientForUpdate(ctx, id)
	if err != nil {
		return sqlc.Client{}, wrapDB(err, "lock client")
	}
	return row, nil
}

func lockActive(ctx context.Context, q *sqlc.Queries, id pgtype.UUID) (sqlc.Client, error) {
	row, err := lockClient(ctx, q, id)
	if err != nil {
		return sqlc.Client{}, err
	}
	if row.ArchivedAt.Valid {
		return sqlc.Client{}, ErrArchived
	}
	return row, nil
}

func (s *Service) Update(ctx context.Context, actor audit.Actor, id string, in ClientInput) (Detail, error) {
	f, err := in.Validate()
	if err != nil {
		return Detail{}, err
	}
	uid, err := parseID(id)
	if err != nil {
		return Detail{}, err
	}
	id = uid.String()
	enc, err := s.sealNotes(id, f.Notes)
	if err != nil {
		return Detail{}, err
	}
	var out Detail
	err = postgres.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		row, err := lockActive(ctx, q, uid)
		if err != nil {
			return err
		}
		current, err := s.clientFromRow(row)
		if err != nil {
			return err
		}
		changed := diffClient(current.ClientFields, f)
		if changed == "" {
			out, err = s.detail(ctx, q, row)
			return err
		}
		updated, err := q.UpdateClient(ctx, sqlc.UpdateClientParams{
			ID: uid, LegalName: f.LegalName, Cuit: f.CUIT, TaxCondition: string(f.TaxCondition),
			Address: f.Address, NotesEnc: enc,
		})
		if err != nil {
			return wrapDB(err, "update client "+id)
		}
		if out, err = s.detail(ctx, q, updated); err != nil {
			return err
		}
		return recordEvent(ctx, tx, audit.Event{
			Actor: actor, Action: audit.ActionClientUpdated, EntityType: entityClient, EntityID: id,
			Data: map[string]string{"fields": changed},
		})
	})
	if err != nil {
		return Detail{}, err
	}
	return out, nil
}

// Archive is idempotent: archiving an archived client returns it without an event.
func (s *Service) Archive(ctx context.Context, actor audit.Actor, id string) (Detail, error) {
	return s.setArchived(ctx, actor, id, true)
}

// Unarchive is idempotent: unarchiving an active client returns it without an event.
func (s *Service) Unarchive(ctx context.Context, actor audit.Actor, id string) (Detail, error) {
	return s.setArchived(ctx, actor, id, false)
}

func (s *Service) setArchived(ctx context.Context, actor audit.Actor, id string, archive bool) (Detail, error) {
	uid, err := parseID(id)
	if err != nil {
		return Detail{}, err
	}
	id = uid.String()
	var out Detail
	err = postgres.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		row, err := lockClient(ctx, q, uid)
		if err != nil {
			return err
		}
		if row.ArchivedAt.Valid == archive {
			out, err = s.detail(ctx, q, row)
			return err
		}
		action := audit.ActionClientUnarchived
		if archive {
			action = audit.ActionClientArchived
			row, err = q.ArchiveClient(ctx, uid)
		} else {
			row, err = q.UnarchiveClient(ctx, uid)
		}
		if err != nil {
			return wrapDB(err, "set archived "+id)
		}
		if out, err = s.detail(ctx, q, row); err != nil {
			return err
		}
		return recordEvent(ctx, tx, audit.Event{
			Actor: actor, Action: action, EntityType: entityClient, EntityID: id, Data: map[string]string{},
		})
	})
	if err != nil {
		return Detail{}, err
	}
	return out, nil
}

func (s *Service) sealContact(id string, f ContactFields) (name, email, index []byte, err error) {
	if name, err = s.codec.seal(fieldContactName, id, f.Name); err != nil {
		return nil, nil, nil, err
	}
	if email, err = s.codec.seal(fieldContactEmail, id, f.Email); err != nil {
		return nil, nil, nil, err
	}
	return name, email, s.codec.emailIndex(f.Email), nil
}

func (s *Service) CreateContact(ctx context.Context, actor audit.Actor, clientID string, in ContactInput) (Contact, error) {
	f, err := in.Validate()
	if err != nil {
		return Contact{}, err
	}
	cid, err := parseID(clientID)
	if err != nil {
		return Contact{}, err
	}
	clientID = cid.String()
	id, err := secrets.NewID()
	if err != nil {
		return Contact{}, fmt.Errorf("new contact id: %w", err)
	}
	uid, err := parseID(id)
	if err != nil {
		return Contact{}, fmt.Errorf("parse new contact id: %w", err)
	}
	nameEnc, emailEnc, index, err := s.sealContact(id, f)
	if err != nil {
		return Contact{}, err
	}
	var out Contact
	err = postgres.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if _, err := lockActive(ctx, q, cid); err != nil {
			return err
		}
		n, err := q.CountContacts(ctx, cid)
		if err != nil {
			return fmt.Errorf("count contacts %s: %w", clientID, err)
		}
		if n >= MaxContacts {
			return ErrContactLimit
		}
		row, err := q.CreateContact(ctx, sqlc.CreateContactParams{
			ID: uid, ClientID: cid, NameEnc: nameEnc, EmailEnc: emailEnc, EmailHmac: index,
			Role: string(f.Role), PortalAccess: f.PortalAccess,
		})
		if err != nil {
			return wrapDB(err, "create contact")
		}
		if out, err = s.contactFromRow(row); err != nil {
			return err
		}
		return recordEvent(ctx, tx, audit.Event{
			Actor: actor, Action: audit.ActionContactCreated, EntityType: entityContact, EntityID: id,
			Data: map[string]string{"clientId": clientID, "role": string(f.Role)},
		})
	})
	if err != nil {
		return Contact{}, err
	}
	return out, nil
}

func (s *Service) UpdateContact(ctx context.Context, actor audit.Actor, clientID, contactID string, in ContactInput) (Contact, error) {
	f, err := in.Validate()
	if err != nil {
		return Contact{}, err
	}
	cid, err := parseID(clientID)
	if err != nil {
		return Contact{}, err
	}
	clientID = cid.String()
	uid, err := parseID(contactID)
	if err != nil {
		return Contact{}, err
	}
	id := uid.String()
	nameEnc, emailEnc, index, err := s.sealContact(id, f)
	if err != nil {
		return Contact{}, err
	}
	var out Contact
	err = postgres.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if _, err := lockActive(ctx, q, cid); err != nil {
			return err
		}
		row, err := q.GetContact(ctx, sqlc.GetContactParams{ID: uid, ClientID: cid})
		if err != nil {
			return wrapDB(err, "get contact "+id)
		}
		current, err := s.contactFromRow(row)
		if err != nil {
			return err
		}
		changed := diffContact(current.ContactFields, f)
		if changed == "" {
			out = current
			return nil
		}
		row, err = q.UpdateContact(ctx, sqlc.UpdateContactParams{
			ID: uid, ClientID: cid, NameEnc: nameEnc, EmailEnc: emailEnc, EmailHmac: index,
			Role: string(f.Role), PortalAccess: f.PortalAccess,
		})
		if err != nil {
			return wrapDB(err, "update contact "+id)
		}
		if out, err = s.contactFromRow(row); err != nil {
			return err
		}
		return recordEvent(ctx, tx, audit.Event{
			Actor: actor, Action: audit.ActionContactUpdated, EntityType: entityContact, EntityID: id,
			Data: map[string]string{"clientId": clientID, "fields": changed},
		})
	})
	if err != nil {
		return Contact{}, err
	}
	return out, nil
}

func (s *Service) DeleteContact(ctx context.Context, actor audit.Actor, clientID, contactID string) error {
	cid, err := parseID(clientID)
	if err != nil {
		return err
	}
	clientID = cid.String()
	uid, err := parseID(contactID)
	if err != nil {
		return err
	}
	contactID = uid.String()
	return postgres.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if _, err := lockActive(ctx, q, cid); err != nil {
			return err
		}
		n, err := q.DeleteContact(ctx, sqlc.DeleteContactParams{ID: uid, ClientID: cid})
		if err != nil {
			return fmt.Errorf("delete contact %s: %w", contactID, err)
		}
		if n == 0 {
			return ErrNotFound
		}
		return recordEvent(ctx, tx, audit.Event{
			Actor: actor, Action: audit.ActionContactDeleted, EntityType: entityContact, EntityID: uid.String(),
			Data: map[string]string{"clientId": clientID},
		})
	})
}
