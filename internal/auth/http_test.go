package auth

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/redisx"
)

func testErrorRules() []httpx.ErrorRule {
	return []httpx.ErrorRule{
		{Target: ErrInvalidCredentials, Status: 401, Code: "INVALID_CREDENTIALS", Message: "x"},
		{Target: ErrSessionInvalid, Status: 401, Code: "SESSION_INVALID", Message: "x"},
		{Target: ErrChallengeInvalid, Status: 401, Code: "LOGIN_CHALLENGE_INVALID", Message: "x"},
		{Target: ErrInvalidTOTP, Status: 401, Code: "INVALID_TOTP_CODE", Message: "x"},
		{Target: ErrInvalidRecoveryCode, Status: 401, Code: "INVALID_RECOVERY_CODE", Message: "x"},
		{Target: ErrTooManyAttempts, Status: 429, Code: "TOO_MANY_ATTEMPTS", Message: "x"},
		{Target: ErrTOTPAlreadyEnabled, Status: 409, Code: "TOTP_ALREADY_ENABLED", Message: "x"},
		{Target: ErrTOTPNotEnabled, Status: 409, Code: "TOTP_NOT_ENABLED", Message: "x"},
	}
}

type httpEnv struct {
	*loginEnv
	handler http.Handler
	jar     map[string]*http.Cookie
	all     []*http.Cookie
}

