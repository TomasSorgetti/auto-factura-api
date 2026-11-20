package logx

import (
	"bytes"
	"encoding/json"
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

func TestSecretRedactedInGoSyntaxAndJSON(t *testing.T) {
	t.Parallel()

	if got := fmt.Sprintf("%#v", Secret("hunter2")); strings.Contains(got, "hunter2") {
		t.Fatalf("%%#v leaked: %s", got)
	}
	b, err := json.Marshal(struct{ S Secret }{Secret("hunter2")})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"S":"[REDACTED]"}` {
		t.Fatalf("json = %s", b)
	}
}
