package clients

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
)

const validCUIT = "20123456786"

func validClientInput() ClientInput {
	return ClientInput{LegalName: "Acme SA", CUIT: validCUIT, TaxCondition: "registered", Address: "Calle 123", Notes: "nota"}
}

func fieldErrs(t *testing.T, err error) httpx.FieldErrors {
	t.Helper()
	var fe httpx.FieldErrors
	if !errors.As(err, &fe) {
		t.Fatalf("expected FieldErrors, got %v", err)
	}
	return fe
}

func TestClientInputValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*ClientInput)
		field  string
		msg    string
	}{
		{"valid", func(*ClientInput) {}, "", ""},
		{"cuit with dashes", func(in *ClientInput) { in.CUIT = "20-12345678-6" }, "", ""},
		{"invalid cuit", func(in *ClientInput) { in.CUIT = "20123456780" }, "cuit", "invalid cuit"},
		{"empty legal name", func(in *ClientInput) { in.LegalName = "  " }, "legalName", "required"},
		{"long legal name", func(in *ClientInput) { in.LegalName = strings.Repeat("ñ", 201) }, "legalName", "too long"},
		{"nul legal name", func(in *ClientInput) { in.LegalName = "Ac\x00me" }, "legalName", "invalid characters"},
		{"tax condition", func(in *ClientInput) { in.TaxCondition = "other" }, "taxCondition", "invalid value"},
		{"long notes", func(in *ClientInput) { in.Notes = strings.Repeat("a", 2001) }, "notes", "too long"},
		{"notes newline", func(in *ClientInput) { in.Notes = "a\nb\tc" }, "", ""},
		{"notes bell", func(in *ClientInput) { in.Notes = "a\x07b" }, "notes", "invalid characters"},
		{"empty notes", func(in *ClientInput) { in.Notes = "" }, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := validClientInput()
			tt.mutate(&in)
			f, err := in.Validate()
			if tt.field == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if f.CUIT != validCUIT {
					t.Fatalf("cuit = %q", f.CUIT)
				}
				return
			}
			fe := fieldErrs(t, err)
			if fe[tt.field] != tt.msg {
				t.Fatalf("fields = %v", fe)
			}
		})
	}

	t.Run("messages never contain the value", func(t *testing.T) {
		t.Parallel()
		in := ClientInput{LegalName: "SECRETNAME\x00", CUIT: "SECRETCUIT", TaxCondition: "SECRETTAX", Address: "SECRETADDR\x01", Notes: "SECRETNOTE\x07"}
		_, err := in.Validate()
		fe := fieldErrs(t, err)
		if len(fe) != 5 {
			t.Fatalf("fields = %v", fe)
		}
		for k, v := range fe {
			if strings.Contains(v, "SECRET") {
				t.Errorf("%s leaks value: %q", k, v)
			}
		}
	})
}

func TestContactInputValidate(t *testing.T) {
	t.Parallel()
	valid := ContactInput{Name: "Ana", Email: "Ana@Ejemplo.com", Role: "to", PortalAccess: true}
	tests := []struct {
		name   string
		mutate func(*ContactInput)
		field  string
		msg    string
	}{
		{"valid lowercases", func(*ContactInput) {}, "", ""},
		{"display name", func(in *ContactInput) { in.Email = "Ana <a@b.com>" }, "email", "invalid email"},
		{"comment", func(in *ContactInput) { in.Email = "a@b.com (x)" }, "email", "invalid email"},
		{"too long", func(in *ContactInput) { in.Email = strings.Repeat("a", 60) + "@" + strings.Repeat("b", 190) + ".com" }, "email", "invalid email"},
		{"empty email", func(in *ContactInput) { in.Email = "" }, "email", "invalid email"},
		{"bad role", func(in *ContactInput) { in.Role = "bcc" }, "role", "invalid value"},
		{"empty name", func(in *ContactInput) { in.Name = " " }, "name", "required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := valid
			tt.mutate(&in)
			f, err := in.Validate()
			if tt.field == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if f.Email != "ana@ejemplo.com" || !f.PortalAccess || f.Role != RoleTo {
					t.Fatalf("fields = %+v", f)
				}
				return
			}
			if fe := fieldErrs(t, err); fe[tt.field] != tt.msg {
				t.Fatalf("fields = %v", fe)
			}
		})
	}
}

func TestListParamsValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		in    ListParams
		field string
		limit int
	}{
		{"default limit", ListParams{}, "", DefaultLimit},
		{"max limit", ListParams{Limit: 100}, "", 100},
		{"limit too big", ListParams{Limit: 101}, "limit", 0},
		{"negative limit", ListParams{Limit: -1}, "limit", 0},
		{"negative offset", ListParams{Offset: -1}, "offset", 0},
		{"offset too big", ListParams{Offset: 10001}, "offset", 0},
		{"query too long", ListParams{Query: strings.Repeat("ñ", 101)}, "q", 0},
		{"query control char", ListParams{Query: "a" + string(rune(0)) + "b"}, "q", 0},
		{"query at limit", ListParams{Query: strings.Repeat("ñ", 100)}, "", DefaultLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.in.Validate()
			if tt.field == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got.Limit != tt.limit {
					t.Fatalf("limit = %d", got.Limit)
				}
				return
			}
			if _, ok := fieldErrs(t, err)[tt.field]; !ok {
				t.Fatalf("missing error for %s", tt.field)
			}
		})
	}
}

func TestLogValueRedacts(t *testing.T) {
	t.Parallel()
	const name, email, notes = "Zulema Quiroga", "zulema@privado.example", "nota confidencial"
	now := time.Now()
	client := Client{ID: "c1", ClientFields: ClientFields{LegalName: name, Notes: notes}}
	values := map[string]any{
		"contact":       Contact{ID: "k1", ClientID: "c1", ContactFields: ContactFields{Name: name, Email: email, Role: RoleTo}, CreatedAt: now},
		"contactFields": ContactFields{Name: name, Email: email, Role: RoleCc},
		"contactInput":  ContactInput{Name: name, Email: email, Role: "to"},
		"clientInput":   ClientInput{LegalName: name, Notes: notes, Address: email},
		"clientFields":  client.ClientFields,
		"client":        client,
		"detail":        Detail{Client: client, Contacts: []Contact{{Name: name, Email: email}}},
	}
	for label, v := range values {
		for _, h := range []func(*bytes.Buffer) slog.Handler{
			func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
			func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
		} {
			var buf bytes.Buffer
			slog.New(h(&buf)).Info("x", "v", v)
			out := buf.String()
			for _, secret := range []string{name, email, notes} {
				if strings.Contains(out, secret) {
					t.Errorf("%s leaked %q: %s", label, secret, out)
				}
			}
		}
	}
}