func newHTTPEnv(t *testing.T) *httpEnv {
	t.Helper()
	e := newLoginEnv(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	errs := httpx.NewErrorMapper(log, testErrorRules())
	prefix := "logintest:httprl:" + strings.ReplaceAll(t.Name(), "/", "_") + ":" + time.Now().Format("150405.000000000")
	t.Cleanup(func() {
		ctx := context.Background()
		iter := e.rdb.Scan(ctx, 0, prefix+"*", 100).Iterator()
		for iter.Next(ctx) {
			e.rdb.Del(ctx, iter.Val())
		}
	})
	rl := redisx.NewLimiter(e.rdb, redisx.LimiterConfig{Prefix: prefix, MaxFailures: 3, Window: time.Minute, MaxBlock: time.Minute})
	h, err := NewHandler(HandlerDeps{
		Login: e.login, Sessions: e.sessions, Store: NewStore(e.pool), RefreshLimiter: rl,
		AccessTTL: 15 * time.Minute, RefreshTTL: 168 * time.Hour, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := httpx.NewRouter(httpx.RouterConfig{Errors: errs, RequireAdmin: RequireAdmin(e.sessions, errs)})
	if err != nil {
		t.Fatal(err)
	}
	h.Register(rt)
	return &httpEnv{loginEnv: e, handler: rt, jar: map[string]*http.Cookie{}}
}

func (e *httpEnv) do(method, path, body string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range e.jar {
		if strings.HasPrefix(path, c.Path) {
			req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
		}
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	e.all = e.all[:0]
	for _, c := range rec.Result().Cookies() {
		e.all = append(e.all, c)
		if c.MaxAge < 0 {
			delete(e.jar, c.Name)
		} else {
			e.jar[c.Name] = c
		}
	}
	return rec
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var b struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return b.Error.Code
}

func decodeData[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var b struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return b.Data
}

func (e *httpEnv) loginBody() string {
	return `{"email":"` + e.email + `","password":"` + loginPassword + `"}`
}

func (e *httpEnv) cookie(name string) *http.Cookie { return e.jar[name] }

func checkCookie(t *testing.T, c *http.Cookie, path string, httpOnly bool) {
	t.Helper()
	if c == nil {
		t.Fatal("missing cookie")
	}
	if c.Path != path || c.HttpOnly != httpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Domain != "" || c.MaxAge <= 0 {
		t.Fatalf("cookie %s: path=%q httpOnly=%v secure=%v sameSite=%v domain=%q maxAge=%d", c.Name, c.Path, c.HttpOnly, c.Secure, c.SameSite, c.Domain, c.MaxAge)
	}
}

func checkSessionCookies(t *testing.T, e *httpEnv) {
	t.Helper()
	checkCookie(t, e.cookie(cookieAccess), "/", true)
	checkCookie(t, e.cookie(cookieRefresh), authPath, true)
	checkCookie(t, e.cookie(cookieDevice), authPath, true)
	hint := e.cookie(cookieSessionHint)
	checkCookie(t, hint, "/", false)
	if hint.Value != "1" {
		t.Fatalf("hint = %q", hint.Value)
	}
	if e.cookie(cookieChallenge) != nil {
		t.Fatal("challenge cookie not cleared")
	}
}

func TestAuthFlow(t *testing.T) {
	e := newHTTPEnv(t)

	rec := e.do("POST", "/api/auth/login", e.loginBody())
	if rec.Code != 200 {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	if next := decodeData[struct{ Next string }](t, rec).Next; next != "totp_setup" {
		t.Fatalf("next = %q", next)
	}
	checkCookie(t, e.cookie(cookieChallenge), authPath, true)
	if strings.Contains(rec.Body.String(), e.cookie(cookieChallenge).Value) {
		t.Fatal("challenge token in body")
	}

	rec = e.do("POST", "/api/auth/totp/setup", "")
	if rec.Code != 200 {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body)
	}
	setup := decodeData[totpSetupResponse](t, rec)
	if setup.Secret == "" || !strings.HasPrefix(setup.QRCode, "data:image/png;base64,") {
		t.Fatalf("setup = %+v", setup)
	}

	rec = e.do("POST", "/api/auth/totp/enroll", `{"code":"`+codeFor(t, setup.Secret, time.Now())+`"}`)
	if rec.Code != 200 {
		t.Fatalf("enroll: %d %s", rec.Code, rec.Body)
	}
	if enr := decodeData[enrollResponse](t, rec); len(enr.RecoveryCodes) != recoveryCount || !enr.SessionIssued {
		t.Fatalf("enroll = %+v", enr)
	}
	checkSessionCookies(t, e)

	rec = e.do("GET", "/api/admin/me", "")
	if rec.Code != 200 {
		t.Fatalf("me: %d %s", rec.Code, rec.Body)
	}
	if me := decodeData[meResponse](t, rec); me.Email != e.email || me.RecoveryCodesLeft != recoveryCount {
		t.Fatalf("me = %+v", me)
	}
}

func TestAuthLoginTOTP(t *testing.T) {
	e := newHTTPEnv(t)
	secret := e.enableTOTP(t)

	rec := e.do("POST", "/api/auth/login", e.loginBody())
	if rec.Code != 200 || decodeData[struct{ Next string }](t, rec).Next != "totp" {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	rec = e.do("POST", "/api/auth/totp", `{"code":"`+codeFor(t, secret, time.Now())+`"}`)
	if rec.Code != 204 {
		t.Fatalf("totp: %d %s", rec.Code, rec.Body)
	}
	checkSessionCookies(t, e)
	if rec := e.do("GET", "/api/admin/me", ""); rec.Code != 200 {
		t.Fatalf("me: %d", rec.Code)
	}
}

func TestAuthLoginRecovery(t *testing.T) {
	e := newHTTPEnv(t)
	secret := e.enableTOTP(t)
	e.do("POST", "/api/auth/login", e.loginBody())
	if rec := e.do("POST", "/api/auth/totp", `{"code":"`+codeFor(t, secret, time.Now())+`"}`); rec.Code != 204 {
		t.Fatalf("totp: %d", rec.Code)
	}
	if rec := e.do("POST", "/api/auth/recovery", `{"code":"AAAAA-AAAAA"}`); errCode(t, rec) != "LOGIN_CHALLENGE_INVALID" {
		t.Fatalf("recovery without challenge: %d %s", rec.Code, rec.Body)
	}
}

func TestAuthErrors(t *testing.T) {
	t.Run("wrong password", func(t *testing.T) {
		e := newHTTPEnv(t)
		rec := e.do("POST", "/api/auth/login", `{"email":"`+e.email+`","password":"nope"}`)
		if rec.Code != 401 || errCode(t, rec) != "INVALID_CREDENTIALS" {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		if e.cookie(cookieChallenge) != nil {
			t.Fatal("challenge set on failure")
		}
	})
	t.Run("totp without challenge", func(t *testing.T) {
		e := newHTTPEnv(t)
		rec := e.do("POST", "/api/auth/totp", `{"code":"123456"}`)
		if rec.Code != 401 || errCode(t, rec) != "LOGIN_CHALLENGE_INVALID" {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})
	t.Run("invalid challenge is cleared", func(t *testing.T) {
		e := newHTTPEnv(t)
		e.jar[cookieChallenge] = &http.Cookie{Name: cookieChallenge, Value: "bogus", Path: authPath}
		rec := e.do("POST", "/api/auth/totp", `{"code":"123456"}`)
		if errCode(t, rec) != "LOGIN_CHALLENGE_INVALID" || e.cookie(cookieChallenge) != nil {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})
	t.Run("unknown field", func(t *testing.T) {
		e := newHTTPEnv(t)
		rec := e.do("POST", "/api/auth/login", `{"email":"a@b.c","password":"x","extra":1}`)
		if rec.Code != 400 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})
	t.Run("validation", func(t *testing.T) {
		e := newHTTPEnv(t)
		rec := e.do("POST", "/api/auth/login", `{"email":"x","password":""}`)
		if rec.Code != 422 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})
	t.Run("too many logins", func(t *testing.T) {
		e := newHTTPEnv(t)
		body := `{"email":"` + e.email + `","password":"nope"}`
		for i := 0; i < 5; i++ {
			if rec := e.do("POST", "/api/auth/login", body); rec.Code != 401 {
				t.Fatalf("attempt %d: %d", i, rec.Code)
			}
		}
		rec := e.do("POST", "/api/auth/login", body)
		if rec.Code != 429 || errCode(t, rec) != "TOO_MANY_ATTEMPTS" {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})
}

func (e *httpEnv) signIn(t *testing.T) {
	t.Helper()
	secret := e.enableTOTP(t)
	e.do("POST", "/api/auth/login", e.loginBody())
	if rec := e.do("POST", "/api/auth/totp", `{"code":"`+codeFor(t, secret, time.Now())+`"}`); rec.Code != 204 {
		t.Fatalf("totp: %d %s", rec.Code, rec.Body)
	}
}

func TestAuthRefreshAndLogout(t *testing.T) {
	t.Run("refresh rotates cookies", func(t *testing.T) {
		e := newHTTPEnv(t)
		e.signIn(t)
		old := e.cookie(cookieRefresh).Value
		rec := e.do("POST", "/api/auth/refresh", "")
		if rec.Code != 204 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		checkCookie(t, e.cookie(cookieAccess), "/", true)
		checkCookie(t, e.cookie(cookieRefresh), authPath, true)
		checkCookie(t, e.cookie(cookieSessionHint), "/", false)
		if e.cookie(cookieRefresh).Value == old {
			t.Fatal("refresh token not rotated")
		}
	})
	t.Run("refresh without cookie clears session", func(t *testing.T) {
		e := newHTTPEnv(t)
		rec := e.do("POST", "/api/auth/refresh", "")
		if rec.Code != 401 || errCode(t, rec) != "SESSION_INVALID" {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		cleared := map[string]bool{}
		for _, c := range e.all {
			if c.MaxAge < 0 {
				cleared[c.Name] = true
			}
		}
		if !cleared[cookieAccess] || !cleared[cookieRefresh] || !cleared[cookieSessionHint] {
			t.Fatalf("cleared = %v", cleared)
		}
	})
	t.Run("refresh with bad token clears session", func(t *testing.T) {
		e := newHTTPEnv(t)
		e.jar[cookieRefresh] = &http.Cookie{Name: cookieRefresh, Value: "garbage", Path: authPath}
		rec := e.do("POST", "/api/auth/refresh", "")
		if rec.Code != 401 || e.cookie(cookieRefresh) != nil {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})
	t.Run("logout revokes the refresh token", func(t *testing.T) {
		e := newHTTPEnv(t)
		e.signIn(t)
		old := *e.cookie(cookieRefresh)
		if rec := e.do("POST", "/api/auth/logout", ""); rec.Code != 204 {
			t.Fatalf("logout: %d", rec.Code)
		}
		if e.cookie(cookieAccess) != nil || e.cookie(cookieRefresh) != nil || e.cookie(cookieSessionHint) != nil {
			t.Fatal("cookies not cleared")
		}
		e.jar[cookieRefresh] = &old
		rec := e.do("POST", "/api/auth/refresh", "")
		if rec.Code != 401 || errCode(t, rec) != "SESSION_INVALID" {
			t.Fatalf("refresh after logout: %d %s", rec.Code, rec.Body)
		}
	})
	t.Run("logout without session is 204", func(t *testing.T) {
		e := newHTTPEnv(t)
		if rec := e.do("POST", "/api/auth/logout", ""); rec.Code != 204 {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("refresh is rate limited per ip", func(t *testing.T) {
		e := newHTTPEnv(t)
		for i := 0; i < 3; i++ {
			e.do("POST", "/api/auth/refresh", "")
		}
		rec := e.do("POST", "/api/auth/refresh", "")
		if rec.Code != 429 || errCode(t, rec) != "TOO_MANY_ATTEMPTS" {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})
}

func TestRequireAdmin(t *testing.T) {
	t.Run("no cookie", func(t *testing.T) {
		e := newHTTPEnv(t)
		rec := e.do("GET", "/api/admin/me", "")
		if rec.Code != 401 || errCode(t, rec) != "UNAUTHENTICATED" {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})
	t.Run("garbage cookie", func(t *testing.T) {
		e := newHTTPEnv(t)
		e.jar[cookieAccess] = &http.Cookie{Name: cookieAccess, Value: "garbage", Path: "/"}
		rec := e.do("GET", "/api/admin/me", "")
		if rec.Code != 401 || errCode(t, rec) != "UNAUTHENTICATED" {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})
	t.Run("expired access", func(t *testing.T) {
		e := newHTTPEnv(t)
		e.signIn(t)
		e.clock.Advance(time.Hour)
		rec := e.do("GET", "/api/admin/me", "")
		if rec.Code != 401 || errCode(t, rec) != "UNAUTHENTICATED" {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})
	t.Run("valid access sets principal", func(t *testing.T) {
		e := newHTTPEnv(t)
		e.signIn(t)
		var got Principal
		h := RequireAdmin(e.sessions, httpx.NewErrorMapper(slog.New(slog.NewTextHandler(io.Discard, nil)), nil))(
			http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got, _ = PrincipalFrom(r.Context()) }))
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: cookieAccess, Value: e.cookie(cookieAccess).Value})
		h.ServeHTTP(httptest.NewRecorder(), req)
		if got.UserID != e.userID {
			t.Fatalf("principal = %+v", got)
		}
	})
}
