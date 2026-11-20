package auth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres/postgrestest"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/redisx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

const loginPassword = "correct horse battery staple"

type loginEnv struct {
	*sessionEnv
	login  *Login
	rdb    *redis.Client
	cipher *secrets.Cipher
	email  string
}

func newLoginEnv(t *testing.T) *loginEnv {
	t.Helper()
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL is not set")
	}
	pool := postgrestest.New(t)
	rdb, err := redisx.New(context.Background(), logx.Secret(url))
	if err != nil {
		t.Fatal(err)
	}
	prefix := "logintest:" + strings.ReplaceAll(t.Name(), "/", "_") + ":" + time.Now().Format("150405.000000000")
	t.Cleanup(func() {
		ctx := context.Background()
		iter := rdb.Scan(ctx, 0, prefix+"*", 100).Iterator()
		for iter.Next(ctx) {
			rdb.Del(ctx, iter.Val())
		}
		_ = rdb.Close()
	})
	hasher, err := secrets.NewPasswordHasher(secrets.Argon2Params{MemoryKiB: 8192, Time: 1, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.NewCipher(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hasher.Hash(context.Background(), loginPassword)
	if err != nil {
		t.Fatal(err)
	}
	clock := &testClock{t: time.Now().UTC().Truncate(time.Second)}
	tk := newTestTokens(t, clock.Now, testAccessKey, testRefreshKey)
	email := "admin@example.com"
	var userID string
	err = pool.QueryRow(context.Background(),
		`INSERT INTO admin_users (email, password_hash) VALUES ($1, $2) RETURNING id::text`, email, hash).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := NewStore(pool)
	sessions := NewSessions(store, tk, log)
	limiter := redisx.NewLimiter(rdb, redisx.LimiterConfig{
		Prefix: prefix + ":rl", MaxFailures: 5, Window: 15 * time.Minute, MaxBlock: 24 * time.Hour,
	})
	login := NewLogin(LoginDeps{
		Store: store, Sessions: sessions, Cipher: cipher, Hasher: hasher, Limiter: limiter,
		Redis: rdb, ChallengePrefix: prefix + ":ch", Log: log,
	})
	return &loginEnv{
		sessionEnv: &sessionEnv{pool: pool, sessions: sessions, clock: clock, userID: userID},
		login:      login, rdb: rdb, cipher: cipher, email: email,
	}
}

func (e *loginEnv) enableTOTP(t *testing.T) string {
	t.Helper()
	key, err := newTOTPKey(e.email)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := e.cipher.Encrypt([]byte(key.Secret()), totpAAD(e.userID))
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.pool.Exec(context.Background(),
		`UPDATE admin_users SET totp_secret_enc = $2, totp_enabled_at = now() WHERE id = $1`, e.userID, sealed)
	if err != nil {
		t.Fatal(err)
	}
	return key.Secret()
}

func (e *loginEnv) start(t *testing.T) LoginStart {
	t.Helper()
	s, err := e.login.Start(context.Background(), e.email, loginPassword, "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func codeFor(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	c, err := totp.GenerateCodeCustom(secret, at, totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var meta = ClientMeta{IP: "1.1.1.1", UserAgent: "test"}

func TestLoginStart(t *testing.T) {
	ctx := context.Background()

	t.Run("email is normalized and next is setup", func(t *testing.T) {
		e := newLoginEnv(t)
		s, err := e.login.Start(ctx, "  Admin@Example.COM ", loginPassword, "1.1.1.1")
		if err != nil {
			t.Fatal(err)
		}
		if s.Next != StepTOTPSetup || s.ChallengeToken.Reveal() == "" || !s.ExpiresAt.After(time.Now()) {
			t.Fatalf("unexpected start: %+v", s)
		}
	})

	t.Run("next is totp when enabled", func(t *testing.T) {
		e := newLoginEnv(t)
		e.enableTOTP(t)
		if s := e.start(t); s.Next != StepTOTP {
			t.Fatalf("next = %q", s.Next)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		e := newLoginEnv(t)
		if _, err := e.login.Start(ctx, e.email, "nope", "1.1.1.1"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("unknown email", func(t *testing.T) {
		e := newLoginEnv(t)
		if _, err := e.login.Start(ctx, "nobody@example.com", loginPassword, "1.1.1.1"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("wrong passwords limit the ip but never the account", func(t *testing.T) {
		e := newLoginEnv(t)
		for i := 0; i < 5; i++ {
			if _, err := e.login.Start(ctx, e.email, "nope", "9.9.9.9"); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("attempt %d err = %v", i, err)
			}
		}
		if _, err := e.login.Start(ctx, e.email, loginPassword, "9.9.9.9"); !errors.Is(err, ErrTooManyAttempts) {
			t.Fatalf("same ip err = %v", err)
		}
		if _, err := e.login.Start(ctx, e.email, loginPassword, "8.8.8.8"); err != nil {
			t.Fatalf("other ip err = %v", err)
		}
	})
}

func TestLoginTOTPSetup(t *testing.T) {
	ctx := context.Background()

	t.Run("begin then complete", func(t *testing.T) {
		e := newLoginEnv(t)
		s := e.start(t)
		setup, err := e.login.BeginTOTPSetup(ctx, s.ChallengeToken.Reveal())
		if err != nil {
			t.Fatal(err)
		}
		if setup.Secret.Reveal() == "" || !strings.HasPrefix(setup.QRCodePNG, "data:image/png;base64,") || !strings.HasPrefix(setup.OtpauthURL.Reveal(), "otpauth://totp/") {
			t.Fatalf("unexpected setup")
		}
		res, err := e.login.CompleteTOTPSetup(ctx, s.ChallengeToken.Reveal(), codeFor(t, setup.Secret.Reveal(), time.Now()), meta)
		if err != nil {
			t.Fatal(err)
		}
		if res.Method != MethodSetup || len(res.RecoveryCodes) != recoveryCount || res.Session.AccessToken.Reveal() == "" || res.UserID != e.userID {
			t.Fatalf("unexpected result")
		}
		var enc []byte
		var codes int
		if err := e.pool.QueryRow(ctx, `SELECT totp_secret_enc FROM admin_users`).Scan(&enc); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(enc, []byte(setup.Secret.Reveal())) {
			t.Fatal("secret stored in clear")
		}
		if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM admin_recovery_codes`).Scan(&codes); err != nil || codes != recoveryCount {
			t.Fatalf("codes = %d, %v", codes, err)
		}
		if _, err := e.login.challenges.get(ctx, s.ChallengeToken.Reveal()); !errors.Is(err, ErrChallengeInvalid) {
			t.Fatalf("challenge still alive: %v", err)
		}
	})

	t.Run("complete without begin", func(t *testing.T) {
		e := newLoginEnv(t)
		s := e.start(t)
		if _, err := e.login.CompleteTOTPSetup(ctx, s.ChallengeToken.Reveal(), "123456", meta); !errors.Is(err, ErrChallengeInvalid) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("begin with totp already enabled", func(t *testing.T) {
		e := newLoginEnv(t)
		e.enableTOTP(t)
		s := e.start(t)
		if _, err := e.login.BeginTOTPSetup(ctx, s.ChallengeToken.Reveal()); !errors.Is(err, ErrTOTPAlreadyEnabled) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("wrong code", func(t *testing.T) {
		e := newLoginEnv(t)
		s := e.start(t)
		setup, err := e.login.BeginTOTPSetup(ctx, s.ChallengeToken.Reveal())
		if err != nil {
			t.Fatal(err)
		}
		bad := codeFor(t, setup.Secret.Reveal(), time.Now().Add(-10*time.Minute))
		if _, err := e.login.CompleteTOTPSetup(ctx, s.ChallengeToken.Reveal(), bad, meta); !errors.Is(err, ErrInvalidTOTP) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestLoginVerifyTOTP(t *testing.T) {
	ctx := context.Background()

	t.Run("correct code then replay", func(t *testing.T) {
		e := newLoginEnv(t)
		secret := e.enableTOTP(t)
		s := e.start(t)
		code := codeFor(t, secret, time.Now())
		res, err := e.login.VerifyTOTP(ctx, s.ChallengeToken.Reveal(), code, meta)
		if err != nil {
			t.Fatal(err)
		}
		if res.Method != MethodTOTP || res.Session.RefreshToken.Reveal() == "" {
			t.Fatal("unexpected result")
		}
		if _, err := e.login.challenges.get(ctx, s.ChallengeToken.Reveal()); !errors.Is(err, ErrChallengeInvalid) {
			t.Fatalf("challenge still alive: %v", err)
		}
		s2 := e.start(t)
		if _, err := e.login.VerifyTOTP(ctx, s2.ChallengeToken.Reveal(), code, meta); !errors.Is(err, ErrInvalidTOTP) {
			t.Fatalf("replay err = %v", err)
		}
	})

	t.Run("five bad codes delete the challenge", func(t *testing.T) {
		e := newLoginEnv(t)
		secret := e.enableTOTP(t)
		s := e.start(t)
		bad := codeFor(t, secret, time.Now().Add(-10*time.Minute))
		for i := 0; i < challengeTries; i++ {
			if _, err := e.login.VerifyTOTP(ctx, s.ChallengeToken.Reveal(), bad, meta); !errors.Is(err, ErrInvalidTOTP) {
				t.Fatalf("attempt %d err = %v", i, err)
			}
		}
		if _, err := e.login.VerifyTOTP(ctx, s.ChallengeToken.Reveal(), bad, meta); !errors.Is(err, ErrChallengeInvalid) {
			t.Fatalf("err = %v", err)
		}
		if _, err := e.login.challenges.get(ctx, s.ChallengeToken.Reveal()); !errors.Is(err, ErrChallengeInvalid) {
			t.Fatalf("challenge still alive: %v", err)
		}
	})

	t.Run("totp not enabled", func(t *testing.T) {
		e := newLoginEnv(t)
		s := e.start(t)
		if _, err := e.login.VerifyTOTP(ctx, s.ChallengeToken.Reveal(), "123456", meta); !errors.Is(err, ErrTOTPNotEnabled) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestLoginSecondFactorLimit(t *testing.T) {
	ctx := context.Background()

	t.Run("concurrent tries across challenges stay within the limit", func(t *testing.T) {
		e := newLoginEnv(t)
		secret := e.enableTOTP(t)
		bad := codeFor(t, secret, time.Now().Add(-10*time.Minute))
		const workers = 12
		tokens := make([]string, workers)
		for i := range tokens {
			s, err := e.login.Start(ctx, e.email, loginPassword, "10.0.0."+strings.Repeat("1", i+1))
			if err != nil {
				t.Fatal(err)
			}
			tokens[i] = s.ChallengeToken.Reveal()
		}
		var verified, limited atomic.Int64
		var wg sync.WaitGroup
		for i := range tokens {
			wg.Go(func() {
				m := ClientMeta{IP: "20.0.0." + strings.Repeat("1", i+1)}
				_, err := e.login.VerifyTOTP(ctx, tokens[i], bad, m)
				switch {
				case errors.Is(err, ErrInvalidTOTP):
					verified.Add(1)
				case errors.Is(err, ErrTooManyAttempts):
					limited.Add(1)
				default:
					t.Errorf("unexpected err = %v", err)
				}
			})
		}
		wg.Wait()
		if verified.Load() != 5 || limited.Load() != workers-5 {
			t.Fatalf("verified = %d, limited = %d", verified.Load(), limited.Load())
		}
	})

	t.Run("unknown challenges do not touch the account limit", func(t *testing.T) {
		e := newLoginEnv(t)
		secret := e.enableTOTP(t)
		for i := 0; i < 10; i++ {
			if _, err := e.login.VerifyTOTP(ctx, "unknown-token", "123456", meta); !errors.Is(err, ErrChallengeInvalid) {
				t.Fatalf("err = %v", err)
			}
		}
		s := e.start(t)
		if _, err := e.login.VerifyTOTP(ctx, s.ChallengeToken.Reveal(), codeFor(t, secret, time.Now()), meta); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestLoginRecoveryCode(t *testing.T) {
	ctx := context.Background()
	e := newLoginEnv(t)
	e.enableTOTP(t)
	codes, err := newRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	hashes := make([][]byte, 0, len(codes))
	for _, c := range codes {
		hashes = append(hashes, hashRecoveryCode(c))
	}
	if err := e.login.store.replaceRecoveryCodes(ctx, e.userID, hashes); err != nil {
		t.Fatal(err)
	}
	s := e.start(t)
	typed := strings.ToLower(strings.ReplaceAll(codes[0], "-", ""))
	res, err := e.login.VerifyRecoveryCode(ctx, s.ChallengeToken.Reveal(), typed, meta)
	if err != nil {
		t.Fatal(err)
	}
	if res.Method != MethodRecovery || res.RecoveryCodesLeft != recoveryCount-1 {
		t.Fatalf("unexpected result: method=%s left=%d", res.Method, res.RecoveryCodesLeft)
	}
	s2 := e.start(t)
	if _, err := e.login.VerifyRecoveryCode(ctx, s2.ChallengeToken.Reveal(), codes[0], meta); !errors.Is(err, ErrInvalidRecoveryCode) {
		t.Fatalf("reuse err = %v", err)
	}
}

func TestLoginChallengeIsolation(t *testing.T) {
	ctx := context.Background()
	e := newLoginEnv(t)
	e.enableTOTP(t)
	s := e.start(t)
	if err := e.rdb.PExpire(ctx, e.login.challenges.key(s.ChallengeToken.Reveal()), time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := e.login.VerifyTOTP(ctx, s.ChallengeToken.Reveal(), "123456", meta); !errors.Is(err, ErrChallengeInvalid) {
		t.Fatalf("err = %v", err)
	}
	if _, err := e.login.VerifyTOTP(ctx, "unknown-token", "123456", meta); !errors.Is(err, ErrChallengeInvalid) {
		t.Fatalf("unknown err = %v", err)
	}
}

func TestLoginRecoveryCodeWithoutTOTP(t *testing.T) {
	e := newLoginEnv(t)
	s := e.start(t)
	if _, err := e.login.VerifyRecoveryCode(context.Background(), s.ChallengeToken.Reveal(), "AAAAA-BBBBB", meta); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Fatalf("err = %v", err)
	}
}
