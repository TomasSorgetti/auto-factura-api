package auth

import (
	"net/http"
	"time"
)

const (
	cookieAccess      = "__Host-af_access"
	cookieRefresh     = "__Secure-af_refresh"
	cookieChallenge   = "__Secure-af_challenge"
	cookieDevice      = "__Secure-af_device"
	cookieSessionHint = "af_session"

	authPath         = "/api/auth"
	challengeMaxAge  = challengeTTL
	sessionHintValue = "1"
)

type cookieConfig struct {
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

func put(w http.ResponseWriter, name, value, path string, maxAge time.Duration, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   int(maxAge / time.Second),
		HttpOnly: httpOnly,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

func drop(w http.ResponseWriter, name, path string, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Path:     path,
		MaxAge:   -1,
		HttpOnly: httpOnly,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

func setSession(w http.ResponseWriter, s IssuedSession, cfg cookieConfig) {
	put(w, cookieAccess, s.AccessToken.Reveal(), "/", cfg.AccessTTL, true)
	put(w, cookieRefresh, s.RefreshToken.Reveal(), authPath, cfg.RefreshTTL, true)
	put(w, cookieSessionHint, sessionHintValue, "/", cfg.RefreshTTL, false)
}

func clearSession(w http.ResponseWriter) {
	drop(w, cookieAccess, "/", true)
	drop(w, cookieRefresh, authPath, true)
	drop(w, cookieSessionHint, "/", false)
}

func setChallenge(w http.ResponseWriter, token string) {
	put(w, cookieChallenge, token, authPath, challengeMaxAge, true)
}

func clearChallenge(w http.ResponseWriter) {
	drop(w, cookieChallenge, authPath, true)
}

func setDevice(w http.ResponseWriter, deviceID string) {
	if deviceID == "" {
		return
	}
	put(w, cookieDevice, deviceID, authPath, DeviceCookieMaxAge, true)
}
