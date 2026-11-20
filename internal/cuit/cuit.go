// Package cuit normalizes and validates Argentine CUIT/CUIL numbers.
package cuit

import "strings"

var (
	weights  = [10]int{5, 4, 3, 2, 7, 6, 5, 4, 3, 2}
	prefixes = map[string]struct{}{"20": {}, "23": {}, "24": {}, "25": {}, "26": {}, "27": {}, "30": {}, "33": {}, "34": {}}
)

// Normalize strips spaces, dots and dashes: "20-12345678-6" becomes "20123456786".
func Normalize(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '.', '-':
			return -1
		}
		return r
	}, s)
}

// Valid reports whether s is 11 digits with a known prefix and a correct check digit.
// It does not normalize: call Normalize first.
func Valid(s string) bool {
	if len(s) != 11 {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	if _, ok := prefixes[s[:2]]; !ok {
		return false
	}
	sum := 0
	for i, w := range weights {
		sum += int(s[i]-'0') * w
	}
	check := 11 - sum%11
	switch check {
	case 11:
		check = 0
	case 10:
		return false
	}
	return int(s[10]-'0') == check
}
