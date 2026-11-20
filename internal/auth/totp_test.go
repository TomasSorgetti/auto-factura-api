package auth

import (
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func TestMatchTOTP(t *testing.T) {
	t.Parallel()
	key, err := newTOTPKey("admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	secret := key.Secret()
	now := time.Date(2026, 3, 1, 12, 0, 10, 0, time.UTC)
	cur := now.Unix() / totpPeriod
	opts := totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}
	codeAt := func(step int64) string {
		c, err := totp.GenerateCodeCustom(secret, time.Unix(step*totpPeriod, 0), opts)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	tests := []struct {
		name     string
		code     string
		wantStep int64
		wantOK   bool
	}{
		{"current", codeAt(cur), cur, true},
		{"previous", codeAt(cur - 1), cur - 1, true},
		{"next", codeAt(cur + 1), cur + 1, true},
		{"two steps back", codeAt(cur - 2), 0, false},
		{"too short", "12345", 0, false},
		{"too long", "1234567", 0, false},
		{"non digit", "12a456", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			step, ok := matchTOTP(secret, tt.code, now)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantOK && step != tt.wantStep {
				t.Fatalf("step = %d, want %d", step, tt.wantStep)
			}
		})
	}
}
