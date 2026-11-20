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

func TestRandomTokenLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n       int
		wantLen int
		wantErr bool
	}{
		{n: -1, wantErr: true},
		{n: 0, wantErr: true},
		{n: 15, wantErr: true},
		{n: 16, wantLen: 22},
		{n: 32, wantLen: 43},
	}
	for _, tt := range tests {
		got, err := RandomToken(tt.n)
		if (err != nil) != tt.wantErr || len(got) != tt.wantLen {
			t.Errorf("RandomToken(%d) = %q, %v", tt.n, got, err)
		}
	}
}
