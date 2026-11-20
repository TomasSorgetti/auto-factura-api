package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func mapEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
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
