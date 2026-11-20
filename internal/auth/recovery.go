package auth

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

func newRecoveryCodes() ([]string, error) {
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	seen := make(map[string]struct{}, recoveryCount)
	out := make([]string, 0, recoveryCount)
	for len(out) < recoveryCount {
		b := make([]byte, 7)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("read random bytes: %w", err)
		}
		s := enc.EncodeToString(b)[:10]
		code := s[:5] + "-" + s[5:]
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out, nil
}

func normalizeRecoveryCode(s string) string {
	return strings.NewReplacer(" ", "", "-", "").Replace(strings.ToUpper(s))
}

func hashRecoveryCode(code string) []byte {
	return secrets.HashToken(normalizeRecoveryCode(code))
}
