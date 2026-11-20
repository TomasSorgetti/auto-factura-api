package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
)

// Error is an HTTP-facing error. Platform errors are shared values: use
// WithMessage and WithFields, which return copies.
type Error struct {
	Status  int
	Code    string
	Message string
	Fields  map[string]string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func (e *Error) WithMessage(msg string) *Error {
	c := *e
	c.Message = msg
	return &c
}

func (e *Error) WithFields(f map[string]string) *Error {
	c := *e
	c.Fields = maps.Clone(f)
	return &c
}

var (
	ErrInvalidJSON          = &Error{Status: http.StatusBadRequest, Code: "INVALID_JSON", Message: "invalid json body"}
	ErrUnauthenticated      = &Error{Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED", Message: "authentication required"}
	ErrForbidden            = &Error{Status: http.StatusForbidden, Code: "FORBIDDEN", Message: "forbidden"}
	ErrCrossOrigin          = &Error{Status: http.StatusForbidden, Code: "CROSS_ORIGIN_REQUEST", Message: "cross-origin request rejected"}
	ErrNotFound             = &Error{Status: http.StatusNotFound, Code: "NOT_FOUND", Message: "resource not found"}
	ErrMethodNotAllowed     = &Error{Status: http.StatusMethodNotAllowed, Code: "METHOD_NOT_ALLOWED", Message: "method not allowed"}
	ErrPayloadTooLarge      = &Error{Status: http.StatusRequestEntityTooLarge, Code: "PAYLOAD_TOO_LARGE", Message: "request body too large"}
	ErrUnsupportedMediaType = &Error{Status: http.StatusUnsupportedMediaType, Code: "UNSUPPORTED_MEDIA_TYPE", Message: "content type must be application/json"}
	ErrInternal             = &Error{Status: http.StatusInternalServerError, Code: "INTERNAL", Message: "internal error"}
	ErrRequestTimeout       = &Error{Status: http.StatusServiceUnavailable, Code: "REQUEST_TIMEOUT", Message: "request timed out"}
	ErrServiceNotReady      = &Error{Status: http.StatusServiceUnavailable, Code: "SERVICE_NOT_READY", Message: "service not ready"}

	errValidationFailed = &Error{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED", Message: "validation failed"}
)

// FieldErrors maps field names to validation messages; it maps to 422.
type FieldErrors map[string]string

func (f FieldErrors) Error() string {
	keys := slices.Sorted(maps.Keys(f))
	return "validation failed: " + strings.Join(keys, ", ")
}

// ErrorRule maps a domain error, matched with errors.Is, to an HTTP error.
type ErrorRule struct {
	Target  error
	Status  int
	Code    string
	Message string
}

// ErrorMapper turns any error into the JSON error envelope. It is immutable
// after construction and safe for concurrent use.
type ErrorMapper struct {
	log   *slog.Logger
	rules []ErrorRule
}

func NewErrorMapper(log *slog.Logger, rules []ErrorRule) *ErrorMapper {
	return &ErrorMapper{log: log, rules: slices.Clone(rules)}
}

// Write responds with err mapped to the error envelope. The original error
// only reaches the log, never the body. A canceled request writes nothing.
func (m *ErrorMapper) Write(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	e, ok := m.resolve(err)
	if !ok {
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			m.log.DebugContext(ctx, "request canceled")
			return
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			e = ErrRequestTimeout
		default:
			e = ErrInternal
		}
	}
	if info := requestInfoFrom(ctx); info != nil {
		info.code = e.Code
	}
	if e.Status >= http.StatusInternalServerError {
		m.log.ErrorContext(ctx, "request failed", "code", e.Code, "err", err)
	}
	m.writeError(w, r, e)
}

func (m *ErrorMapper) resolve(err error) (*Error, bool) {
	var he *Error
	if errors.As(err, &he) {
		return he, true
	}
	var fe FieldErrors
	if errors.As(err, &fe) {
		return errValidationFailed.WithFields(fe), true
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return ErrPayloadTooLarge, true
	}
	for _, rule := range m.rules {
		if errors.Is(err, rule.Target) {
			return &Error{Status: rule.Status, Code: rule.Code, Message: rule.Message}, true
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrRequestTimeout, true
	}
	if errors.Is(err, context.Canceled) {
		return nil, false
	}
	return ErrInternal, true
}

type errorBody struct {
	Status  int               `json:"status"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (m *ErrorMapper) writeError(w http.ResponseWriter, r *http.Request, e *Error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(e.Status)
	body := struct {
		Error errorBody `json:"error"`
	}{errorBody{Status: e.Status, Code: e.Code, Message: e.Message, Fields: e.Fields}}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		m.log.WarnContext(r.Context(), "write error response", "err", err)
	}
}
