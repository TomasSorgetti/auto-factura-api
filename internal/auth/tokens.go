package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
)

const (
	issuer          = "autofactura"
	audienceAccess  = "admin-access"
	audienceRefresh = "admin-refresh"
	leeway          = 30 * time.Second
	refreshGrace    = 10 * time.Minute
)

// TokensConfig configures JWT signing. Access and refresh tokens use different
// keys and audiences so one can never validate as the other.
type TokensConfig struct {
	AccessSecret, RefreshSecret logx.Secret
	AccessTTL, RefreshTTL       time.Duration
	Now                         func() time.Time
}

// Tokens signs and validates HS256 access and refresh JWTs.
type Tokens struct {
	accessKey, refreshKey []byte
	accessTTL, refreshTTL time.Duration
	now                   func() time.Time
}

func NewTokens(cfg TokensConfig) (*Tokens, error) {
	if cfg.AccessSecret.Reveal() == "" || cfg.RefreshSecret.Reveal() == "" {
		return nil, errors.New("jwt secrets must not be empty")
	}
	if cfg.AccessTTL <= 0 || cfg.RefreshTTL <= 0 {
		return nil, errors.New("jwt ttl must be positive")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Tokens{
		accessKey:  []byte(cfg.AccessSecret.Reveal()),
		refreshKey: []byte(cfg.RefreshSecret.Reveal()),
		accessTTL:  cfg.AccessTTL,
		refreshTTL: cfg.RefreshTTL,
		now:        now,
	}, nil
}

func (t *Tokens) sign(key []byte, aud, sub, jti string, ttl time.Duration) (string, time.Time, error) {
	now := t.now()
	exp := now.Add(ttl)
	claims := jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   sub,
		Audience:  jwt.ClaimStrings{aud},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
		ID:        jti,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign jwt: %w", err)
	}
	return signed, exp, nil
}

func (t *Tokens) SignAccess(userID string) (string, time.Time, error) {
	return t.sign(t.accessKey, audienceAccess, userID, "", t.accessTTL)
}

func (t *Tokens) SignRefresh(userID, jti string) (string, time.Time, error) {
	return t.sign(t.refreshKey, audienceRefresh, userID, jti, t.refreshTTL)
}

func (t *Tokens) verify(raw string, key []byte, aud string) (*jwt.RegisteredClaims, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(raw, &claims,
		func(*jwt.Token) (any, error) { return key, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(aud),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(leeway),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil || claims.Subject == "" || claims.IssuedAt == nil {
		return nil, ErrSessionInvalid
	}
	return &claims, nil
}

// VerifyAccess returns the subject and issue time of a valid access token.
func (t *Tokens) VerifyAccess(raw string) (string, time.Time, error) {
	c, err := t.verify(raw, t.accessKey, audienceAccess)
	if err != nil {
		return "", time.Time{}, err
	}
	return c.Subject, c.IssuedAt.Time, nil
}

// VerifyRefresh returns the subject and jti of a valid refresh token.
func (t *Tokens) VerifyRefresh(raw string) (string, string, error) {
	c, err := t.verify(raw, t.refreshKey, audienceRefresh)
	if err != nil || c.ID == "" {
		return "", "", ErrSessionInvalid
	}
	return c.Subject, c.ID, nil
}
