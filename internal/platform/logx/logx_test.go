package logx

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestRequestIDAttr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  context.Context
		with bool
		want bool
	}{
		{name: "with id", ctx: WithRequestID(context.Background(), "abc"), want: true},
		{name: "without id", ctx: context.Background(), want: false},
		{name: "with id and With attrs", ctx: WithRequestID(context.Background(), "abc"), with: true, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			log := New(&buf, slog.LevelInfo, "json")
			if tt.with {
				log = log.With("component", "x")
			}
			log.InfoContext(tt.ctx, "hello")
			out := buf.String()
			if got := strings.Contains(out, `"request_id":"abc"`); got != tt.want {
				t.Fatalf("request_id present = %v, want %v: %s", got, tt.want, out)
			}
			if tt.with && !strings.Contains(out, `"component":"x"`) {
				t.Fatalf("With attr lost: %s", out)
			}
		})
	}
}

func TestLogxWithJob(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := New(&buf, slog.LevelInfo, "json")
	log.InfoContext(WithJob(context.Background(), 42, "auth.prune_tokens"), "hello")
	out := buf.String()
	if !strings.Contains(out, `"job_id":42`) || !strings.Contains(out, `"job_kind":"auth.prune_tokens"`) {
		t.Fatalf("job attrs missing: %s", out)
	}
	buf.Reset()
	log.InfoContext(context.Background(), "hello")
	if strings.Contains(buf.String(), "job_id") {
		t.Fatalf("unexpected job attrs: %s", buf.String())
	}
}
