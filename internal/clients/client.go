// Package clients holds clients and their contacts. Personal fields are encrypted at rest.
package clients

import (
	"log/slog"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/TomasSorgetti/auto-factura-api/internal/cuit"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
)

type TaxCondition string

const (
	TaxRegistered    TaxCondition = "registered"
	TaxMonotributo   TaxCondition = "monotributo"
	TaxExempt        TaxCondition = "exempt"
	TaxFinalConsumer TaxCondition = "final_consumer"
)

func (t TaxCondition) valid() bool {
	switch t {
	case TaxRegistered, TaxMonotributo, TaxExempt, TaxFinalConsumer:
		return true
	}
	return false
}

type Role string

const (
	RoleTo Role = "to"
	RoleCc Role = "cc"
)

func (r Role) valid() bool {
	switch r {
	case RoleTo, RoleCc:
		return true
	}
	return false
}

const (
	maxLegalName = 200
	maxAddress   = 300
	maxNotes     = 2000
	maxName      = 120
	maxEmail     = 254
	maxQuery     = 100
	maxOffset    = 10000
	MaxContacts  = 20
	DefaultLimit = 50
	MaxLimit     = 100

	redacted = "[REDACTED]"
)

type ClientFields struct {
	LegalName    string
	CUIT         string
	TaxCondition TaxCondition
	Address      string
	// Notes is empty when the client has none.
	Notes string
}

type Client struct {
	ID string
	ClientFields
	ArchivedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Summary struct {
	ID, LegalName, CUIT string
	TaxCondition        TaxCondition
	ArchivedAt          *time.Time
	ContactsCount       int
}

type ContactFields struct {
	Name string
	// Email is normalized: lower case, no display name.
	Email        string
	Role         Role
	PortalAccess bool
}

type Contact struct {
	ID, ClientID string
	ContactFields
	CreatedAt, UpdatedAt time.Time
}

type Detail struct {
	Client
	Contacts []Contact
}

type Page struct {
	Items []Summary
	Total int
}

func (c Client) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", c.ID), slog.Bool("archived", c.ArchivedAt != nil))
}

func (d Detail) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", d.ID), slog.Int("contacts", len(d.Contacts)))
}

func (c Contact) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", c.ID), slog.String("role", string(c.Role)),
		slog.String("name", redacted), slog.String("email", redacted))
}

func (ClientFields) LogValue() slog.Value { return slog.StringValue(redacted) }

func (c ContactFields) LogValue() slog.Value {
	return slog.GroupValue(slog.String("role", string(c.Role)), slog.String("name", redacted), slog.String("email", redacted))
}

func (ClientInput) LogValue() slog.Value { return slog.StringValue(redacted) }

func (ContactInput) LogValue() slog.Value { return slog.StringValue(redacted) }

type ClientInput struct {
	LegalName    string `json:"legalName"`
	CUIT         string `json:"cuit"`
	TaxCondition string `json:"taxCondition"`
	Address      string `json:"address"`
	Notes        string `json:"notes"`
}

// Validate returns httpx.FieldErrors keyed by JSON name. Messages never include the received value.
func (in ClientInput) Validate() (ClientFields, error) {
	errs := httpx.FieldErrors{}
	f := ClientFields{}
	f.LegalName = text(errs, "legalName", in.LegalName, maxLegalName)
	f.CUIT = cuit.Normalize(in.CUIT)
	if !cuit.Valid(f.CUIT) {
		errs["cuit"] = "invalid cuit"
	}
	f.TaxCondition = TaxCondition(in.TaxCondition)
	if !f.TaxCondition.valid() {
		errs["taxCondition"] = "invalid value"
	}
	f.Address = text(errs, "address", in.Address, maxAddress)
	f.Notes = notes(errs, in.Notes)
	if len(errs) > 0 {
		return ClientFields{}, errs
	}
	return f, nil
}

type ContactInput struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	PortalAccess bool   `json:"portalAccess"`
}

func (in ContactInput) Validate() (ContactFields, error) {
	errs := httpx.FieldErrors{}
	f := ContactFields{PortalAccess: in.PortalAccess}
	f.Name = text(errs, "name", in.Name, maxName)
	email, ok := normalizeEmail(in.Email)
	if !ok {
		errs["email"] = "invalid email"
	}
	f.Email = email
	f.Role = Role(in.Role)
	if !f.Role.valid() {
		errs["role"] = "invalid value"
	}
	if len(errs) > 0 {
		return ContactFields{}, errs
	}
	return f, nil
}

type ListParams struct {
	Query    string
	Archived bool
	// Limit 0 means DefaultLimit.
	Limit  int
	Offset int
}

func (p ListParams) Validate() (ListParams, error) {
	errs := httpx.FieldErrors{}
	p.Query = strings.TrimSpace(p.Query)
	if utf8.RuneCountInString(p.Query) > maxQuery || !utf8.ValidString(p.Query) || strings.ContainsFunc(p.Query, unicode.IsControl) {
		errs["q"] = "invalid value"
	}
	if p.Limit == 0 {
		p.Limit = DefaultLimit
	}
	if p.Limit < 1 || p.Limit > MaxLimit {
		errs["limit"] = "out of range"
	}
	if p.Offset < 0 || p.Offset > maxOffset {
		errs["offset"] = "out of range"
	}
	if len(errs) > 0 {
		return ListParams{}, errs
	}
	return p, nil
}

func text(errs httpx.FieldErrors, key, raw string, limit int) string {
	s := strings.TrimSpace(raw)
	switch n := utf8.RuneCountInString(s); {
	case !utf8.ValidString(s) || strings.ContainsFunc(s, unicode.IsControl):
		errs[key] = "invalid characters"
	case n == 0:
		errs[key] = "required"
	case n > limit:
		errs[key] = "too long"
	}
	return s
}

func notes(errs httpx.FieldErrors, raw string) string {
	s := strings.TrimSpace(raw)
	switch {
	case !utf8.ValidString(s) || strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\t'
	}):
		errs["notes"] = "invalid characters"
	case utf8.RuneCountInString(s) > maxNotes:
		errs["notes"] = "too long"
	}
	return s
}

func normalizeEmail(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > maxEmail {
		return "", false
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Name != "" || addr.Address != s {
		return "", false
	}
	return strings.ToLower(s), true
}

func diffClient(old, next ClientFields) string {
	var names []string
	add := func(changed bool, name string) {
		if changed {
			names = append(names, name)
		}
	}
	add(old.LegalName != next.LegalName, "legalName")
	add(old.CUIT != next.CUIT, "cuit")
	add(old.TaxCondition != next.TaxCondition, "taxCondition")
	add(old.Address != next.Address, "address")
	add(old.Notes != next.Notes, "notes")
	return strings.Join(names, ",")
}

func diffContact(old, next ContactFields) string {
	var names []string
	add := func(changed bool, name string) {
		if changed {
			names = append(names, name)
		}
	}
	add(old.Name != next.Name, "name")
	add(old.Email != next.Email, "email")
	add(old.Role != next.Role, "role")
	add(old.PortalAccess != next.PortalAccess, "portalAccess")
	return strings.Join(names, ",")
}
