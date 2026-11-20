package auth

import (
	"strings"
	"testing"
	"time"
)

func TestAlertEmailEscapes(t *testing.T) {
	t.Parallel()
	msg, err := newDeviceMessage("a@example.com", alertData{
		When: time.Date(2026, 11, 20, 1, 43, 0, 0, time.UTC), Device: `<script>alert(1)</script>`,
		IP: "190.0.0.1", Method: MethodTOTP, LoginURL: "https://app.example.com/login",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg.HTML, "<script>") {
		t.Fatalf("html not escaped: %s", msg.HTML)
	}
	if !strings.Contains(msg.HTML, "&lt;script&gt;") || !strings.Contains(msg.HTML, "20/11/2026 01:43") {
		t.Fatalf("unexpected html: %s", msg.HTML)
	}
}

func TestAlertEmailOmitsEmptyIP(t *testing.T) {
	t.Parallel()
	msg, err := newDeviceMessage("a@example.com", alertData{When: time.Now(), Device: "x", Method: MethodRecovery, LoginURL: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg.Text, "Dirección IP") || strings.Contains(msg.HTML, "Dirección IP") {
		t.Fatal("empty IP line present")
	}
}

func TestAlertEmailInvalidIP(t *testing.T) {
	t.Parallel()
	msg, err := newDeviceMessage("a@example.com", alertData{When: time.Now(), Device: "x", IP: "<b>nope</b>", Method: MethodTOTP, LoginURL: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg.Text, "Dirección IP") || strings.Contains(msg.HTML, "nope") {
		t.Fatal("invalid IP rendered")
	}
}
