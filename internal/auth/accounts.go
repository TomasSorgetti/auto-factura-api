package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TomasSorgetti/auto-factura-api/internal/audit"
	"github.com/TomasSorgetti/auto-factura-api/internal/db/sqlc"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

const (
	minPasswordBytes = 12
	maxPasswordBytes = 128
	uniqueViolation  = "23505"
)

// Accounts holds the out-of-band operations on the single admin user: creation
// and rescue. Every rescue revokes all sessions.
type Accounts struct {
	store  *Store
	hasher *secrets.PasswordHasher
	audit  Auditor
	now    func() time.Time
}

// NewAccounts builds the account operations; audit may be nil when no event must be recorded.
func NewAccounts(pool *pgxpool.Pool, hasher *secrets.PasswordHasher, audit Auditor) *Accounts {
	return &Accounts{store: NewStore(pool), hasher: hasher, audit: audit, now: time.Now}
}

// CreateUser creates the single admin user and returns its ID. It returns
// ErrAdminExists when one already exists.
func (a *Accounts) CreateUser(ctx context.Context, email, password string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Name != "" || addr.Address != email {
		return "", errors.New("invalid email address")
	}
	if err := checkPassword(password); err != nil {
		return "", err
	}
	if _, err := a.store.q.GetAdminUser(ctx); err == nil {
		return "", ErrAdminExists
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("get admin user: %w", err)
	}
	hash, err := a.hasher.Hash(ctx, password)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	u, err := a.store.q.CreateAdminUser(ctx, sqlc.CreateAdminUserParams{Email: email, PasswordHash: hash})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return "", ErrAdminExists
	}
	if err != nil {
		return "", fmt.Errorf("create admin user: %w", err)
	}
	a.record(ctx, audit.ActionUserCreated)
	return u.ID.String(), nil
}

func (a *Accounts) record(ctx context.Context, action audit.Action) {
	recordEvent(ctx, a.audit, audit.Event{Actor: cliActor, Action: action})
}

// ResetPassword replaces the password and revokes every session.
func (a *Accounts) ResetPassword(ctx context.Context, password string) error {
	if err := checkPassword(password); err != nil {
		return err
	}
	hash, err := a.hasher.Hash(ctx, password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	err = a.onAdmin(ctx, func(st *Store, id string) error {
		u, err := parseUUID(id)
		if err != nil {
			return err
		}
		if err := st.q.SetAdminPassword(ctx, sqlc.SetAdminPasswordParams{ID: u, PasswordHash: hash}); err != nil {
			return fmt.Errorf("set admin password: %w", err)
		}
		return st.revokeAll(ctx, id, a.now())
	})
	if err != nil {
		return err
	}
	a.record(ctx, audit.ActionPasswordReset)
	return nil
}

// ResetTOTP clears the TOTP secret and recovery codes and revokes every
// session; the next login enrolls again.
func (a *Accounts) ResetTOTP(ctx context.Context) error {
	err := a.onAdmin(ctx, func(st *Store, id string) error {
		u, err := parseUUID(id)
		if err != nil {
			return err
		}
		if err := st.q.ResetAdminTOTP(ctx, u); err != nil {
			return fmt.Errorf("reset admin totp: %w", err)
		}
		if err := st.q.DeleteRecoveryCodes(ctx, u); err != nil {
			return fmt.Errorf("delete recovery codes: %w", err)
		}
		return st.revokeAll(ctx, id, a.now())
	})
	if err != nil {
		return err
	}
	a.record(ctx, audit.ActionTOTPReset)
	return nil
}

func (a *Accounts) RevokeSessions(ctx context.Context) error {
	err := a.onAdmin(ctx, func(st *Store, id string) error {
		return st.revokeAll(ctx, id, a.now())
	})
	if err != nil {
		return err
	}
	a.record(ctx, audit.ActionSessionsRevoked)
	return nil
}

// PruneExpiredTokens deletes refresh tokens past their expiry and returns how many.
func (a *Accounts) PruneExpiredTokens(ctx context.Context) (int64, error) {
	var n int64
	err := a.onAdmin(ctx, func(st *Store, _ string) error {
		var err error
		n, err = st.q.DeleteExpiredRefreshTokens(ctx, tstz(a.now()))
		if err != nil {
			return fmt.Errorf("delete expired refresh tokens: %w", err)
		}
		return nil
	})
	return n, err
}

// onAdmin runs fn in a transaction holding the lock of the single admin row.
func (a *Accounts) onAdmin(ctx context.Context, fn func(st *Store, id string) error) error {
	u, err := a.store.q.GetAdminUser(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get admin user: %w", err)
	}
	id := u.ID.String()
	return a.store.InTx(ctx, func(st *Store) error {
		if _, err := st.lockSessionState(ctx, id); err != nil {
			return err
		}
		return fn(st, id)
	})
}

func checkPassword(p string) error {
	if len(p) < minPasswordBytes || len(p) > maxPasswordBytes {
		return ErrWeakPassword
	}
	return nil
}
