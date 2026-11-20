package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testDTO struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func (d testDTO) Validate() error {
	if d.Name == "fail" {
		return FieldErrors{"name": "is invalid"}
	}
	return nil
}

type ptrDTO struct{ Name string }

func (d *ptrDTO) Validate() error {
	if d.Name == "" {
		return FieldErrors{"Name": "is required"}
	}
	return nil
}

func TestDecodePointerRejectsNull(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`null`, ` null`, `[]`, `"x"`, `1`} {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		_, err := Decode[*ptrDTO](httptest.NewRecorder(), req)
		var he *Error
		if !errors.As(err, &he) || he.Message != "body must be a json object" {
			t.Fatalf("body %q: got %v", body, err)
		}
	}
}

func TestDecode(t *testing.T) {
	t.Parallel()

	big := `{"name":"` + strings.Repeat("a", int(MaxJSONBody)) + `"}`
	tests := []struct {
		name     string
		ctype    string
		body     string
		wantMsg  string
		wantErr  error
		wantType string
	}{
		{name: "ok", ctype: "application/json", body: `{"name":"a","age":3}`},
		{name: "charset", ctype: "application/json; charset=utf-8", body: `{"name":"a"}`},
		{name: "no content type", body: `{}`, wantErr: ErrUnsupportedMediaType},
		{name: "empty", ctype: "application/json", body: ``, wantErr: ErrInvalidJSON, wantMsg: "request body is empty"},
		{name: "malformed", ctype: "application/json", body: `{"name":`, wantErr: ErrInvalidJSON, wantMsg: "malformed json"},
		{name: "unknown field", ctype: "application/json", body: `{"x":1}`, wantErr: ErrInvalidJSON, wantMsg: `unknown field "x"`},
		{name: "invalid type", ctype: "application/json", body: `{"age":"x"}`, wantType: "age"},
		{name: "two objects", ctype: "application/json", body: `{} {}`, wantErr: ErrInvalidJSON, wantMsg: "body must contain a single json object"},
		{name: "null body", ctype: "application/json", body: `null`, wantErr: ErrInvalidJSON, wantMsg: "body must be a json object"},
		{name: "array body", ctype: "application/json", body: `[1]`, wantErr: ErrInvalidJSON, wantMsg: "body must be a json object"},
		{name: "long unknown field", ctype: "application/json", body: `{"` + strings.Repeat("z", 200) + `":1}`, wantErr: ErrInvalidJSON, wantMsg: `unknown field "` + strings.Repeat("z", 63)},
		{name: "too large", ctype: "application/json", body: big},
		{name: "validate fails", ctype: "application/json", body: `{"name":"fail"}`, wantType: "name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			if tt.ctype != "" {
				req.Header.Set("Content-Type", tt.ctype)
			}
			_, err := Decode[testDTO](httptest.NewRecorder(), req)

			switch {
			case tt.name == "too large":
				var mbe *http.MaxBytesError
				if !errors.As(err, &mbe) {
					t.Fatalf("want MaxBytesError, got %v", err)
				}
			case tt.wantType != "":
				var fe FieldErrors
				if !errors.As(err, &fe) || fe[tt.wantType] == "" {
					t.Fatalf("want field error on %s, got %v", tt.wantType, err)
				}
			case tt.wantErr != nil:
				var he *Error
				if !errors.As(err, &he) || he.Code != tt.wantErr.(*Error).Code {
					t.Fatalf("want %v, got %v", tt.wantErr, err)
				}
				if tt.wantMsg != "" && he.Message != tt.wantMsg {
					t.Fatalf("message %q, want %q", he.Message, tt.wantMsg)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}
