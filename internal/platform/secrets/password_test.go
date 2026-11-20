package secrets

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

var testParams = Argon2Params{MemoryKiB: 8192, Time: 1, Threads: 1}

func newTestHasher(t *testing.T, p Argon2Params) *PasswordHasher {
	t.Helper()
	h, err := NewPasswordHasher(p)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	return h
}

func TestNewPasswordHasherParams(t *testing.T) {
	t.Parallel()

	tests := map[string]Argon2Params{
		"zero":         {},
		"time zero":    {MemoryKiB: 8192, Time: 0, Threads: 1},
		"threads zero": {MemoryKiB: 8192, Time: 1, Threads: 0},
		"memory low":   {MemoryKiB: 1024, Time: 1, Threads: 1},
		"memory high":  {MemoryKiB: 262145, Time: 1, Threads: 1},
		"time high":    {MemoryKiB: 8192, Time: 11, Threads: 1},
		"threads high": {MemoryKiB: 8192, Time: 1, Threads: 9},
	}
	for name, p := range tests {
		if _, err := NewPasswordHasher(p); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestPasswordHasher(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := newTestHasher(t, testParams)
	encoded, err := h.Hash(ctx, "correct horse")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=8192,t=1,p=1$") {
		t.Fatalf("unexpected format %q", encoded)
	}
	if err := h.Verify(ctx, "correct horse", encoded); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := h.Verify(ctx, "wrong", encoded); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("wrong password: got %v", err)
	}

	other := newTestHasher(t, Argon2Params{MemoryKiB: 16384, Time: 2, Threads: 2})
	if err := other.Verify(ctx, "correct horse", encoded); err != nil {
		t.Fatalf("hash with other params: %v", err)
	}

	h.DummyVerify(ctx, "x")

	parts := strings.Split(encoded, "$")
	invalid := map[string]string{
		"argon2i":    strings.Replace(encoded, "argon2id", "argon2i", 1),
		"version":    strings.Replace(encoded, "v=19", "v=18", 1),
		"memory":     strings.Replace(encoded, "m=8192", "m=999999999", 1),
		"time":       strings.Replace(encoded, "t=1", "t=11", 1),
		"threads":    strings.Replace(encoded, "p=1", "p=9", 1),
		"bad base64": "$argon2id$v=19$m=8192,t=1,p=1$!!!$" + parts[5],
		"short key":  "$argon2id$v=19$m=8192,t=1,p=1$" + parts[4] + "$AAAA",
		"empty":      "",
	}
	for name, enc := range invalid {
		if err := h.Verify(ctx, "correct horse", enc); !errors.Is(err, ErrInvalidHash) {
			t.Errorf("%s: got %v", name, err)
		}
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := h.Verify(canceled, "correct horse", encoded); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context: got %v", err)
	}
}

func TestPasswordHasherConcurrency(t *testing.T) {
	t.Parallel()

	h := newTestHasher(t, testParams)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			_, err := h.Hash(context.Background(), "pw")
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
	}
}
