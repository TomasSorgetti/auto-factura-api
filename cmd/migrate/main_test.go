package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestMigrateRefusesDown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		env     string
		wantErr string
	}{
		{name: "down without flag", args: []string{"down"}, env: "development", wantErr: "down is disabled"},
		{name: "down in production with flag", args: []string{"--allow-down", "down"}, env: "production", wantErr: "down is disabled"},
		{name: "down in production with trailing flag", args: []string{"down", "--allow-down"}, env: "production", wantErr: "down is disabled"},
		{name: "extra argument", args: []string{"up", "now"}, env: "development", wantErr: "usage"},
		{name: "unknown command", args: []string{"sideways"}, env: "development", wantErr: "usage"},
		{name: "no command", args: nil, env: "development", wantErr: "usage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{
				"APP_ENV":      tt.env,
				"DATABASE_URL": "postgres://user:pw@invalid.invalid:5432/db",
			}
			err := run(context.Background(), tt.args, func(k string) string { return env[k] }, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}
