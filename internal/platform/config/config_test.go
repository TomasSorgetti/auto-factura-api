package config

import (
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"
)

func mapEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
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
			cfg, err := Load(mapEnv(map[string]string{"APP_ENV": tt.env, "CORS_ALLOWED_ORIGINS": tt.value}))
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
