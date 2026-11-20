package settings

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
)

func validInput() Input {
	return Input{
		CUIT: "20-12345678-6", LegalName: "  Juan Perez ", Address: "Av. Siempreviva 742", GrossIncomeNumber: "Exento",
		ActivityStartDate: "2020-03-01", PointOfSale: 3, Category: "D", AnnualCap: 2_500_000_000,
	}
}

var today = time.Date(2026, time.November, 20, 15, 30, 0, 0, time.UTC)

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Input)
		field  string
		msg    string
	}{
		{"cuit bad", func(i *Input) { i.CUIT = "20123456787" }, "cuit", "invalid cuit"},
		{"legal name empty", func(i *Input) { i.LegalName = "   " }, "legalName", "required"},
		{"legal name long", func(i *Input) { i.LegalName = strings.Repeat("a", 201) }, "legalName", "too long"},
		{"address empty", func(i *Input) { i.Address = "" }, "address", "required"},
		{"address long", func(i *Input) { i.Address = strings.Repeat("a", 301) }, "address", "too long"},
		{"gross income empty", func(i *Input) { i.GrossIncomeNumber = "" }, "grossIncomeNumber", "required"},
		{"gross income long", func(i *Input) { i.GrossIncomeNumber = strings.Repeat("a", 31) }, "grossIncomeNumber", "too long"},
		{"date malformed", func(i *Input) { i.ActivityStartDate = "01/03/2020" }, "activityStartDate", "invalid date"},
		{"date too old", func(i *Input) { i.ActivityStartDate = "1989-12-31" }, "activityStartDate", "invalid date"},
		{"date tomorrow", func(i *Input) { i.ActivityStartDate = "2026-11-21" }, "activityStartDate", "in the future"},
		{"pos zero", func(i *Input) { i.PointOfSale = 0 }, "pointOfSale", "out of range"},
		{"pos too big", func(i *Input) { i.PointOfSale = 100000 }, "pointOfSale", "out of range"},
		{"category", func(i *Input) { i.Category = "L" }, "monotributoCategory", "invalid category"},
		{"cap zero", func(i *Input) { i.AnnualCap = 0 }, "annualCapCents", "out of range"},
		{"cap too big", func(i *Input) { i.AnnualCap = 100_000_000_000_001 }, "annualCapCents", "out of range"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := validInput()
			tt.mutate(&in)
			_, err := in.Validate(today)
			var fe httpx.FieldErrors
			if !errors.As(err, &fe) || fe[tt.field] != tt.msg || len(fe) != 1 {
				t.Fatalf("got %v, want only %s=%q", err, tt.field, tt.msg)
			}
		})
	}

	t.Run("valid, normalized and trimmed", func(t *testing.T) {
		t.Parallel()
		f, err := validInput().Validate(today)
		if err != nil {
			t.Fatal(err)
		}
		if f.CUIT != "20123456786" || f.LegalName != "Juan Perez" || f.PointOfSale != 3 || f.Category != CategoryD {
			t.Fatalf("unexpected fields %+v", f)
		}
	})
	t.Run("start date today is ok", func(t *testing.T) {
		t.Parallel()
		in := validInput()
		in.ActivityStartDate = "2026-11-20"
		if _, err := in.Validate(today); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("everything invalid", func(t *testing.T) {
		t.Parallel()
		_, err := Input{}.Validate(today)
		var fe httpx.FieldErrors
		if !errors.As(err, &fe) || len(fe) != 8 {
			t.Fatalf("got %v", err)
		}
	})
}

func TestDiffFields(t *testing.T) {
	t.Parallel()

	base, err := validInput().Validate(today)
	if err != nil {
		t.Fatal(err)
	}
	if got := DiffFields(base, base); got != "" {
		t.Errorf("no changes: got %q", got)
	}
	next := base
	next.PointOfSale = 4
	next.CUIT = "30500010912"
	if got := DiffFields(base, next); got != "cuit,pointOfSale" {
		t.Errorf("got %q", got)
	}
	all := "cuit,legalName,address,grossIncomeNumber,activityStartDate,pointOfSale,monotributoCategory,annualCapCents"
	if got := DiffFields(Fields{}, base); got != all {
		t.Errorf("all fields: got %q", got)
	}
}
