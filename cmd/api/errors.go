package main

import (
	"net/http"

	"github.com/TomasSorgetti/auto-factura-api/internal/auth"
	"github.com/TomasSorgetti/auto-factura-api/internal/clients"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
)

func errorRules() []httpx.ErrorRule {
	return []httpx.ErrorRule{
		{Target: auth.ErrInvalidCredentials, Status: http.StatusUnauthorized, Code: "INVALID_CREDENTIALS", Message: "invalid email or password"},
		{Target: auth.ErrSessionInvalid, Status: http.StatusUnauthorized, Code: "SESSION_INVALID", Message: "session invalid or expired"},
		{Target: auth.ErrChallengeInvalid, Status: http.StatusUnauthorized, Code: "LOGIN_CHALLENGE_INVALID", Message: "login challenge invalid or expired"},
		{Target: auth.ErrInvalidTOTP, Status: http.StatusUnauthorized, Code: "INVALID_TOTP_CODE", Message: "invalid authenticator code"},
		{Target: auth.ErrInvalidRecoveryCode, Status: http.StatusUnauthorized, Code: "INVALID_RECOVERY_CODE", Message: "invalid recovery code"},
		{Target: auth.ErrTooManyAttempts, Status: http.StatusTooManyRequests, Code: "TOO_MANY_ATTEMPTS", Message: "too many attempts, try again later"},
		{Target: auth.ErrTOTPAlreadyEnabled, Status: http.StatusConflict, Code: "TOTP_ALREADY_ENABLED", Message: "authenticator already enabled"},
		{Target: auth.ErrTOTPNotEnabled, Status: http.StatusConflict, Code: "TOTP_NOT_ENABLED", Message: "authenticator not enabled"},
		{Target: clients.ErrNotFound, Status: http.StatusNotFound, Code: "NOT_FOUND", Message: "resource not found"},
		{Target: clients.ErrCUITTaken, Status: http.StatusConflict, Code: "CLIENT_CUIT_TAKEN", Message: "a client with this cuit already exists"},
		{Target: clients.ErrArchived, Status: http.StatusConflict, Code: "CLIENT_ARCHIVED", Message: "client is archived"},
		{Target: clients.ErrEmailTaken, Status: http.StatusConflict, Code: "CONTACT_EMAIL_TAKEN", Message: "contact email already exists for this client"},
		{Target: clients.ErrContactLimit, Status: http.StatusConflict, Code: "CONTACT_LIMIT_REACHED", Message: "client contact limit reached"},
	}
}
