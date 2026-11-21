package secrets

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestDeriveKey(t *testing.T) {
	t.Parallel()
	master := bytes.Repeat([]byte{1}, 32)

	t.Run("length and determinism", func(t *testing.T) {
		t.Parallel()
		a, err := DeriveKey(master, PurposeClientData)
		if err != nil {
			t.Fatal(err)
		}
		if len(a) != 32 {
			t.Fatalf("len = %d", len(a))
		}
		b := mustDerive(t, master, PurposeClientData)
		if !bytes.Equal(a, b) {
			t.Fatal("not deterministic")
		}
		if bytes.Equal(a, master) {
			t.Fatal("equals master")
		}
	})
	t.Run("purposes are independent", func(t *testing.T) {
		t.Parallel()
		a := mustDerive(t, master, PurposeClientData)
		b := mustDerive(t, master, PurposeClientBlindIndex)
		if bytes.Equal(a, b) {
			t.Fatal("equal keys")
		}
	})
	t.Run("invalid input", func(t *testing.T) {
		t.Parallel()
		for _, n := range []int{31, 33} {
			if _, err := DeriveKey(make([]byte, n), PurposeClientData); err == nil {
				t.Fatalf("len %d: expected error", n)
			}
		}
		if _, err := DeriveKey(master, ""); err == nil {
			t.Fatal("empty purpose: expected error")
		}
	})
	t.Run("fixed vector", func(t *testing.T) {
		t.Parallel()
		got := mustDerive(t, master, PurposeClientData)
		const want = "06329b047e0618f3bc9ffc39ce7745ad58135fddf294ee2a6bb014c51db93a85"
		if h := hex.EncodeToString(got); h != want {
			t.Fatalf("vector = %s", h)
		}
	})
}

func mustDerive(t *testing.T, master []byte, p Purpose) []byte {
	t.Helper()
	k, err := DeriveKey(master, p)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
