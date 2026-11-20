package secrets

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/sync/semaphore"
)

var (
	ErrPasswordMismatch = errors.New("password mismatch")
	ErrInvalidHash      = errors.New("invalid password hash")
)

const (
	saltLen        = 16
	keyLen         = 32
	minMemoryKiB   = 8192
	maxMemoryKiB   = 262144
	maxTime        = 10
	maxThreads     = 8
	maxConcurrency = 2
)

type Argon2Params struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
}

// PasswordHasher hashes with argon2id. At most two hashes run at once.
type PasswordHasher struct {
	params Argon2Params
	sem    *semaphore.Weighted
}

// NewPasswordHasher rejects parameters outside the bounds Verify accepts
// (memory 8192 to 262144 KiB, time 1 to 10, threads 1 to 8), since argon2
// panics on zero time or threads.
func NewPasswordHasher(p Argon2Params) (*PasswordHasher, error) {
	if p.MemoryKiB < minMemoryKiB || p.MemoryKiB > maxMemoryKiB ||
		p.Time < 1 || p.Time > maxTime || p.Threads < 1 || p.Threads > maxThreads {
		return nil, errors.New("argon2 params out of range")
	}
	return &PasswordHasher{params: p, sem: semaphore.NewWeighted(maxConcurrency)}, nil
}

func (h *PasswordHasher) Hash(ctx context.Context, password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}
	if err := h.sem.Acquire(ctx, 1); err != nil {
		return "", err
	}
	defer h.sem.Release(1)
	key := argon2.IDKey([]byte(password), salt, h.params.Time, h.params.MemoryKiB, h.params.Threads, keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.params.MemoryKiB, h.params.Time, h.params.Threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify uses the parameters stored in encoded, not the current ones. It
// returns nil, ErrPasswordMismatch, ErrInvalidHash or the context error.
func (h *PasswordHasher) Verify(ctx context.Context, password, encoded string) error {
	p, salt, want, err := parseHash(encoded)
	if err != nil {
		return err
	}
	if err := h.sem.Acquire(ctx, 1); err != nil {
		return err
	}
	defer h.sem.Release(1)
	got := argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrPasswordMismatch
	}
	return nil
}

// DummyVerify runs one hash with the current params and discards the result, so an unknown email costs the same.
func (h *PasswordHasher) DummyVerify(ctx context.Context, password string) {
	if err := h.sem.Acquire(ctx, 1); err != nil {
		return
	}
	defer h.sem.Release(1)
	salt := make([]byte, saltLen)
	argon2.IDKey([]byte(password), salt, h.params.Time, h.params.MemoryKiB, h.params.Threads, keyLen)
}

func parseHash(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	version, ok := strings.CutPrefix(parts[2], "v=")
	if !ok || version != strconv.Itoa(argon2.Version) {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	var p Argon2Params
	fields := strings.Split(parts[3], ",")
	if len(fields) != 3 {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	m, okM := parseUint(fields[0], "m=", maxMemoryKiB)
	t, okT := parseUint(fields[1], "t=", maxTime)
	th, okP := parseUint(fields[2], "p=", maxThreads)
	if !okM || !okT || !okP || m == 0 || t == 0 || th == 0 {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	p = Argon2Params{MemoryKiB: uint32(m), Time: uint32(t), Threads: uint8(th)}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != saltLen {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) != keyLen {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	return p, salt, key, nil
}

func parseUint(field, prefix string, max uint64) (uint64, bool) {
	v, ok := strings.CutPrefix(field, prefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil || n > max {
		return 0, false
	}
	return n, true
}
