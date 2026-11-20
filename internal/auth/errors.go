package auth

import "errors"

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrSessionInvalid     = errors.New("session invalid or expired")
	ErrNotFound           = errors.New("admin user not found")
)
