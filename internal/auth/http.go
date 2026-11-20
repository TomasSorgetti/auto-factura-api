package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/redisx"
)

const (
	maxUserAgent  = 500
	refreshIPKey  = "refresh:ip:"
	logoutTimeout = 5 * time.Second
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (r loginRequest) Validate() error {
	fe := httpx.FieldErrors{}
	if len(r.Email) < 3 || len(r.Email) > 254 || !strings.Contains(r.Email, "@") {
		fe["email"] = "required"
	}
	if len(r.Password) < 1 || len(r.Password) > 128 {
		fe["password"] = "required"
	}
	if len(fe) > 0 {
		return fe
	}
	return nil
}

type codeRequest struct {
	Code string `json:"code"`
}

func (r codeRequest) Validate() error {
	if len(r.Code) < 1 || len(r.Code) > 32 {
		return httpx.FieldErrors{"code": "required"}
	}
	return nil
}

type loginResponse struct {
	Next      LoginStep `json:"next"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type totpSetupResponse struct {
	Secret     string `json:"secret"`
	OtpauthURL string `json:"otpauthUrl"`
	QRCode     string `json:"qrCode"`
}

type enrollResponse struct {
	RecoveryCodes []string `json:"recoveryCodes"`
	SessionIssued bool     `json:"sessionIssued"`
}

type recoveryResponse struct {
	RecoveryCodesLeft int `json:"recoveryCodesLeft"`
}

type meResponse struct {
	Email             string `json:"email"`
	RecoveryCodesLeft int    `json:"recoveryCodesLeft"`
}

// HandlerDeps are the collaborators of Handler. RefreshLimiter bounds
// /api/auth/refresh per client IP.
type HandlerDeps struct {
	Login          *Login
	Sessions       *Sessions
	Store          *Store
	RefreshLimiter *redisx.Limiter
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
	Log            *slog.Logger
}

// Handler exposes the auth endpoints. It is immutable after construction.
type Handler struct {
	login     *Login
	sessions  *Sessions
	store     *Store
	refreshRL *redisx.Limiter
	cookies   cookieConfig
	log       *slog.Logger
}

func NewHandler(d HandlerDeps) (*Handler, error) {
	if d.Login == nil || d.Sessions == nil || d.Store == nil || d.RefreshLimiter == nil || d.Log == nil {
		return nil, errors.New("auth handler: login, sessions, store, refresh limiter and log are required")
	}
	if d.AccessTTL <= 0 || d.RefreshTTL <= 0 {
		return nil, errors.New("auth handler: ttl must be positive")
	}
	return &Handler{
		login: d.Login, sessions: d.Sessions, store: d.Store, refreshRL: d.RefreshLimiter,
		cookies: cookieConfig{AccessTTL: d.AccessTTL, RefreshTTL: d.RefreshTTL}, log: d.Log,
	}, nil
}

func (h *Handler) Register(rt *httpx.Router) {
	rt.System("POST /api/auth/login", h.startLogin)
	rt.System("POST /api/auth/totp/setup", h.beginSetup)
	rt.System("POST /api/auth/totp/enroll", h.enroll)
	rt.System("POST /api/auth/totp", h.verifyTOTP)
	rt.System("POST /api/auth/recovery", h.verifyRecovery)
	rt.System("POST /api/auth/refresh", h.refresh)
	rt.System("POST /api/auth/logout", h.logout)
	rt.Admin("GET /api/admin/me", h.me)
}

func cookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func clientMeta(r *http.Request) ClientMeta {
	return ClientMeta{
		IP:        httpx.ClientIP(r),
		UserAgent: truncate(r.UserAgent(), maxUserAgent),
		DeviceID:  cookieValue(r, cookieDevice),
	}
}

func challengeToken(r *http.Request) (string, error) {
	tok := cookieValue(r, cookieChallenge)
	if tok == "" {
		return "", ErrChallengeInvalid
	}
	return tok, nil
}

func (h *Handler) startLogin(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[loginRequest](w, r)
	if err != nil {
		return err
	}
	start, err := h.login.Start(r.Context(), req.Email, req.Password, httpx.ClientIP(r))
	if err != nil {
		return err
	}
	setChallenge(w, start.ChallengeToken.Reveal())
	return httpx.JSON(w, http.StatusOK, loginResponse{Next: start.Next, ExpiresAt: start.ExpiresAt})
}

func (h *Handler) beginSetup(w http.ResponseWriter, r *http.Request) error {
	tok, err := challengeToken(r)
	if err != nil {
		return err
	}
	setup, err := h.login.BeginTOTPSetup(r.Context(), tok)
	if err != nil {
		return challengeErr(w, err)
	}
	return httpx.JSON(w, http.StatusOK, totpSetupResponse{
		Secret: setup.Secret.Reveal(), OtpauthURL: setup.OtpauthURL.Reveal(), QRCode: setup.QRCodePNG,
	})
}

func challengeErr(w http.ResponseWriter, err error) error {
	if errors.Is(err, ErrChallengeInvalid) {
		clearChallenge(w)
	}
	return err
}

func (h *Handler) complete(w http.ResponseWriter, res LoginResult) {
	setSession(w, res.Session, h.cookies)
	setDevice(w, res.DeviceID)
	clearChallenge(w)
}

func (h *Handler) enroll(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[codeRequest](w, r)
	if err != nil {
		return err
	}
	tok, err := challengeToken(r)
	if err != nil {
		return err
	}
	res, err := h.login.CompleteTOTPSetup(r.Context(), tok, req.Code, clientMeta(r))
	issued := true
	switch {
	case errors.Is(err, ErrSetupSessionNotIssued):
		h.log.ErrorContext(r.Context(), "totp enrolled but session not issued", "err", err)
		clearChallenge(w)
		issued = false
	case err != nil:
		return challengeErr(w, err)
	default:
		h.complete(w, res)
	}
	codes := make([]string, 0, len(res.RecoveryCodes))
	for _, c := range res.RecoveryCodes {
		codes = append(codes, c.Reveal())
	}
	return httpx.JSON(w, http.StatusOK, enrollResponse{RecoveryCodes: codes, SessionIssued: issued})
}

func (h *Handler) verifyTOTP(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[codeRequest](w, r)
	if err != nil {
		return err
	}
	tok, err := challengeToken(r)
	if err != nil {
		return err
	}
	res, err := h.login.VerifyTOTP(r.Context(), tok, req.Code, clientMeta(r))
	if err != nil {
		return challengeErr(w, err)
	}
	h.complete(w, res)
	httpx.NoContent(w)
	return nil
}

func (h *Handler) verifyRecovery(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[codeRequest](w, r)
	if err != nil {
		return err
	}
	tok, err := challengeToken(r)
	if err != nil {
		return err
	}
	res, err := h.login.VerifyRecoveryCode(r.Context(), tok, req.Code, clientMeta(r))
	if err != nil {
		return challengeErr(w, err)
	}
	h.complete(w, res)
	return httpx.JSON(w, http.StatusOK, recoveryResponse{RecoveryCodesLeft: res.RecoveryCodesLeft})
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) error {
	meta := clientMeta(r)
	d, err := h.refreshRL.Take(r.Context(), refreshIPKey+ipOrUnknown(meta.IP))
	if err != nil {
		return err
	}
	if d > 0 {
		return ErrTooManyAttempts
	}
	raw := cookieValue(r, cookieRefresh)
	if raw == "" {
		clearSession(w)
		return ErrSessionInvalid
	}
	session, err := h.sessions.Refresh(r.Context(), raw, meta)
	if err != nil {
		if errors.Is(err, ErrSessionInvalid) {
			clearSession(w)
		}
		return err
	}
	setSession(w, session, h.cookies)
	httpx.NoContent(w)
	return nil
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) error {
	clearSession(w)
	if raw := cookieValue(r, cookieRefresh); raw != "" {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), logoutTimeout)
		defer cancel()
		if err := h.sessions.Logout(ctx, raw); err != nil {
			h.log.WarnContext(ctx, "logout revoke failed", "err", err)
		}
	}
	httpx.NoContent(w)
	return nil
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) error {
	p, ok := PrincipalFrom(r.Context())
	if !ok {
		return httpx.ErrUnauthenticated
	}
	email, left, err := h.store.Me(r.Context(), p.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrSessionInvalid
		}
		return err
	}
	return httpx.JSON(w, http.StatusOK, meResponse{Email: email, RecoveryCodesLeft: left})
}

type authenticator interface {
	Authenticate(ctx context.Context, rawAccess string) (Principal, error)
}

// RequireAdmin authenticates the access cookie and puts the Principal in the
// context. Only an invalid session is a 401; infrastructure errors pass through.
func RequireAdmin(a authenticator, errs *httpx.ErrorMapper) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := cookieValue(r, cookieAccess)
			if raw == "" {
				errs.Write(w, r, httpx.ErrUnauthenticated)
				return
			}
			p, err := a.Authenticate(r.Context(), raw)
			if err != nil {
				if errors.Is(err, ErrSessionInvalid) {
					errs.Write(w, r, httpx.ErrUnauthenticated)
					return
				}
				errs.Write(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
		})
	}
}
