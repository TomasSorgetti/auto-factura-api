package secrets

import (
	"bytes"
	"testing"
)

func TestRandomToken(t *testing.T) {
	t.Parallel()

	a, err := RandomToken(32)
	if err != nil {
		t.Fatalf("random token: %v", err)
	}
	b, err := RandomToken(32)
	if err != nil {
		t.Fatalf("random token: %v", err)
	}
	if len(a) != 43 || a == b {
		t.Fatalf("unexpected tokens %q %q", a, b)
	}
	if len(HashToken(a)) != 32 || !bytes.Equal(HashToken(a), HashToken(a)) || bytes.Equal(HashToken(a), HashToken(b)) {
		t.Fatal("unexpected token hashes")
	}
}
