package secrets

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// RandomToken returns n random bytes encoded as base64url without padding.
// n must be at least 16.
func RandomToken(n int) (string, error) {
	if n < 16 {
		return "", fmt.Errorf("random token: need at least 16 bytes, got %d", n)
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken is SHA-256 of the token, for tokens that already carry 256 bits of entropy.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
