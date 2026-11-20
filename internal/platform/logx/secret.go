package logx

import "log/slog"

const redacted = "[REDACTED]"

// Secret holds a sensitive string that is redacted when logged or printed.
// Reveal is the only way to read the value.
type Secret string

func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

func (Secret) String() string { return redacted }

func (s Secret) Reveal() string { return string(s) }
