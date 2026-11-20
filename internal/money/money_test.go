package money

import (
	"errors"
	"math"
	"testing"
)

func TestAdd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		a, b    Cents
		want    Cents
		wantErr error
	}{
		{"small", 1, 2, 3, nil},
		{"max plus one", math.MaxInt64, 1, 0, ErrOverflow},
		{"min minus one", math.MinInt64, -1, 0, ErrOverflow},
		{"max plus min", math.MaxInt64, math.MinInt64, -1, nil},
		{"negatives", -5, -7, -12, nil},
		{"mixed", 10, -25, -15, nil},
		{"max plus zero", math.MaxInt64, 0, math.MaxInt64, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Add(tt.a, tt.b)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSum(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      []Cents
		want    Cents
		wantErr error
	}{
		{"empty", nil, 0, nil},
		{"three", []Cents{100, 250, -50}, 300, nil},
		{"overflow mid list", []Cents{math.MaxInt64, 1, -5}, 0, ErrOverflow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Sum(tt.in...)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestARCA(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   Cents
		want string
	}{
		{0, "0.00"},
		{5, "0.05"},
		{50, "0.50"},
		{123456, "1234.56"},
		{-50, "-0.50"},
		{math.MaxInt64, "92233720368547758.07"},
		{math.MinInt64, "-92233720368547758.08"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.in.ARCA(); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   Cents
		want string
	}{
		{0, "$ 0,00"},
		{1, "$ 0,01"},
		{100000, "$ 1.000,00"},
		{123456789, "$ 1.234.567,89"},
		{-1, "-$ 0,01"},
		{math.MinInt64, "-$ 92.233.720.368.547.758,08"},
		{math.MaxInt64, "$ 92.233.720.368.547.758,07"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.in.String(); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
