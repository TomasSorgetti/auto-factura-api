package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
)

const (
	testAccessKey  = "access-secret-access-secret-access"
	testRefreshKey = "refresh-secret-refresh-secret-refre"
	testUser       = "11111111-1111-4111-8111-111111111111"
	testJTI        = "22222222-2222-4222-8222-222222222222"
)

var fixedNow = time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

func newTestTokens(t *testing.T, now func() time.Time, access, refresh string) *Tokens {
	t.Helper()
	tk, err := NewTokens(TokensConfig{
		AccessSecret:  logx.Secret(access),
		RefreshSecret: logx.Secret(refresh),
		AccessTTL:     15 * time.Minute,
		RefreshTTL:    7 * 24 * time.Hour,
		Now:           now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestTokens(t *testing.T) {
	t.Parallel()
	clock := func() time.Time { return fixedNow }
	tk := newTestTokens(t, clock, testAccessKey, testRefreshKey)

	access, _, err := tk.SignAccess(testUser)
	if err != nil {
		t.Fatal(err)
	}
	refresh, _, err := tk.SignRefresh(testUser, testJTI)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		sub, iat, err := tk.VerifyAccess(access)
		if err != nil || sub != testUser || !iat.Equal(fixedNow) {
			t.Fatalf("access: %v %s %v", err, sub, iat)
		}
		sub, jti, err := tk.VerifyRefresh(refresh)
		if err != nil || sub != testUser || jti != testJTI {
			t.Fatalf("refresh: %v %s %s", err, sub, jti)
		}
	})

	t.Run("expired", func(t *testing.T) {
		t.Parallel()
		late := newTestTokens(t, func() time.Time { return fixedNow.Add(time.Hour) }, testAccessKey, testRefreshKey)
		if _, _, err := late.VerifyAccess(access); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("alg none", func(t *testing.T) {
		t.Parallel()
		s, err := jwt.NewWithClaims(jwt.SigningMethodNone, accessClaims()).SignedString(jwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := tk.VerifyAccess(s); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("alg HS384", func(t *testing.T) {
		t.Parallel()
		s, err := jwt.NewWithClaims(jwt.SigningMethodHS384, accessClaims()).SignedString([]byte(testAccessKey))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := tk.VerifyAccess(s); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("refresh as access", func(t *testing.T) {
		t.Parallel()
		if _, _, err := tk.VerifyAccess(refresh); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("access as refresh", func(t *testing.T) {
		t.Parallel()
		if _, _, err := tk.VerifyRefresh(access); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("same key still rejected by audience", func(t *testing.T) {
		t.Parallel()
		same := newTestTokens(t, clock, testAccessKey, testAccessKey)
		r, _, err := same.SignRefresh(testUser, testJTI)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := same.VerifyAccess(r); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("other key", func(t *testing.T) {
		t.Parallel()
		other := newTestTokens(t, clock, "another-access-secret-another-acc", testRefreshKey)
		s, _, err := other.SignAccess(testUser)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := tk.VerifyAccess(s); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("iat in the future", func(t *testing.T) {
		t.Parallel()
		future := newTestTokens(t, func() time.Time { return fixedNow.Add(2 * time.Minute) }, testAccessKey, testRefreshKey)
		s, _, err := future.SignAccess(testUser)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := tk.VerifyAccess(s); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("got %v", err)
		}
	})
}

func accessClaims() jwt.RegisteredClaims {
	return jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   testUser,
		Audience:  jwt.ClaimStrings{audienceAccess},
		IssuedAt:  jwt.NewNumericDate(fixedNow),
		ExpiresAt: jwt.NewNumericDate(fixedNow.Add(15 * time.Minute)),
	}
}
