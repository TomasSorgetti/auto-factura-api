package cuit

import "testing"

func TestValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"person", "20123456786", true},
		{"woman", "27111111117", true},
		{"company", "30500010912", true},
		{"company 2", "30712345671", true},
		{"check digit 11 maps to 0", "20000000060", true},
		{"wrong check digit", "20123456787", false},
		{"remainder gives 10", "20000000011", false},
		{"10 digits", "2012345678", false},
		{"12 digits", "201234567860", false},
		{"unknown prefix 99", "99123456781", false},
		{"letters", "2012345678a", false},
		{"empty", "", false},
		{"with dashes is not normalized", "20-12345678-6", false},
		{"with dashes after normalize", Normalize("20-12345678-6"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Valid(tt.in); got != tt.want {
				t.Errorf("Valid(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"20-12345678-6":   "20123456786",
		" 20.12345678 6 ": "20123456786",
		"20123456786":     "20123456786",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
