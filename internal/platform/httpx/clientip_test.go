package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, xff, remote, want string
	}{
		{"multiple values", "203.0.113.5, 10.0.0.1", "10.0.0.2:1234", "203.0.113.5"},
		{"garbage falls back", "not-an-ip", "10.0.0.2:1234", "10.0.0.2"},
		{"empty falls back", "", "10.0.0.2:1234", "10.0.0.2"},
		{"ipv6 xff", "2001:db8::1", "10.0.0.2:1234", "2001:db8::1"},
		{"ipv6 remote", "", "[2001:db8::2]:443", "2001:db8::2"},
		{"remote without port", "", "10.0.0.3", "10.0.0.3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.remote
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := ClientIP(r); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
