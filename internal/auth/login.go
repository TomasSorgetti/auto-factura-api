package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/TomasSorgetti/auto-factura-api/internal/audit"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/redisx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

const (
	auditStagePassword = "password"
	auditStageTOTP     = "totp"
	auditStageRecovery = "recovery_code"

	rateLimitAuditTTL = 15 * time.Minute
)

// accountKey counts only second-factor attempts, so wrong passwords from a
// stranger cannot lock the account; passwords are limited per IP.
const accountKey = "login:account"

// passwordGlobalKey bounds password attempts from any source, because the
// client IP behind a proxy can be spoofed and the per-IP limit then fails.
const passwordGlobalKey = "login:password-global"

type LoginStep string

const (
	StepTOTP      LoginStep = "totp"
	StepTOTPSetup LoginStep = "totp_setup"
)

type LoginMethod string

const (
	MethodTOTP     LoginMethod = "totp"
	MethodRecovery LoginMethod = "recovery_code"
	MethodSetup    LoginMethod = "totp_setup"
)

type LoginStart struct {
	ChallengeToken logx.Secret
	ExpiresAt      time.Time
	Next           LoginStep
}

// LoginResult is what a completed login returns. RecoveryCodes is set only when
// TOTP enrollment completes, and is the only time they are available.
type LoginResult struct {
	Session           IssuedSession
	UserID            string
	DeviceID          string
	Method            LoginMethod
	RecoveryCodes     []logx.Secret
	RecoveryCodesLeft int
}

// LoginDeps are the collaborators of Login. ChallengePrefix namespaces the Redis challenge keys.
type LoginDeps struct {
	Store           *Store
	Sessions        *Sessions
	Cipher          *secrets.Cipher
	Hasher          *secrets.PasswordHasher
	Limiter         *redisx.Limiter
	PasswordLimiter *redisx.Limiter
	Redis           *redis.Client
	Devices         *DeviceTracker
	Audit           Auditor
	ChallengePrefix string
	Log             *slog.Logger
	Now             func() time.Time
}

// Login runs the two-step admin login: password, then TOTP, recovery code or TOTP enrollment.
type Login struct {
	store      *Store
	sessions   *Sessions
	cipher     *secrets.Cipher
	hasher     *secrets.PasswordHasher
	limiter    *redisx.Limiter
	pwLimiter  *redisx.Limiter
	challenges *challengeStore
	devices    *DeviceTracker
	audit      Auditor
	log        *slog.Logger
	now        func() time.Time
}

func NewLogin(d LoginDeps) (*Login, error) {
	if d.Devices == nil {
		return nil, errors.New("login: device tracker is required")
	}
	if d.PasswordLimiter == nil {
		return nil, errors.New("login: password limiter is required")
	}
	now := d.Now
	if now == nil {
		now = time.Now
	}
	prefix := d.ChallengePrefix
	if prefix == "" {
		prefix = "auth:challenge"
	}
	return &Login{
		store:      d.Store,
		sessions:   d.Sessions,
		cipher:     d.Cipher,
		hasher:     d.Hasher,
		limiter:    d.Limiter,
		pwLimiter:  d.PasswordLimiter,
		challenges: &challengeStore{rdb: d.Redis, prefix: prefix, now: now},
		devices:    d.Devices,
		audit:      d.Audit,
		log:        d.Log,
		now:        now,
	}, nil
}

func ipKey(ip string) string { return "login:ip:" + ipOrUnknown(ip) }

func factorIPKey(ip string) string { return "login:factor-ip:" + ipOrUnknown(ip) }

func ipOrUnknown(ip string) string {
	if ip == "" {
		return "unknown"
	}
	return ip
}

func (l *Login) take(ctx context.Context, keys ...string) error {
	d, err := l.limiter.Take(ctx, keys...)
	if err != nil {
		return err
	}
	if d > 0 {
		return ErrTooManyAttempts
	}
	return nil
}

