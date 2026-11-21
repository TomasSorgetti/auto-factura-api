package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TomasSorgetti/auto-factura-api/internal/clients"
)

func TestClientsErrorRules(t *testing.T) {
	t.Parallel()

	_, errs := testDeps()
	const secret = "30712345678 persona@example.com"
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{clients.ErrNotFound, http.StatusNotFound, "NOT_FOUND"},
		{clients.ErrCUITTaken, http.StatusConflict, "CLIENT_CUIT_TAKEN"},
		{clients.ErrArchived, http.StatusConflict, "CLIENT_ARCHIVED"},
		{clients.ErrEmailTaken, http.StatusConflict, "CONTACT_EMAIL_TAKEN"},
		{clients.ErrContactLimit, http.StatusConflict, "CONTACT_LIMIT_REACHED"},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			errs.Write(rec, httptest.NewRequest("GET", "/", nil), fmt.Errorf("op %s: %w", secret, tt.err))
			if rec.Code != tt.status || errorCode(t, rec) != tt.code {
				t.Fatalf("status %d body %s", rec.Code, rec.Body)
			}
			if strings.Contains(rec.Body.String(), "30712345678") || strings.Contains(rec.Body.String(), "persona@example.com") {
				t.Errorf("body leaks wrapped values: %s", rec.Body)
			}
		})
	}
	if errors.Is(clients.ErrNotFound, clients.ErrArchived) {
		t.Fatal("sentinels must be distinct")
	}
}
