package clients

import (
	"bytes"
	"errors"
	"testing"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

func newTestCodec(t *testing.T) *Codec {
	t.Helper()
	c, err := NewCodec(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCodec(t *testing.T) {
	t.Parallel()
	c := newTestCodec(t)

	t.Run("round trip", func(t *testing.T) {
		t.Parallel()
		sealed, err := c.seal(fieldNotes, "id-1", "hola")
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.open(fieldNotes, "id-1", sealed)
		if err != nil || got != "hola" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("fresh nonce", func(t *testing.T) {
		t.Parallel()
		a, _ := c.seal(fieldNotes, "id-1", "hola")
		b, _ := c.seal(fieldNotes, "id-1", "hola")
		if bytes.Equal(a, b) {
			t.Fatal("same ciphertext")
		}
	})
	t.Run("bound to id and field", func(t *testing.T) {
		t.Parallel()
		sealed, _ := c.seal(fieldContactEmail, "id-1", "a@b.com")
		if _, err := c.open(fieldContactEmail, "id-2", sealed); !errors.Is(err, secrets.ErrDecrypt) {
			t.Fatalf("other id: %v", err)
		}
		if _, err := c.open(fieldContactName, "id-1", sealed); !errors.Is(err, secrets.ErrDecrypt) {
			t.Fatalf("other field: %v", err)
		}
	})
	t.Run("blind index", func(t *testing.T) {
		t.Parallel()
		if !bytes.Equal(c.emailIndex("a@b.com"), c.emailIndex("a@b.com")) {
			t.Fatal("not deterministic")
		}
		if bytes.Equal(c.emailIndex("a@b.com"), c.emailIndex("c@b.com")) {
			t.Fatal("equal indexes")
		}
	})
	t.Run("bad master", func(t *testing.T) {
		t.Parallel()
		if _, err := NewCodec(make([]byte, 16)); err == nil {
			t.Fatal("expected error")
		}
	})
}