// finish runs after a verified second factor. Clearing the challenge and the
// limiter is best-effort: neither may lose a session or the recovery codes.
func (l *Login) finish(ctx context.Context, token string, user *adminUser, ip string, meta ClientMeta, result LoginResult) (LoginResult, error) {
	if err := l.challenges.delete(ctx, token); err != nil {
		l.log.WarnContext(ctx, "delete login challenge failed")
	}
	result.UserID = user.ID
	session, err := l.sessions.Issue(ctx, user.ID, meta)
	if err != nil {
		if len(result.RecoveryCodes) > 0 {
			return result, fmt.Errorf("%w: %w", ErrSetupSessionNotIssued, err)
		}
		return LoginResult{}, fmt.Errorf("issue session: %w", err)
	}
	if err := l.limiter.Reset(ctx, accountKey, ipKey(ip), factorIPKey(ip)); err != nil {
		l.log.WarnContext(ctx, "reset login limiter failed")
	}
	result.Session = session
	result.DeviceID = l.devices.Track(ctx, user.ID, user.Email, meta, result.Method)
	recordEvent(ctx, l.audit, audit.Event{
		Actor: adminActor(user.ID, meta.IP), Action: audit.ActionLoginSucceeded,
		Data: map[string]string{"method": auditMethod(result.Method)},
	})
	return result, nil
}

func auditMethod(m LoginMethod) string {
	if m == MethodSetup {
		return "totp_enrollment"
	}
	return string(m)
}

// firstRateLimited reports whether this is the first rejection of ip within rateLimitAuditTTL, so a
// blocked client writes one audit event per block and not one per request.
func (l *Login) firstRateLimited(ctx context.Context, ip string) bool {
	key := l.challenges.prefix + ":audit-rl:" + ipOrUnknown(ip)
	ok, err := l.challenges.rdb.SetNX(ctx, key, 1, rateLimitAuditTTL).Result()
	if err != nil {
		l.log.WarnContext(ctx, "dedupe rate limited audit event", "err", err)
		return false
	}
	return ok
}

func (l *Login) auditFailure(ctx context.Context, ip, stage string, err error) {
	var reason string
	switch {
	case errors.Is(err, ErrTooManyAttempts):
		if !l.firstRateLimited(ctx, ip) {
			l.log.WarnContext(ctx, "login rate limited")
			return
		}
		reason = "rate_limited"
	case errors.Is(err, ErrInvalidCredentials), errors.Is(err, ErrInvalidTOTP), errors.Is(err, ErrInvalidRecoveryCode):
		reason = "invalid"
	default:
		return
	}
	recordEvent(ctx, l.audit, audit.Event{
		Actor: anonymousActor(ip), Action: audit.ActionLoginFailed,
		Data: map[string]string{"stage": stage, "reason": reason},
	})
}

func (l *Login) loadChallenge(ctx context.Context, token string) (challenge, *adminUser, error) {
	ch, err := l.challenges.get(ctx, token)
	if err != nil {
		return challenge{}, nil, err
	}
	user, err := l.store.userByID(ctx, ch.userID)
	if errors.Is(err, ErrNotFound) {
		return challenge{}, nil, ErrChallengeInvalid
	}
	if err != nil {
		return challenge{}, nil, err
	}
	if user.SessionsRevokedAt != nil && !user.SessionsRevokedAt.Before(ch.issuedAt) {
		return challenge{}, nil, ErrChallengeInvalid
	}
	return ch, user, nil
}

// secondFactor counts the try on the challenge and on the limiter before the
// code is checked, so concurrent tries cannot exceed either limit. The
// challenge is checked first: only someone holding a valid one reaches the
// account key.
func (l *Login) secondFactor(ctx context.Context, token, ip string) (challenge, *adminUser, error) {
	ch, user, err := l.loadChallenge(ctx, token)
	if err != nil {
		return challenge{}, nil, err
	}
	if err := l.challenges.attempt(ctx, token); err != nil {
		return challenge{}, nil, err
	}
	if err := l.take(ctx, accountKey, factorIPKey(ip)); err != nil {
		return challenge{}, nil, err
	}
	return ch, user, nil
}

