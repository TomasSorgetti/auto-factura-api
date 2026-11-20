package settings

import "errors"

// ErrNotConfigured means the settings row does not exist yet.
var ErrNotConfigured = errors.New("settings not configured")
