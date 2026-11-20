package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/TomasSorgetti/auto-factura-api/internal/emails/emailstest"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

func TestShouldAlertNewDevice(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	tests := []struct {
		name        string
		isNew, prev bool
		last        *time.Time
		want        bool
	}{
		{"known device", false, true, nil, false},
		{"new without previous", true, false, nil, false},
		{"new with previous and no alert", true, true, nil, true},
		{"inside cooldown", true, true, at(5 * time.Minute), false},
		{"exactly at cooldown", true, true, at(10 * time.Minute), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldAlertNewDevice(tt.isNew, tt.prev, tt.last, now); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDescribeUserAgent(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, ua, want string }{
		{"chrome windows", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", "Chrome en Windows"},
		{"edge", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0", "Edge en Windows"},
		{"safari iphone", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1", "Safari en iOS"},
		{"firefox linux", "Mozilla/5.0 (X11; Linux x86_64; rv:127.0) Gecko/20100101 Firefox/127.0", "Firefox en Linux"},
		{"empty", "", "Navegador desconocido en sistema desconocido"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := describeUserAgent(tt.ua); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsDeviceID(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]bool{
		"3f2b8c1e-5d4a-4b7e-9c10-0a1b2c3d4e5f": true,
		"3F2B8C1E-5D4A-4B7E-9C10-0A1B2C3D4E5F": true,
		"":                                     false,
		"not-a-uuid":                           false,
		"3f2b8c1e5d4a4b7e9c100a1b2c3d4e5f":     false,
		"3f2b8c1e-5d4a-4b7e-9c10-0a1b2c3d4e5g": false,
	} {
		if got := isDeviceID(s); got != want {
			t.Errorf("isDeviceID(%q) = %v, want %v", s, got, want)
		}
	}
}

type deviceEnv struct {
	*sessionEnv
	tracker *DeviceTracker
	mail    *emailstest.Fake
	now     time.Time
}

func newDeviceEnv(t *testing.T) *deviceEnv {
	t.Helper()
	se := newSessionEnv(t)
	mail := &emailstest.Fake{}
	e := &deviceEnv{sessionEnv: se, mail: mail, now: time.Now().UTC().Truncate(time.Second)}
	tracker, err := NewDeviceTracker(DeviceTrackerDeps{
		Store: NewStore(se.pool), Sender: mail, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return e.now }, Loc: time.UTC, BaseURL: "https://app.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	e.tracker = tracker
	return e
}

func (e *deviceEnv) track(device string) string {
	return e.tracker.Track(context.Background(), e.userID, "admin@example.com",
		ClientMeta{IP: "190.0.0.1", UserAgent: "Mozilla/5.0 (Windows NT 10.0) Chrome/126.0 Safari/537.36", DeviceID: device}, MethodTOTP)
}

const (
	deviceA = "11111111-1111-4111-8111-111111111111"
	deviceB = "22222222-2222-4222-8222-222222222222"
	deviceC = "33333333-3333-4333-8333-333333333333"
)

func TestDeviceTrackerTrack(t *testing.T) {
	e := newDeviceEnv(t)

	if got := e.track(deviceA); got != deviceA {
		t.Fatalf("device id = %q", got)
	}
	if n := len(e.mail.Sent()); n != 0 {
		t.Fatalf("first device sent %d mails", n)
	}

	e.now = e.now.Add(time.Hour)
	e.track(deviceB)
	sent := e.mail.Sent()
	if len(sent) != 1 {
		t.Fatalf("second device sent %d mails", len(sent))
	}
	if sent[0].Subject != newDeviceSubject || !strings.Contains(sent[0].Text, "190.0.0.1") ||
		!strings.Contains(sent[0].HTML, "190.0.0.1") || sent[0].To != "admin@example.com" {
		t.Fatalf("unexpected message: %+v", sent[0])
	}

	e.now = e.now.Add(5 * time.Minute)
	e.track(deviceC)
	if n := len(e.mail.Sent()); n != 1 {
		t.Fatalf("device inside cooldown sent mail, total %d", n)
	}

	e.now = e.now.Add(time.Hour)
	e.track(deviceB)
	if n := len(e.mail.Sent()); n != 1 {
		t.Fatalf("known device sent mail, total %d", n)
	}

	fresh := e.track("garbage")
	if !isDeviceID(fresh) || fresh == "garbage" {
		t.Fatalf("invalid id was not replaced: %q", fresh)
	}
}

func TestDeviceTrackerSenderFails(t *testing.T) {
	e := newDeviceEnv(t)
	e.track(deviceA)
	e.mail.SetError(errors.New("boom"))
	e.now = e.now.Add(time.Hour)
	if got := e.track(deviceB); got != deviceB {
		t.Fatalf("device id = %q", got)
	}
	var id string
	err := e.pool.QueryRow(context.Background(),
		`SELECT id::text FROM admin_login_devices WHERE user_id = $1 AND device_hash = $2`, e.userID, secrets.HashToken(deviceB)).Scan(&id)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("failed device was kept: %v", err)
	}
	e.mail.SetError(nil)
	e.track(deviceB)
	if n := len(e.mail.Sent()); n != 1 {
		t.Fatalf("retry sent %d mails", n)
	}
}

func TestDeviceTrackerCanceledContext(t *testing.T) {
	e := newDeviceEnv(t)
	e.track(deviceA)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.now = e.now.Add(time.Hour)
	got := e.tracker.Track(ctx, e.userID, "admin@example.com", ClientMeta{IP: "190.0.0.1", DeviceID: deviceB}, MethodTOTP)
	if got != deviceB {
		t.Fatalf("device id = %q", got)
	}
	if n := len(e.mail.Sent()); n != 1 {
		t.Fatalf("canceled context sent %d mails", n)
	}
}
