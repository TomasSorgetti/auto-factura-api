package audit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres/postgrestest"
)

const testUUID = "0b6f3b0e-3f0e-4a57-9a53-6a0d3f5c1c11"

func TestEventValidate(t *testing.T) {
	t.Parallel()
	manyKeys := map[string]string{}
	for i := 0; i < 21; i++ {
		manyKeys[strings.Repeat("k", i+1)] = "v"
	}
	tests := []struct {
		name    string
		event   Event
		wantErr bool
	}{
		{"admin without id", Event{Actor: Actor{Type: ActorAdmin}, Action: ActionLogout}, true},
		{"system with id", Event{Actor: Actor{Type: ActorSystem, ID: testUUID}, Action: ActionLogout}, true},
		{"id not uuid", Event{Actor: Actor{Type: ActorAdmin, ID: "nope"}, Action: ActionLogout}, true},
		{"unknown action", Event{Actor: Actor{Type: ActorSystem}, Action: "auth.unknown"}, true},
		{"unknown actor", Event{Actor: Actor{Type: "bot"}, Action: ActionLogout}, true},
		{"21 keys", Event{Actor: Actor{Type: ActorSystem}, Action: ActionLogout, Data: manyKeys}, true},
		{"value 501 bytes", Event{Actor: Actor{Type: ActorSystem}, Action: ActionLogout, Data: map[string]string{"a": strings.Repeat("x", 501)}}, true},
		{"invalid ip", Event{Actor: Actor{Type: ActorSystem, IP: "999.1.1.1"}, Action: ActionLogout}, true},
		{"valid admin", Event{Actor: Actor{Type: ActorAdmin, ID: testUUID, IP: "203.0.113.7"}, Action: ActionLogout, Data: map[string]string{"a": "b"}}, false},
		{"valid system", Event{Actor: Actor{Type: ActorSystem}, Action: ActionSettingsUpdated}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := tt.event.validate()
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidEvent) {
					t.Fatalf("got %v, want ErrInvalidEvent", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRecord(t *testing.T) {
	t.Parallel()
	pool := postgrestest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	count := func() int {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM audit_events").Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	ev := Event{
		Actor:      Actor{Type: ActorAdmin, ID: testUUID, IP: "203.0.113.7"},
		Action:     ActionLoginSucceeded,
		EntityType: "admin_user",
		EntityID:   testUUID,
		Data:       map[string]string{"device": "firefox"},
	}
	if err := Record(logx.WithRequestID(ctx, "req-123"), pool, ev); err != nil {
		t.Fatalf("record: %v", err)
	}
	var reqID, action, data string
	err := pool.QueryRow(ctx, "SELECT request_id, action, data::text FROM audit_events").Scan(&reqID, &action, &data)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if reqID != "req-123" || action != string(ActionLoginSucceeded) || !strings.Contains(data, "firefox") {
		t.Fatalf("unexpected row: %q %q %q", reqID, action, data)
	}

	before := count()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := Record(ctx, tx, Event{Actor: Actor{Type: ActorSystem}, Action: ActionSettingsUpdated}); err != nil {
		t.Fatalf("record in tx: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := count(); got != before {
		t.Fatalf("rows = %d, want %d", got, before)
	}
}

func TestAppendOnly(t *testing.T) {
	t.Parallel()
	pool := postgrestest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := Record(ctx, pool, Event{Actor: Actor{Type: ActorSystem}, Action: ActionSettingsUpdated}); err != nil {
		t.Fatalf("record: %v", err)
	}
	for _, stmt := range []string{
		"UPDATE audit_events SET action = 'auth.logout'",
		"DELETE FROM audit_events",
	} {
		_, err := pool.Exec(ctx, stmt)
		if err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("%s: got %v, want append-only error", stmt, err)
		}
	}
}
