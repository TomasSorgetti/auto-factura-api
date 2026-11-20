package auth

import "errors"

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrSessionInvalid     = errors.New("session invalid or expired")
	ErrNotFound           = errors.New("admin user not found")
	ErrAdminExists        = errors.New("admin user already exists")
	ErrWeakPassword       = errors.New("password must be between 12 and 128 bytes")
)

var (
	ErrChallengeInvalid    = errors.New("login challenge invalid or expired")
	ErrInvalidTOTP         = errors.New("invalid totp code")
	ErrInvalidRecoveryCode = errors.New("invalid recovery code")
	ErrTooManyAttempts     = errors.New("too many attempts")
	ErrTOTPAlreadyEnabled  = errors.New("totp already enabled")
	ErrTOTPNotEnabled      = errors.New("totp not enabled")
)

// ErrSetupSessionNotIssued means TOTP was enabled and recovery codes were
// created, but the session could not be issued. The LoginResult returned with
// it still carries the RecoveryCodes, which are shown only once.
var ErrSetupSessionNotIssued = errors.New("totp enabled but session not issued")
