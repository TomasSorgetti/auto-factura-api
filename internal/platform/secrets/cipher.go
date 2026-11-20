package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

var ErrDecrypt = errors.New("decrypt: authentication failed")

// Cipher encrypts small values with AES-256-GCM. Output is nonce || ciphertext.
type Cipher struct{ aead cipher.AEAD }

func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, errors.New("master key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt uses a fresh random nonce per call. aad binds the value to its
// context and must be passed again to Decrypt.
func (c *Cipher) Encrypt(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize(), c.aead.NonceSize()+len(plaintext)+c.aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("read nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, plaintext, aad), nil
}

// Decrypt returns ErrDecrypt for any failure, without detail.
func (c *Cipher) Decrypt(sealed, aad []byte) ([]byte, error) {
	n := c.aead.NonceSize()
	if len(sealed) < n+c.aead.Overhead() {
		return nil, ErrDecrypt
	}
	plain, err := c.aead.Open(nil, sealed[:n], sealed[n:], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}
