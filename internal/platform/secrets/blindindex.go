package secrets

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
)

// BlindIndex computes HMAC-SHA256 over values for exact-match lookups of encrypted data.
// It never reveals the key and is safe for concurrent use.
const redacted = "[REDACTED]"

type BlindIndex struct{ key []byte }

func NewBlindIndex(key []byte) (*BlindIndex, error) {
	if len(key) != keySize {
		return nil, errors.New("blind index key must be 32 bytes")
	}
	return &BlindIndex{key: append([]byte(nil), key...)}, nil
}

// Sum returns the 32-byte index of value. The caller normalizes value beforehand.
func (b *BlindIndex) Sum(value string) []byte {
	m := hmac.New(sha256.New, b.key)
	m.Write([]byte(value))
	return m.Sum(nil)
}

func (BlindIndex) LogValue() slog.Value { return slog.StringValue(redacted) }

func (BlindIndex) String() string { return redacted }

func (BlindIndex) GoString() string { return `"` + redacted + `"` }

func (BlindIndex) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, redacted) }
