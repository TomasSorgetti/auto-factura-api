package config

import (
	"bytes"
	"encoding/base64"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

const testDatabaseURL = "postgres://user:s3cret@localhost:5432/db?sslmode=disable"

var testMasterKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))

var envDefaults = map[string]string{
	"DATABASE_URL":       testDatabaseURL,
	"REDIS_URL":          "redis://localhost:6379/0",
	"MASTER_KEY":         testMasterKey,
	"JWT_ACCESS_SECRET":  strings.Repeat("a", 32),
	"JWT_REFRESH_SECRET": strings.Repeat("b", 32),
}

func mapEnv(m map[string]string) func(string) string {
	return func(k string) string {
		if v, ok := m[k]; ok {
			return v
		}
		return envDefaults[k]
	}
}

func TestLoadAuth(t *testing.T) {
	t.Parallel()

	prod := map[string]string{"APP_ENV": "production", "RESEND_API_KEY": "re_x", "EMAIL_FROM": "Auto <a@example.com>", "PUBLIC_BASE_URL": "https://app.example.com"}
	with := func(base map[string]string, kv ...string) map[string]string {
		m := maps.Clone(base)
		for i := 0; i < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	dev := map[string]string{"APP_ENV": "development"}

	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "defaults", env: dev},
		{name: "production ok", env: prod},
		{name: "master key missing", env: with(dev, "MASTER_KEY", ""), wantErr: "MASTER_KEY"},
		{name: "master key not base64", env: with(dev, "MASTER_KEY", "***"), wantErr: "MASTER_KEY"},
		{name: "master key 16 bytes", env: with(dev, "MASTER_KEY", base64.StdEncoding.EncodeToString(make([]byte, 16))), wantErr: "MASTER_KEY"},
		{name: "access secret short", env: with(dev, "JWT_ACCESS_SECRET", "short"), wantErr: "JWT_ACCESS_SECRET"},
		{name: "refresh secret short", env: with(dev, "JWT_REFRESH_SECRET", "short"), wantErr: "JWT_REFRESH_SECRET"},
		{name: "equal secrets", env: with(dev, "JWT_REFRESH_SECRET", strings.Repeat("a", 32)), wantErr: "must differ"},
		{name: "access ttl too short", env: with(dev, "ACCESS_TOKEN_TTL", "30s"), wantErr: "ACCESS_TOKEN_TTL"},
		{name: "access ttl too long", env: with(dev, "ACCESS_TOKEN_TTL", "2h"), wantErr: "ACCESS_TOKEN_TTL"},
		{name: "refresh ttl too short", env: with(dev, "REFRESH_TOKEN_TTL", "10m"), wantErr: "REFRESH_TOKEN_TTL"},
		{name: "refresh ttl too long", env: with(dev, "REFRESH_TOKEN_TTL", "1000h"), wantErr: "REFRESH_TOKEN_TTL"},
		{name: "argon2 memory low", env: with(dev, "ARGON2_MEMORY_KIB", "100"), wantErr: "ARGON2_MEMORY_KIB"},
		{name: "argon2 time high", env: with(dev, "ARGON2_TIME", "11"), wantErr: "ARGON2_TIME"},
		{name: "argon2 threads zero", env: with(dev, "ARGON2_THREADS", "0"), wantErr: "ARGON2_THREADS"},
		{name: "production without resend", env: with(prod, "RESEND_API_KEY", ""), wantErr: "RESEND_API_KEY"},
		{name: "email from invalid", env: with(prod, "EMAIL_FROM", "nope"), wantErr: "EMAIL_FROM"},
		{name: "email from missing", env: with(prod, "EMAIL_FROM", ""), wantErr: "EMAIL_FROM"},
		{name: "base url with path", env: with(dev, "PUBLIC_BASE_URL", "http://localhost:5173/x"), wantErr: "PUBLIC_BASE_URL"},
		{name: "base url http in production", env: with(prod, "PUBLIC_BASE_URL", "http://app.example.com"), wantErr: "PUBLIC_BASE_URL"},
		{name: "invalid tz", env: with(dev, "BUSINESS_TZ", "Mars/Olympus"), wantErr: "BUSINESS_TZ"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Load(mapEnv(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected %s error, got %v", tt.wantErr, err)
				}
				for _, secret := range []string{strings.Repeat("a", 32), strings.Repeat("b", 32), testMasterKey, "re_x"} {
					if strings.Contains(err.Error(), secret) {
						t.Fatalf("error leaks a secret: %v", err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(cfg.MasterKey.Reveal()) != 32 || cfg.AccessTokenTTL != 15*time.Minute || cfg.RefreshTokenTTL != 168*time.Hour ||
				cfg.Argon2 != (secrets.Argon2Params{MemoryKiB: 19456, Time: 2, Threads: 1}) ||
				cfg.BusinessLocation == nil || cfg.BusinessLocation.String() != "America/Argentina/Buenos_Aires" {
				t.Fatalf("unexpected defaults: %+v", cfg)
			}
		})
	}
}

func TestLoadPublicBaseURLTrailingSlash(t *testing.T) {
	t.Parallel()

	cfg, err := Load(mapEnv(map[string]string{"APP_ENV": "development", "PUBLIC_BASE_URL": "https://x.com/"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicBaseURL != "https://x.com" {
		t.Fatalf("PublicBaseURL = %q", cfg.PublicBaseURL)
	}
}

func TestLoadRedis(t *testing.T) {
	t.Parallel()

	for _, url := range []string{"", "http://s3cret@host/0"} {
		_, err := Load(mapEnv(map[string]string{"APP_ENV": "test", "REDIS_URL": url}))
		if err == nil || !strings.Contains(err.Error(), "REDIS_URL") {
			t.Fatalf("url %q: expected REDIS_URL error, got %v", url, err)
		}
		if strings.Contains(err.Error(), "s3cret") {
			t.Fatalf("error leaks the url: %v", err)
		}
	}
}

func TestLoadDatabase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
		conns   int32
		timeout time.Duration
	}{
		{name: "defaults", env: map[string]string{"APP_ENV": "test"}, conns: 5, timeout: 5 * time.Second},
		{name: "custom", env: map[string]string{"APP_ENV": "test", "DB_MAX_CONNS": "10", "DB_STATEMENT_TIMEOUT": "30s"}, conns: 10, timeout: 30 * time.Second},
		{name: "missing url", env: map[string]string{"APP_ENV": "test", "DATABASE_URL": ""}, wantErr: "DATABASE_URL"},
		{name: "invalid url", env: map[string]string{"APP_ENV": "test", "DATABASE_URL": "postgres://user:s3cret@host:notaport/db"}, wantErr: "DATABASE_URL"},
		{name: "conns zero", env: map[string]string{"APP_ENV": "test", "DB_MAX_CONNS": "0"}, wantErr: "DB_MAX_CONNS"},
		{name: "conns too many", env: map[string]string{"APP_ENV": "test", "DB_MAX_CONNS": "21"}, wantErr: "DB_MAX_CONNS"},
		{name: "conns not a number", env: map[string]string{"APP_ENV": "test", "DB_MAX_CONNS": "x"}, wantErr: "DB_MAX_CONNS"},
		{name: "timeout too short", env: map[string]string{"APP_ENV": "test", "DB_STATEMENT_TIMEOUT": "500ms"}, wantErr: "DB_STATEMENT_TIMEOUT"},
		{name: "timeout too long", env: map[string]string{"APP_ENV": "test", "DB_STATEMENT_TIMEOUT": "2m"}, wantErr: "DB_STATEMENT_TIMEOUT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Load(mapEnv(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected %s error, got %v", tt.wantErr, err)
				}
				if strings.Contains(err.Error(), "s3cret") {
					t.Fatalf("error leaks the password: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.DBMaxConns != tt.conns || cfg.DBStatementTimeout != tt.timeout {
				t.Fatalf("got %d, %s", cfg.DBMaxConns, cfg.DBStatementTimeout)
			}
		})
	}
}

func TestLoadMigrate(t *testing.T) {
	t.Parallel()

	cfg, err := LoadMigrate(mapEnv(map[string]string{"APP_ENV": "production"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != EnvProduction || cfg.DatabaseURL.Reveal() != testDatabaseURL {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	_, err = LoadMigrate(mapEnv(map[string]string{"DATABASE_URL": ""}))
	if err == nil || !strings.Contains(err.Error(), "APP_ENV") || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected both errors, got %v", err)
	}
}

func TestLoadCORSAllowedOrigins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     string
		value   string
		want    []string
		wantErr bool
	}{
		{name: "empty", env: "development", value: ""},
		{name: "two with spaces", env: "development", value: " http://localhost:5173 , https://app.example.com ", want: []string{"http://localhost:5173", "https://app.example.com"}},
		{name: "wildcard", env: "development", value: "*", wantErr: true},
		{name: "with path", env: "development", value: "https://app.example.com/x", wantErr: true},
		{name: "with query", env: "development", value: "https://app.example.com?a=1", wantErr: true},
		{name: "with user", env: "development", value: "https://u@app.example.com", wantErr: true},
		{name: "bad scheme", env: "development", value: "ftp://app.example.com", wantErr: true},
		{name: "no host", env: "development", value: "https://", wantErr: true},
		{name: "http in production", env: "production", value: "http://app.example.com", wantErr: true},
		{name: "https in production", env: "production", value: "https://app.example.com", want: []string{"https://app.example.com"}},
		{name: "uppercase host", env: "development", value: "https://App.Example.COM", want: []string{"https://app.example.com"}},
		{name: "default https port", env: "development", value: "https://app.example.com:443", want: []string{"https://app.example.com"}},
		{name: "default http port", env: "development", value: "http://localhost:80", want: []string{"http://localhost"}},
		{name: "non default port kept", env: "development", value: "http://localhost:443", want: []string{"http://localhost:443"}},
		{name: "trailing slash", env: "development", value: "https://app.example.com/", want: []string{"https://app.example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Load(mapEnv(map[string]string{
				"APP_ENV": tt.env, "CORS_ALLOWED_ORIGINS": tt.value,
				"RESEND_API_KEY": "re_x", "EMAIL_FROM": "a@example.com", "PUBLIC_BASE_URL": "https://app.example.com",
			}))
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "CORS_ALLOWED_ORIGINS") {
					t.Fatalf("expected CORS_ALLOWED_ORIGINS error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(cfg.CORSAllowedOrigins, tt.want) {
				t.Fatalf("got %v, want %v", cfg.CORSAllowedOrigins, tt.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		wantErr []string
	}{
		{name: "defaults", env: map[string]string{"APP_ENV": "development"}},
		{name: "missing app env", env: map[string]string{}, wantErr: []string{"APP_ENV"}},
		{name: "bad log level", env: map[string]string{"APP_ENV": "test", "LOG_LEVEL": "verbose"}, wantErr: []string{"LOG_LEVEL"}},
		{name: "request timeout zero", env: map[string]string{"APP_ENV": "test", "REQUEST_TIMEOUT": "0s"}, wantErr: []string{"REQUEST_TIMEOUT"}},
		{name: "request timeout too long", env: map[string]string{"APP_ENV": "test", "REQUEST_TIMEOUT": "2m"}, wantErr: []string{"REQUEST_TIMEOUT"}},
		{name: "production text format", env: map[string]string{"APP_ENV": "production", "LOG_FORMAT": "text"}, wantErr: []string{"LOG_FORMAT"}},
		{
			name:    "several invalid",
			env:     map[string]string{"LOG_LEVEL": "x", "LOG_FORMAT": "xml", "SHUTDOWN_TIMEOUT": "1h"},
			wantErr: []string{"APP_ENV", "LOG_LEVEL", "LOG_FORMAT", "SHUTDOWN_TIMEOUT"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Load(mapEnv(tt.env))
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.Env != EnvDevelopment || cfg.HTTPAddr != ":8080" || cfg.LogLevel != slog.LevelInfo ||
					cfg.LogFormat != "json" || cfg.RequestTimeout != 15*time.Second || cfg.ShutdownTimeout != 10*time.Second {
					t.Fatalf("unexpected defaults: %+v", cfg)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			for _, key := range tt.wantErr {
				if !strings.Contains(err.Error(), key) {
					t.Errorf("error %q does not mention %s", err, key)
				}
			}
		})
	}
}

func TestLoadWorker(t *testing.T) {
	t.Parallel()

	cfg, err := LoadWorker(mapEnv(map[string]string{"APP_ENV": "development"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DBMaxConns != 10 || cfg.ShutdownTimeout != 25*time.Second || cfg.DBStatementTimeout != 5*time.Second ||
		cfg.LogFormat != "json" || cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}

	for _, tc := range []struct{ key, value string }{
		{"WORKER_DB_MAX_CONNS", "7"},
		{"WORKER_DB_MAX_CONNS", "31"},
		{"WORKER_SHUTDOWN_TIMEOUT", "61s"},
	} {
		_, err := LoadWorker(mapEnv(map[string]string{"APP_ENV": "development", tc.key: tc.value}))
		if err == nil || !strings.Contains(err.Error(), tc.key) {
			t.Fatalf("%s=%s: err = %v", tc.key, tc.value, err)
		}
	}
}