func challengeAAD(uid string) []byte { return []byte("challenge:" + uid) }

func totpAAD(uid string) []byte { return []byte("admin_users.totp_secret:" + uid) }

func (l *Login) Start(ctx context.Context, email, password, ip string) (LoginStart, error) {
	start, err := l.start(ctx, email, password, ip)
	if err != nil {
		l.auditFailure(ctx, ip, auditStagePassword, err)
	}
	return start, err
}

func (l *Login) start(ctx context.Context, email, password, ip string) (LoginStart, error) {
	if err := l.take(ctx, ipKey(ip)); err != nil {
		return LoginStart{}, err
	}
	d, err := l.pwLimiter.Take(ctx, passwordGlobalKey)
	if err != nil {
		return LoginStart{}, err
	}
	if d > 0 {
		return LoginStart{}, ErrTooManyAttempts
	}
	email = strings.ToLower(strings.TrimSpace(email))
	user, err := l.store.userByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		l.hasher.DummyVerify(ctx, password)
		return LoginStart{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginStart{}, err
	}
	err = l.hasher.Verify(ctx, password, user.PasswordHash)
	if errors.Is(err, secrets.ErrPasswordMismatch) {
		return LoginStart{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginStart{}, fmt.Errorf("verify password: %w", err)
	}
	token, expiresAt, err := l.challenges.create(ctx, user.ID)
	if err != nil {
		return LoginStart{}, err
	}
	next := StepTOTPSetup
	if user.TOTPEnabled {
		next = StepTOTP
	}
	return LoginStart{ChallengeToken: logx.Secret(token), ExpiresAt: expiresAt, Next: next}, nil
}

// BeginTOTPSetup guesses nothing, so it is bounded by the challenge alone.
func (l *Login) BeginTOTPSetup(ctx context.Context, token string) (TOTPSetup, error) {
	_, user, err := l.loadChallenge(ctx, token)
	if err != nil {
		return TOTPSetup{}, err
	}
	if user.TOTPEnabled {
		return TOTPSetup{}, ErrTOTPAlreadyEnabled
	}
	key, err := newTOTPKey(user.Email)
	if err != nil {
		return TOTPSetup{}, err
	}
	qr, err := qrDataURL(key)
	if err != nil {
		return TOTPSetup{}, err
	}
	sealed, err := l.cipher.Encrypt([]byte(key.Secret()), challengeAAD(user.ID))
	if err != nil {
		return TOTPSetup{}, fmt.Errorf("encrypt pending secret: %w", err)
	}
	if err := l.challenges.setPending(ctx, token, sealed); err != nil {
		return TOTPSetup{}, err
	}
	return TOTPSetup{Secret: logx.Secret(key.Secret()), OtpauthURL: logx.Secret(key.URL()), QRCodePNG: qr}, nil
}

// CompleteTOTPSetup enables TOTP and issues the session. When only the session
// fails, it returns ErrSetupSessionNotIssued together with a result that still
// holds the recovery codes, which the caller must show.
func (l *Login) CompleteTOTPSetup(ctx context.Context, token, code string, meta ClientMeta) (res LoginResult, err error) {
	defer func() { l.auditFailure(ctx, meta.IP, auditStageTOTP, err) }()
	ch, user, err := l.secondFactor(ctx, token, meta.IP)
	if err != nil {
		return LoginResult{}, err
	}
	if user.TOTPEnabled {
		return LoginResult{}, ErrTOTPAlreadyEnabled
	}
	if len(ch.pending) == 0 {
		return LoginResult{}, ErrChallengeInvalid
	}
	secret, err := l.cipher.Decrypt(ch.pending, challengeAAD(user.ID))
	if err != nil {
		l.log.ErrorContext(ctx, "decrypt pending totp secret")
		return LoginResult{}, fmt.Errorf("decrypt pending totp secret: %w", err)
	}
	step, ok := matchTOTP(string(secret), code, l.now())
	if !ok {
		return LoginResult{}, ErrInvalidTOTP
	}
	sealed, err := l.cipher.Encrypt(secret, totpAAD(user.ID))
	if err != nil {
		return LoginResult{}, fmt.Errorf("encrypt totp secret: %w", err)
	}
	codes, err := newRecoveryCodes()
	if err != nil {
		return LoginResult{}, err
	}
	hashes := make([][]byte, 0, len(codes))
	shown := make([]logx.Secret, 0, len(codes))
	for _, c := range codes {
		hashes = append(hashes, hashRecoveryCode(c))
		shown = append(shown, logx.Secret(c))
	}
	err = l.store.InTx(ctx, func(st *Store) error {
		enabled, err := st.enableTOTP(ctx, user.ID, sealed, step)
		if err != nil {
			return err
		}
		if !enabled {
			return ErrTOTPAlreadyEnabled
		}
		return st.replaceRecoveryCodes(ctx, user.ID, hashes)
	})
	if err != nil {
		return LoginResult{}, err
	}
	recordEvent(ctx, l.audit, audit.Event{Actor: adminActor(user.ID, meta.IP), Action: audit.ActionTOTPEnrolled})
	return l.finish(ctx, token, user, meta.IP, meta, LoginResult{Method: MethodSetup, RecoveryCodes: shown})
}

func (l *Login) VerifyTOTP(ctx context.Context, token, code string, meta ClientMeta) (res LoginResult, err error) {
	defer func() { l.auditFailure(ctx, meta.IP, auditStageTOTP, err) }()
	_, user, err := l.secondFactor(ctx, token, meta.IP)
	if err != nil {
		return LoginResult{}, err
	}
	if !user.TOTPEnabled {
		return LoginResult{}, ErrTOTPNotEnabled
	}
	secret, err := l.cipher.Decrypt(user.TOTPSecretEnc, totpAAD(user.ID))
	if err != nil {
		l.log.ErrorContext(ctx, "decrypt totp secret")
		return LoginResult{}, fmt.Errorf("decrypt totp secret: %w", err)
	}
	step, ok := matchTOTP(string(secret), code, l.now())
	if ok {
		fresh, err := l.store.advanceTOTPStep(ctx, user.ID, step)
		if err != nil {
			return LoginResult{}, err
		}
		ok = fresh
	}
	if !ok {
		return LoginResult{}, ErrInvalidTOTP
	}
	return l.finish(ctx, token, user, meta.IP, meta, LoginResult{Method: MethodTOTP})
}

func (l *Login) VerifyRecoveryCode(ctx context.Context, token, code string, meta ClientMeta) (res LoginResult, err error) {
	defer func() { l.auditFailure(ctx, meta.IP, auditStageRecovery, err) }()
	_, user, err := l.secondFactor(ctx, token, meta.IP)
	if err != nil {
		return LoginResult{}, err
	}
	if !user.TOTPEnabled {
		return LoginResult{}, ErrTOTPNotEnabled
	}
	used, err := l.store.useRecoveryCode(ctx, user.ID, hashRecoveryCode(code))
	if err != nil {
		return LoginResult{}, err
	}
	if !used {
		return LoginResult{}, ErrInvalidRecoveryCode
	}
	left, err := l.store.unusedRecoveryCodes(ctx, user.ID)
	if err != nil {
		return LoginResult{}, err
	}
	recordEvent(ctx, l.audit, audit.Event{
		Actor: adminActor(user.ID, meta.IP), Action: audit.ActionRecoveryCodeUsed,
		Data: map[string]string{"remaining": strconv.Itoa(left)},
	})
	return l.finish(ctx, token, user, meta.IP, meta, LoginResult{Method: MethodRecovery, RecoveryCodesLeft: left})
}
