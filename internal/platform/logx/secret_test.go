package logx

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestSecretRedacted(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("msg", "key", Secret("hunter2"))
	if strings.Contains(buf.String(), "hunter2") {
		t.Fatalf("secret leaked: %s", buf.String())
	}
	if got := fmt.Sprint(Secret("hunter2")); got != "[REDACTED]" {
		t.Fatalf("Sprint = %q", got)
	}
	if Secret("hunter2").Reveal() != "hunter2" {
		t.Fatal("Reveal must return the value")
	}
}
