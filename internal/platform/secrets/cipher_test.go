package secrets

import (
	"bytes"
	"errors"
	"testing"
)

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	c, err := NewCipher(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	return c
}

func TestCipherRoundTrip(t *testing.T) {
	t.Parallel()

	c := newTestCipher(t)
	aad := []byte("admin_users.totp_secret:1")
	plain := []byte("hello")

	a, err := c.Encrypt(plain, aad)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	b, err := c.Encrypt(plain, aad)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions of the same value are identical")
	}
	got, err := c.Decrypt(a, aad)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("decrypt: %q, %v", got, err)
	}

	if _, err := c.Decrypt(a, []byte("other")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("different aad: got %v", err)
	}
	tampered := bytes.Clone(a)
	tampered[len(tampered)-1] ^= 1
	if _, err := c.Decrypt(tampered, aad); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("tampered: got %v", err)
	}
	if _, err := c.Decrypt([]byte{1, 2}, aad); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("short input: got %v", err)
	}
	if _, err := NewCipher(make([]byte, 31)); err == nil {
		t.Fatal("expected error for a 31 byte key")
	}
}
