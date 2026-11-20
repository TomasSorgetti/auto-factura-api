package httpx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var errSentinel = errors.New("sentinel")

func TestErrorMapperWrite(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancelExpired()

	tests := []struct {
		name       string
		err        error
		ctx        context.Context
		wantStatus int
		wantCode   string
		wantFields map[string]string
		wantEmpty  bool
	}{
		{name: "direct", err: ErrNotFound, wantStatus: 404, wantCode: "NOT_FOUND"},
		{name: "wrapped", err: fmt.Errorf("load: %w", ErrForbidden), wantStatus: 403, wantCode: "FORBIDDEN"},
		{name: "field errors", err: FieldErrors{"name": "is required"}, wantStatus: 422, wantCode: "VALIDATION_FAILED", wantFields: map[string]string{"name": "is required"}},
		{name: "max bytes", err: &http.MaxBytesError{Limit: 1}, wantStatus: 413, wantCode: "PAYLOAD_TOO_LARGE"},
		{name: "rule", err: fmt.Errorf("x: %w", errSentinel), wantStatus: 409, wantCode: "CONFLICT_X"},
		{name: "deadline", err: fmt.Errorf("x: %w", context.DeadlineExceeded), wantStatus: 503, wantCode: "REQUEST_TIMEOUT"},
		{name: "canceled", err: context.Canceled, ctx: canceled, wantEmpty: true},
		{name: "canceled with expired deadline", err: context.Canceled, ctx: expired, wantStatus: 503, wantCode: "REQUEST_TIMEOUT"},
		{name: "canceled with live ctx", err: context.Canceled, wantStatus: 500, wantCode: "INTERNAL"},
		{name: "unknown", err: errors.New("secret internal detail"), wantStatus: 500, wantCode: "INTERNAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			log, logBuf := newTestLogger()
			m := NewErrorMapper(log, []ErrorRule{{Target: errSentinel, Status: 409, Code: "CONFLICT_X", Message: "conflict"}})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.ctx != nil {
				req = req.WithContext(tt.ctx)
			}
			rec := httptest.NewRecorder()
			m.Write(rec, req, tt.err)

			if tt.wantEmpty {
				if rec.Body.Len() != 0 || rec.Result().StatusCode != 200 {
					t.Fatalf("expected nothing written, got %d %q", rec.Code, rec.Body.String())
				}
				if !strings.Contains(logBuf.String(), "request canceled") {
					t.Fatalf("expected debug log, got %q", logBuf.String())
				}
				return
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status %d, want %d", rec.Code, tt.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
				t.Fatalf("content type %q", ct)
			}
			env := readEnvelope(t, rec)
			if env.Error.Code != tt.wantCode || env.Error.Status != tt.wantStatus {
				t.Fatalf("got %+v", env.Error)
			}
			for k, v := range tt.wantFields {
				if env.Error.Fields[k] != v {
					t.Fatalf("fields %v", env.Error.Fields)
				}
			}
			if tt.wantStatus == 500 {
				detail := tt.err.Error()
				if strings.Contains(rec.Body.String(), detail) {
					t.Fatal("body leaks internal error")
				}
				if !strings.Contains(logBuf.String(), detail) {
					t.Fatal("log must contain the internal error")
				}
			}
		})
	}
}

func TestErrorCopy(t *testing.T) {
	t.Parallel()
	c := ErrInvalidJSON.WithMessage("other").WithFields(map[string]string{"a": "b"})
	if c.Message != "other" || c.Fields["a"] != "b" {
		t.Fatalf("copy not updated: %+v", c)
	}
	if ErrInvalidJSON.Message != "invalid json body" || ErrInvalidJSON.Fields != nil {
		t.Fatalf("shared error mutated: %+v", ErrInvalidJSON)
	}
}
