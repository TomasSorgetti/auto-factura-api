package auth

import (
	"testing"
	"time"
)

func TestClassifyRefresh(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	ptr := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	id := "replacement"
	live := &refreshRow{ID: id, ExpiresAt: now.Add(time.Hour)}
	next := "next"
	rotated := &refreshRow{ID: id, ExpiresAt: now.Add(time.Hour), RevokedAt: ptr(-time.Minute), ReplacedByID: &next}

	tests := []struct {
		name  string
		token *refreshRow
		repl  *refreshRow
		want  RefreshVerdict
	}{
		{"nil", nil, nil, RefreshUnknown},
		{"expired", &refreshRow{ExpiresAt: now.Add(-time.Second)}, nil, RefreshUnknown},
		{"active", &refreshRow{ExpiresAt: now.Add(time.Hour)}, nil, RefreshActive},
		{"revoked without replacement", &refreshRow{ExpiresAt: now.Add(time.Hour), RevokedAt: ptr(-time.Minute)}, nil, RefreshRevoked},
		{"rotated 5m ago", &refreshRow{ExpiresAt: now.Add(time.Hour), RevokedAt: ptr(-5 * time.Minute), ReplacedByID: &id}, live, RefreshSuperseded},
		{"rotated 11m ago live replacement", &refreshRow{ExpiresAt: now.Add(time.Hour), RevokedAt: ptr(-11 * time.Minute), ReplacedByID: &id}, live, RefreshSuperseded},
		{"rotated 11m ago rotated replacement", &refreshRow{ExpiresAt: now.Add(time.Hour), RevokedAt: ptr(-11 * time.Minute), ReplacedByID: &id}, rotated, RefreshReplayed},
		{"rotated 5m ago replacement closed by logout", &refreshRow{ExpiresAt: now.Add(time.Hour), RevokedAt: ptr(-5 * time.Minute), ReplacedByID: &id}, rotatedNoReplacement(now), RefreshRevoked},
		{"rotated 11m ago missing replacement", &refreshRow{ExpiresAt: now.Add(time.Hour), RevokedAt: ptr(-11 * time.Minute), ReplacedByID: &id}, nil, RefreshSuperseded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyRefresh(tt.token, tt.repl, now); got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func rotatedNoReplacement(now time.Time) *refreshRow {
	at := now.Add(-time.Minute)
	return &refreshRow{ID: "replacement", ExpiresAt: now.Add(time.Hour), RevokedAt: &at}
}
