package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TomasSorgetti/auto-factura-api/internal/db/sqlc"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres"
)

// Store persists refresh tokens and session state through sqlc.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
	tx   pgx.Tx
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlc.New(pool)}
}

// InTx runs fn with a Store bound to a single transaction.
func (s *Store) InTx(ctx context.Context, fn func(*Store) error) error {
	return postgres.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&Store{pool: s.pool, q: s.q.WithTx(tx), tx: tx})
	})
}

// txn returns the transaction of a Store created by InTx; it is nil outside one.
func (s *Store) txn() pgx.Tx { return s.tx }

func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, fmt.Errorf("parse uuid: %w", err)
	}
	return u, nil
}

func newUUID() (pgtype.UUID, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return pgtype.UUID{}, fmt.Errorf("read random bytes: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return pgtype.UUID{Bytes: b, Valid: true}, nil
}

func tstz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func toRefreshRow(r sqlc.AdminRefreshToken) *refreshRow {
	row := &refreshRow{
		ID:        r.ID.String(),
		UserID:    r.UserID.String(),
		TokenHash: r.TokenHash,
		CreatedAt: r.CreatedAt.Time,
		ExpiresAt: r.ExpiresAt.Time,
	}
	if r.RevokedAt.Valid {
		t := r.RevokedAt.Time
		row.RevokedAt = &t
	}
	if r.ReplacedByID.Valid {
		id := r.ReplacedByID.String()
		row.ReplacedByID = &id
	}
	return row
}

func (s *Store) createRefresh(ctx context.Context, userID, jti pgtype.UUID, hash []byte, meta ClientMeta, createdAt, expiresAt time.Time) (string, error) {
	id, err := s.q.CreateRefreshToken(ctx, sqlc.CreateRefreshTokenParams{
		UserID:    userID,
		Jti:       jti,
		TokenHash: hash,
		Ip:        truncate(meta.IP, 64),
		UserAgent: truncate(meta.UserAgent, 500),
		ExpiresAt: tstz(expiresAt),
		CreatedAt: tstz(createdAt),
	})
	if err != nil {
		return "", fmt.Errorf("create refresh token: %w", err)
	}
	return id.String(), nil
}

// refreshByJTIForUpdate locks the row; it returns nil when none exists.
func (s *Store) refreshByJTIForUpdate(ctx context.Context, jti string) (*refreshRow, error) {
	u, err := parseUUID(jti)
	if err != nil {
		return nil, nil
	}
	r, err := s.q.GetRefreshTokenByJTIForUpdate(ctx, u)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get refresh token: %w", err)
	}
	return toRefreshRow(r), nil
}

func (s *Store) refreshByID(ctx context.Context, id string) (*refreshRow, error) {
	u, err := parseUUID(id)
	if err != nil {
		return nil, err
	}
	r, err := s.q.GetRefreshTokenByID(ctx, u)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get refresh token by id: %w", err)
	}
	return toRefreshRow(r), nil
}

const maxChain = 8

// chainTail follows replaced_by_id from start for at most maxChain hops and
// returns the last row reached (start itself when it has no replacement).
func (s *Store) chainTail(ctx context.Context, start *refreshRow) (*refreshRow, error) {
	cur := start
	for i := 0; i < maxChain && cur != nil && cur.ReplacedByID != nil; i++ {
		next, err := s.refreshByID(ctx, *cur.ReplacedByID)
		if err != nil {
			return nil, err
		}
		if next == nil {
			return cur, nil
		}
		cur = next
	}
	return cur, nil
}

func (s *Store) rotate(ctx context.Context, oldID string, at time.Time, newID string) error {
	o, err := parseUUID(oldID)
	if err != nil {
		return err
	}
	n, err := parseUUID(newID)
	if err != nil {
		return err
	}
	if err := s.q.RotateRefreshToken(ctx, sqlc.RotateRefreshTokenParams{ID: o, RevokedAt: tstz(at), ReplacedByID: n}); err != nil {
		return fmt.Errorf("rotate refresh token: %w", err)
	}
	return nil
}

func (s *Store) revoke(ctx context.Context, id string, at time.Time) error {
	u, err := parseUUID(id)
	if err != nil {
		return err
	}
	if err := s.q.RevokeRefreshToken(ctx, sqlc.RevokeRefreshTokenParams{ID: u, RevokedAt: tstz(at)}); err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	return nil
}

func (s *Store) revokeAll(ctx context.Context, userID string, at time.Time) error {
	u, err := parseUUID(userID)
	if err != nil {
		return err
	}
	if err := s.q.RevokeAdminSessions(ctx, sqlc.RevokeAdminSessionsParams{ID: u, SessionsRevokedAt: tstz(at)}); err != nil {
		return fmt.Errorf("revoke admin sessions: %w", err)
	}
	if err := s.q.RevokeAllRefreshTokens(ctx, sqlc.RevokeAllRefreshTokensParams{UserID: u, RevokedAt: tstz(at)}); err != nil {
		return fmt.Errorf("revoke refresh tokens: %w", err)
	}
	return nil
}

// RevokeAll invalidates every session of the user in one transaction.
func (s *Store) RevokeAll(ctx context.Context, userID string, at time.Time) error {
	return s.InTx(ctx, func(st *Store) error {
		return st.revokeAll(ctx, userID, at)
	})
}

// sessionsRevokedAt returns the user's sessions_revoked_at (nil when unset) or ErrNotFound.
func (s *Store) sessionsRevokedAt(ctx context.Context, userID string) (*time.Time, error) {
	u, err := parseUUID(userID)
	if err != nil {
		return nil, ErrNotFound
	}
	st, err := s.q.GetAdminSessionState(ctx, u)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get admin session state: %w", err)
	}
	return timePtr(st.SessionsRevokedAt), nil
}

// lockSessionState locks the admin_users row until the transaction ends. Every
// path that touches tokens takes this lock first, so lock order is always user
// then token.
func (s *Store) lockSessionState(ctx context.Context, userID string) (*time.Time, error) {
	u, err := parseUUID(userID)
	if err != nil {
		return nil, ErrNotFound
	}
	v, err := s.q.LockAdminSessionState(ctx, u)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock admin session state: %w", err)
	}
	return timePtr(v), nil
}

func timePtr(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit])
}

// Me returns the email and the unused recovery codes of the user.
func (s *Store) Me(ctx context.Context, userID string) (email string, recoveryCodesLeft int, err error) {
	u, err := s.userByID(ctx, userID)
	if err != nil {
		return "", 0, err
	}
	left, err := s.unusedRecoveryCodes(ctx, userID)
	if err != nil {
		return "", 0, err
	}
	return u.Email, left, nil
}
