package auth

import "time"

// RefreshVerdict is the outcome of classifying a presented refresh token.
type RefreshVerdict int

const (
	RefreshActive RefreshVerdict = iota + 1
	RefreshSuperseded
	RefreshRevoked
	RefreshReplayed
	RefreshUnknown
)

type refreshRow struct {
	ID           string
	UserID       string
	TokenHash    []byte
	CreatedAt    time.Time
	ExpiresAt    time.Time
	RevokedAt    *time.Time
	ReplacedByID *string
}

func classifyRefresh(token *refreshRow, replacement *refreshRow, now time.Time) RefreshVerdict {
	if token == nil || !token.ExpiresAt.After(now) {
		return RefreshUnknown
	}
	if token.RevokedAt == nil {
		return RefreshActive
	}
	if token.ReplacedByID == nil {
		return RefreshRevoked
	}
	if replacement != nil && replacement.RevokedAt != nil && replacement.ReplacedByID == nil {
		return RefreshRevoked
	}
	if now.Sub(*token.RevokedAt) <= refreshGrace {
		return RefreshSuperseded
	}
	if replacement != nil && replacement.RevokedAt != nil {
		return RefreshReplayed
	}
	return RefreshSuperseded
}
