package secrets

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestBlindIndex(t *testing.T) {
	t.Parallel()
	k1 := bytes.Repeat([]byte{1}, 32)
	k2 := bytes.Repeat([]byte{2}, 32)
	b1, err := NewBlindIndex(k1)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := NewBlindIndex(k2)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("length and determinism", func(t *testing.T) {
		t.Parallel()
		a := b1.Sum("a@b.com")
		if len(a) != 32 {
			t.Fatalf("len = %d", len(a))
		}
		if !bytes.Equal(a, b1.Sum("a@b.com")) {
			t.Fatal("not deterministic")
		}
	})
	t.Run("different values", func(t *testing.T) {
		t.Parallel()
		if bytes.Equal(b1.Sum("a"), b1.Sum("b")) {
			t.Fatal("equal sums")
		}
	})
	t.Run("different keys", func(t *testing.T) {
		t.Parallel()
		if bytes.Equal(b1.Sum("a"), b2.Sum("a")) {
			t.Fatal("equal sums")
		}
	})
	t.Run("bad key length", func(t *testing.T) {
		t.Parallel()
		if _, err := NewBlindIndex(make([]byte, 16)); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("key never leaks", func(t *testing.T) {
		t.Parallel()
		key := make([]byte, 32)
		for i := range key {
			key[i] = byte(i + 100)
		}
		b, err := NewBlindIndex(key)
		if err != nil {
			t.Fatal(err)
		}
		raw := fmt.Sprint(key)
		rawHex := hex.EncodeToString(key)
		type holder struct{ B BlindIndex }
		type ptrHolder struct{ B *BlindIndex }
		values := map[string]any{"pointer": b, "value": *b, "struct": holder{*b}, "ptrStruct": ptrHolder{b}}
		for name, v := range values {
			for _, verb := range []string{"%v", "%d", "%+v", "%#v", "%s", "%x"} {
				out := fmt.Sprintf(verb, v)
				if strings.Contains(out, raw) || strings.Contains(out, rawHex) || strings.Contains(out, string(key)) {
					t.Errorf("%s %s leaked key: %s", name, verb, out)
				}
			}
			var buf bytes.Buffer
			slog.New(slog.NewTextHandler(&buf, nil)).Info("x", "v", v)
			slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "v", v)
			out := buf.String()
			if strings.Contains(out, raw) || strings.Contains(out, rawHex) {
				t.Errorf("%s slog leaked key: %s", name, out)
			}
		}
		if got := fmt.Sprintf("%+v", *b); got != "[REDACTED]" {
			t.Errorf("got %q", got)
		}
	})
}
