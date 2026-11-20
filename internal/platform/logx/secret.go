package logx

import (
	"encoding/json"
	"log/slog"
	"strconv"
)

const redacted = "[REDACTED]"

// Secret holds a sensitive string that is redacted when logged or printed.
// Reveal is the only way to read the value.
type Secret string

func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

func (Secret) String() string { return redacted }

func (Secret) GoString() string { return strconv.Quote(redacted) }

func (Secret) MarshalJSON() ([]byte, error) { return json.Marshal(redacted) }

func (s Secret) Reveal() string { return string(s) }
