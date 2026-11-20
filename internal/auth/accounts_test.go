package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres/postgrestest"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

const insertRefresh = `INSERT INTO admin_refresh_tokens (user_id, jti, token_hash, expires_at)
	VALUES ($1, gen_random_uuid(), decode(repeat($2, 32), 'hex'), now() + $3::interval)`

func newAccounts(t *testing.T) (*Accounts, *pgxpool.Pool, *secrets.PasswordHasher) {
	t.Helper()
	pool := postgrestest.New(t)
	hasher, err := secrets.NewPasswordHasher(secrets.Argon2Params{MemoryKiB: 8192, Time: 1, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	return NewAccounts(pool, hasher), pool, hasher
}

func TestAccountsCreateUser(t *testing.T) {
	t.Parallel()
	a, pool, _ := newAccounts(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name, email, password string
		want                  error
	}{
		{"invalid email", "not-an-email", "una-contraseña-larga", nil},
		{"named address", "Vos <vos@ejemplo.com>", "una-contraseña-larga", nil},
		{"short password", "vos@ejemplo.com", "12345678901", ErrWeakPassword},
		{"long password", "vos@ejemplo.com", strings.Repeat("a", 129), ErrWeakPassword},
	} {
		_, err := a.CreateUser(ctx, tc.email, tc.password)
		if err == nil {
			t.Fatalf("%s: want error", tc.name)
		}
		if tc.want != nil && !errors.Is(err, tc.want) {
			t.Fatalf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}

	id, err := a.CreateUser(ctx, "  Vos@Ejemplo.com ", "una-contraseña-larga")
	if err != nil || id == "" {
		t.Fatalf("create: id=%q err=%v", id, err)
	}
	var email string
	if err := pool.QueryRow(ctx, `SELECT email FROM admin_users`).Scan(&email); err != nil || email != "vos@ejemplo.com" {
		t.Fatalf("email = %q err=%v", email, err)
	}
	if _, err := a.CreateUser(ctx, "otro@ejemplo.com", "una-contraseña-larga"); !errors.Is(err, ErrAdminExists) {
		t.Fatalf("second create err = %v", err)
	}
}

func TestAccountsResetPassword(t *testing.T) {
	t.Parallel()
	a, pool, hasher := newAccounts(t)
	ctx := context.Background()
	if err := a.ResetPassword(ctx, "una-contraseña-larga"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no user err = %v", err)
	}
	id, err := a.CreateUser(ctx, "vos@ejemplo.com", "contraseña-vieja-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, insertRefresh, id, "01", "1 day"); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := pool.QueryRow(ctx, `SELECT password_hash FROM admin_users`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := a.ResetPassword(ctx, "11"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("weak err = %v", err)
	}
	if err := a.ResetPassword(ctx, "contraseña-nueva-2"); err != nil {
		t.Fatal(err)
	}
	var after string
	var unrevoked int
	if err := pool.QueryRow(ctx, `SELECT password_hash FROM admin_users`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Fatal("hash did not change")
	}
	if hasher.Verify(ctx, "contraseña-vieja-1", after) == nil {
		t.Fatal("old password still verifies")
	}
	if err := hasher.Verify(ctx, "contraseña-nueva-2", after); err != nil {
		t.Fatalf("new password does not verify: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM admin_refresh_tokens WHERE revoked_at IS NULL`).Scan(&unrevoked); err != nil || unrevoked != 0 {
		t.Fatalf("unrevoked = %d err=%v", unrevoked, err)
	}
}

func TestAccountsResetTOTP(t *testing.T) {
	t.Parallel()
	a, pool, _ := newAccounts(t)
	ctx := context.Background()
	id, err := a.CreateUser(ctx, "vos@ejemplo.com", "una-contraseña-larga")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE admin_users SET totp_secret_enc = '\x01', totp_enabled_at = now(), totp_last_step = 5`); err != nil {
		t.Fatal(err)
	}
	for _, b := range []string{"01", "02"} {
		if _, err := pool.Exec(ctx, `INSERT INTO admin_recovery_codes (user_id, code_hash) VALUES ($1, decode(repeat($2, 32), 'hex'))`, id, b); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, insertRefresh, id, "01", "1 day"); err != nil {
		t.Fatal(err)
	}
	if err := a.ResetTOTP(ctx); err != nil {
		t.Fatal(err)
	}
	var cleared, codes, unrevoked int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM admin_users WHERE totp_secret_enc IS NULL AND totp_enabled_at IS NULL
		AND totp_last_step IS NULL AND sessions_revoked_at IS NOT NULL`).Scan(&cleared); err != nil || cleared != 1 {
		t.Fatalf("cleared = %d err=%v", cleared, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM admin_recovery_codes`).Scan(&codes); err != nil || codes != 0 {
		t.Fatalf("codes = %d err=%v", codes, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM admin_refresh_tokens WHERE revoked_at IS NULL`).Scan(&unrevoked); err != nil || unrevoked != 0 {
		t.Fatalf("unrevoked = %d err=%v", unrevoked, err)
	}
}

func TestAccountsRevokeAndPrune(t *testing.T) {
	t.Parallel()
	a, pool, _ := newAccounts(t)
	ctx := context.Background()
	if err := a.RevokeSessions(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no user err = %v", err)
	}
	id, err := a.CreateUser(ctx, "vos@ejemplo.com", "una-contraseña-larga")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, insertRefresh, id, "01", "-1 day"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, insertRefresh, id, "02", "1 day"); err != nil {
		t.Fatal(err)
	}
	if err := a.RevokeSessions(ctx); err != nil {
		t.Fatal(err)
	}
	n, err := a.PruneExpiredTokens(ctx)
	if err != nil || n != 1 {
		t.Fatalf("pruned = %d err=%v", n, err)
	}
}
