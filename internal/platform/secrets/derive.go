package secrets

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
)

const keySize = 32

// Purpose names the use of a derived subkey. Each purpose yields an independent key.
type Purpose string

const (
	PurposeClientData       Purpose = "auto-factura/client-data/v1"
	PurposeClientBlindIndex Purpose = "auto-factura/client-blind-index/v1"
)

// DeriveKey returns a 32-byte subkey of master for purpose using HKDF-SHA256 with no salt.
// Different purposes yield independent keys. master must be 32 bytes.
func DeriveKey(master []byte, p Purpose) ([]byte, error) {
	if len(master) != keySize {
		return nil, errors.New("master key must be 32 bytes")
	}
	if p == "" {
		return nil, errors.New("purpose must not be empty")
	}
	return hkdf.Key(sha256.New, master, nil, string(p), keySize)
}
