package settings

import "errors"

// ErrNotConfigured means the settings row does not exist yet.
var ErrNotConfigured = errors.New("settings not configured")

// ErrConfirmationMismatch means the confirmation CUIT does not match the stored one.
var ErrConfirmationMismatch = errors.New("confirmation cuit does not match the stored cuit")

// ErrInvalidArcaEnv means the requested environment is neither homologation nor production.
var ErrInvalidArcaEnv = errors.New("invalid arca environment")
