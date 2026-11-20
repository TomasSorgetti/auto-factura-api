package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

// Validator is implemented by request DTOs; Validate usually returns FieldErrors.
type Validator interface {
	Validate() error
}

const MaxJSONBody int64 = 1 << 20

var errNotObject = ErrInvalidJSON.WithMessage("body must be a json object")

// Decode reads a single JSON object of at most MaxJSONBody bytes, rejects
// unknown fields and runs Validate.
func Decode[T Validator](w http.ResponseWriter, r *http.Request) (T, error) {
	var v T
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return v, ErrUnsupportedMediaType
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBody)
	dec := json.NewDecoder(r.Body)
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return v, decodeError(err)
	}
	if raw = bytes.TrimSpace(raw); len(raw) == 0 || raw[0] != '{' {
		return v, errNotObject
	}
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	if err := strict.Decode(&v); err != nil {
		return v, decodeError(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return v, err
		}
		return v, ErrInvalidJSON.WithMessage("body must contain a single json object")
	}
	if err := v.Validate(); err != nil {
		return v, err
	}
	return v, nil
}

func decodeError(err error) error {
	var (
		syntax *json.SyntaxError
		typ    *json.UnmarshalTypeError
		mbe    *http.MaxBytesError
	)
	switch {
	case errors.Is(err, io.EOF):
		return ErrInvalidJSON.WithMessage("request body is empty")
	case errors.As(err, &mbe):
		return err
	case errors.As(err, &syntax), errors.Is(err, io.ErrUnexpectedEOF):
		return ErrInvalidJSON.WithMessage("malformed json")
	case errors.As(err, &typ):
		if typ.Field == "" {
			return errNotObject
		}
		return FieldErrors{typ.Field: "invalid type"}
	}
	if name, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		if runes := []rune(name); len(runes) > 64 {
			name = string(runes[:64])
		}
		return ErrInvalidJSON.WithMessage("unknown field " + name)
	}
	return ErrInvalidJSON
}
