package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres/postgrestest"
)

func envFor(url string) func(string) string {
	m := map[string]string{
		"APP_ENV":           "test",
		"DATABASE_URL":      url,
		"ARGON2_MEMORY_KIB": "8192",
		"ARGON2_TIME":       "1",
	}
	return func(k string) string { return m[k] }
}

func TestRunUsage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no command", nil},
		{"unknown command", []string{"explode"}},
		{"create-user without email", []string{"create-user"}},
		{"extra argument", []string{"revoke-sessions", "now"}},
		{"unknown flag", []string{"reset-totp", "--password-stdin"}},
	} {
		err := run(context.Background(), tc.args, envFor("postgres://u:p@localhost:1/x"), strings.NewReader(""), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("%s: err = %v, want usage error", tc.name, err)
		}
	}
}

func TestRunCreateUserStdin(t *testing.T) {
	t.Parallel()
	_, url := postgrestest.NewURL(t)
	ctx := context.Background()

	var out bytes.Buffer
	args := []string{"create-user", "--email", "Vos@Ejemplo.com", "--password-stdin"}
	if err := run(ctx, args, envFor(url), strings.NewReader("una-contraseña-larga\r\n"), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "una-contraseña-larga") || !strings.Contains(out.String(), "admin user created: vos@ejemplo.com") {
		t.Fatalf("stdout = %q", out.String())
	}

	err := run(ctx, []string{"reset-password", "--password-stdin"}, envFor(url), strings.NewReader(""), &out)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty stdin err = %v", err)
	}
	if err := run(ctx, []string{"reset-password", "--password-stdin"}, envFor(url), strings.NewReader("otra-contraseña-larga\n"), &out); err != nil {
		t.Fatal(err)
	}
	for cmd, want := range map[string]string{
		"revoke-sessions": "all sessions revoked",
		"reset-totp":      "next login will ask to enroll again",
		"prune-tokens":    "pruned 0 expired refresh tokens",
	} {
		out.Reset()
		if err := run(ctx, []string{cmd}, envFor(url), strings.NewReader(""), &out); err != nil || !strings.Contains(out.String(), want) {
			t.Fatalf("%s: err=%v out=%q", cmd, err, out.String())
		}
	}
}

func TestRunCreateUserGenerated(t *testing.T) {
	t.Parallel()
	_, url := postgrestest.NewURL(t)
	ctx := context.Background()

	var out bytes.Buffer
	if err := run(ctx, []string{"create-user", "--email", "vos@ejemplo.com"}, envFor(url), strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	const prefix = "generated password (shown once): "
	var pwd string
	for _, line := range strings.Split(out.String(), "\n") {
		if p, ok := strings.CutPrefix(line, prefix); ok {
			pwd = p
		}
	}
	if len(pwd) != 24 {
		t.Fatalf("generated password length = %d, stdout %q", len(pwd), out.String())
	}

	err := run(ctx, []string{"create-user", "--email", "otro@ejemplo.com"}, envFor(url), strings.NewReader(""), &out)
	if err == nil || !strings.Contains(err.Error(), "admin user already exists") {
		t.Fatalf("second create err = %v", err)
	}
}
