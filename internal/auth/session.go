package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/audit"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

// ClientMeta describes the caller of a session operation.
type ClientMeta struct {
	IP        string
	UserAgent string
	DeviceID  string
}

type Principal struct {
	UserID string
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// IssuedSession is what HTTP turns into cookies. Values are logx.Secret so they never reach a log.
type IssuedSession struct {
	AccessToken      logx.Secret
	AccessExpiresAt  time.Time
	RefreshToken     logx.Secret
	RefreshExpiresAt time.Time
}

// Sessions issues, rotates, revokes and authenticates admin sessions.
type Sessions struct {
	store  *Store
	tokens *Tokens
	log    *slog.Logger
	audit  Auditor
}

func NewSessions(store *Store, tokens *Tokens, log *slog.Logger, audit Auditor) *Sessions {
	return &Sessions{store: store, tokens: tokens, log: log, audit: audit}
}

func (s *Sessions) mint(ctx context.Context, st *Store, userID string, meta ClientMeta) (IssuedSession, string, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return IssuedSession{}, "", err
	}
	jti, err := newUUID()
	if err != nil {
		return IssuedSession{}, "", err
	}
	access, accessExp, err := s.tokens.SignAccess(userID)
	if err != nil {
		return IssuedSession{}, "", err
	}
	refresh, refreshExp, err := s.tokens.SignRefresh(userID, jti.String())
	if err != nil {
		return IssuedSession{}, "", err
	}
	id, err := st.createRefresh(ctx, uid, jti, secrets.HashToken(refresh), meta, s.tokens.now(), refreshExp)
	if err != nil {
		return IssuedSession{}, "", err
	}
	return IssuedSession{
		AccessToken:      logx.Secret(access),
		AccessExpiresAt:  accessExp,
		RefreshToken:     logx.Secret(refresh),
		RefreshExpiresAt: refreshExp,
	}, id, nil
}

func (s *Sessions) Issue(ctx context.Context, userID string, meta ClientMeta) (IssuedSession, error) {
	out, _, err := s.mint(ctx, s.store, userID, meta)
	return out, err
}

func (s *Sessions) Refresh(ctx context.Context, raw string, meta ClientMeta) (IssuedSession, error) {
	sub, jti, err := s.tokens.VerifyRefresh(raw)
	if err != nil {
		return IssuedSession{}, err
	}
	var (
		out        IssuedSession
		replayedID string
	)
	err = s.store.InTx(ctx, func(st *Store) error {
		now := s.tokens.now()
		revokedAt, err := st.lockSessionState(ctx, sub)
		if errors.Is(err, ErrNotFound) {
			return ErrSessionInvalid
		}
		if err != nil {
			return err
		}
		row, err := st.refreshByJTIForUpdate(ctx, jti)
		if err != nil {
			return err
		}
		if !matches(row, sub, raw) {
			return ErrSessionInvalid
		}
		if revokedAt != nil && !revokedAt.Before(row.CreatedAt) {
			return ErrSessionInvalid
		}
		var replacement, tail *refreshRow
		if row.ReplacedByID != nil {
			if replacement, err = st.refreshByID(ctx, *row.ReplacedByID); err != nil {
				return err
			}
			if tail, err = st.chainTail(ctx, replacement); err != nil {
				return err
			}
		}
		verdict := classifyRefresh(row, replacement, now)
		if verdict == RefreshSuperseded && tail != nil && tail.RevokedAt != nil && tail.ReplacedByID == nil {
			verdict = RefreshRevoked
		}
		switch verdict {
		case RefreshActive:
			issued, newID, err := s.mint(ctx, st, sub, meta)
			if err != nil {
				return err
			}
			if err := st.rotate(ctx, row.ID, now, newID); err != nil {
				return err
			}
			out = issued
			return nil
		case RefreshSuperseded:
			issued, _, err := s.mint(ctx, st, sub, meta)
			if err != nil {
				return err
			}
			out = issued
			return nil
		case RefreshReplayed:
			if err := st.revokeAll(ctx, sub, now); err != nil {
				return err
			}
			replayedID = row.ID
			return nil
		case RefreshRevoked, RefreshUnknown:
			return ErrSessionInvalid
		default:
			return ErrSessionInvalid
		}
	})
	if err != nil {
		return IssuedSession{}, err
	}
	if replayedID != "" {
		s.log.WarnContext(ctx, "refresh token replayed", "token_id", replayedID)
		recordEvent(ctx, s.audit, audit.Event{Actor: adminActor(sub, meta.IP), Action: audit.ActionRefreshReplay})
		return IssuedSession{}, ErrSessionInvalid
	}
	return out, nil
}

func matches(row *refreshRow, sub, raw string) bool {
	return row != nil && row.UserID == sub &&
		subtle.ConstantTimeCompare(row.TokenHash, secrets.HashToken(raw)) == 1
}

// Logout revokes the presented refresh token. An invalid or unknown token is not an error.
func (s *Sessions) Logout(ctx context.Context, raw string, meta ClientMeta) error {
	sub, jti, err := s.tokens.VerifyRefresh(raw)
	if err != nil {
		return nil
	}
	revoked := false
	err = s.store.InTx(ctx, func(st *Store) error {
		if _, err := st.lockSessionState(ctx, sub); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil
			}
			return err
		}
		row, err := st.refreshByJTIForUpdate(ctx, jti)
		if err != nil {
			return err
		}
		if !matches(row, sub, raw) {
			return nil
		}
		if err := st.revoke(ctx, row.ID, s.tokens.now()); err != nil {
			return err
		}
		revoked = true
		return nil
	})
	if err != nil {
		return err
	}
	if revoked {
		recordEvent(ctx, s.audit, audit.Event{Actor: adminActor(sub, meta.IP), Action: audit.ActionLogout})
	}
	return nil
}

func (s *Sessions) Authenticate(ctx context.Context, rawAccess string) (Principal, error) {
	sub, iat, err := s.tokens.VerifyAccess(rawAccess)
	if err != nil {
		return Principal{}, err
	}
	revokedAt, err := s.store.sessionsRevokedAt(ctx, sub)
	if errors.Is(err, ErrNotFound) {
		return Principal{}, ErrSessionInvalid
	}
	if err != nil {
		return Principal{}, err
	}
	if revokedAt != nil && iat.Before(revokedAt.Truncate(time.Second)) {
		return Principal{}, ErrSessionInvalid
	}
	return Principal{UserID: sub}, nil
}

func (s *Sessions) RevokeAll(ctx context.Context, userID string) error {
	return s.store.RevokeAll(ctx, userID, s.tokens.now())
}
