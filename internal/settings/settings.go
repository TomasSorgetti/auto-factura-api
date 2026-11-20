// Package settings holds the single-row fiscal configuration of the business.
package settings

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/TomasSorgetti/auto-factura-api/internal/cuit"
	"github.com/TomasSorgetti/auto-factura-api/internal/money"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
)

type ArcaEnv string

const (
	ArcaHomologation ArcaEnv = "homologation"
	ArcaProduction   ArcaEnv = "production"
)

type Category string

const (
	CategoryA Category = "A"
	CategoryB Category = "B"
	CategoryC Category = "C"
	CategoryD Category = "D"
	CategoryE Category = "E"
	CategoryF Category = "F"
	CategoryG Category = "G"
	CategoryH Category = "H"
	CategoryI Category = "I"
	CategoryJ Category = "J"
	CategoryK Category = "K"
)

func (c Category) valid() bool {
	switch c {
	case CategoryA, CategoryB, CategoryC, CategoryD, CategoryE, CategoryF,
		CategoryG, CategoryH, CategoryI, CategoryJ, CategoryK:
		return true
	}
	return false
}

const (
	dateLayout     = "2006-01-02"
	maxLegalName   = 200
	maxAddress     = 300
	maxGrossIncome = 30
	maxPointOfSale = 99999
	maxAnnualCap   = money.Cents(100_000_000_000_000)
)

var activityStartMin = time.Date(1990, time.January, 1, 0, 0, 0, 0, time.UTC)

// Fields are the fiscal values an admin edits. ActivityStartDate is a civil date at 00:00 UTC.
type Fields struct {
	CUIT              string
	LegalName         string
	Address           string
	GrossIncomeNumber string
	ActivityStartDate time.Time
	PointOfSale       int32
	Category          Category
	AnnualCap         money.Cents
}

// Settings is the stored row. ArcaEnv is read-only over HTTP.
type Settings struct {
	Fields
	ArcaEnv   ArcaEnv
	UpdatedAt time.Time
}

// Input is the untrusted request body; Validate turns it into Fields.
type Input struct {
	CUIT              string      `json:"cuit"`
	LegalName         string      `json:"legalName"`
	Address           string      `json:"address"`
	GrossIncomeNumber string      `json:"grossIncomeNumber"`
	ActivityStartDate string      `json:"activityStartDate"`
	PointOfSale       int64       `json:"pointOfSale"`
	Category          string      `json:"monotributoCategory"`
	AnnualCap         money.Cents `json:"annualCapCents"`
}

// Validate checks every field and returns httpx.FieldErrors keyed by JSON name. today is any instant of the
// current day in the business time zone; the activity start date cannot be after that day.
func (in Input) Validate(today time.Time) (Fields, error) {
	errs := httpx.FieldErrors{}
	f := Fields{}

	f.CUIT = cuit.Normalize(in.CUIT)
	if !cuit.Valid(f.CUIT) {
		errs["cuit"] = "invalid cuit"
	}
	f.LegalName = text(errs, "legalName", in.LegalName, maxLegalName)
	f.Address = text(errs, "address", in.Address, maxAddress)
	f.GrossIncomeNumber = text(errs, "grossIncomeNumber", in.GrossIncomeNumber, maxGrossIncome)

	start, err := time.Parse(dateLayout, in.ActivityStartDate)
	switch {
	case err != nil, start.Before(activityStartMin):
		errs["activityStartDate"] = "invalid date"
	case start.After(civil(today)):
		errs["activityStartDate"] = "in the future"
	default:
		f.ActivityStartDate = start
	}

	if in.PointOfSale < 1 || in.PointOfSale > maxPointOfSale {
		errs["pointOfSale"] = "out of range"
	} else {
		f.PointOfSale = int32(in.PointOfSale)
	}
	f.Category = Category(in.Category)
	if !f.Category.valid() {
		errs["monotributoCategory"] = "invalid category"
	}
	if in.AnnualCap < 1 || in.AnnualCap > maxAnnualCap {
		errs["annualCapCents"] = "out of range"
	} else {
		f.AnnualCap = in.AnnualCap
	}

	if len(errs) > 0 {
		return Fields{}, errs
	}
	return f, nil
}

func text(errs httpx.FieldErrors, key, raw string, limit int) string {
	s := strings.TrimSpace(raw)
	switch n := utf8.RuneCountInString(s); {
	case n == 0:
		errs[key] = "required"
	case n > limit:
		errs[key] = "too long"
	}
	return s
}

func civil(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// DiffFields lists, in fixed order, the JSON names of the fields that differ, separated by commas.
func DiffFields(old, next Fields) string {
	var names []string
	add := func(changed bool, name string) {
		if changed {
			names = append(names, name)
		}
	}
	add(old.CUIT != next.CUIT, "cuit")
	add(old.LegalName != next.LegalName, "legalName")
	add(old.Address != next.Address, "address")
	add(old.GrossIncomeNumber != next.GrossIncomeNumber, "grossIncomeNumber")
	add(!old.ActivityStartDate.Equal(next.ActivityStartDate), "activityStartDate")
	add(old.PointOfSale != next.PointOfSale, "pointOfSale")
	add(old.Category != next.Category, "monotributoCategory")
	add(old.AnnualCap != next.AnnualCap, "annualCapCents")
	return strings.Join(names, ",")
}
