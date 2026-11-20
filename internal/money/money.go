// Package money represents amounts of Argentine pesos as integer cents.
package money

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// Cents is an amount in hundredths of a peso. It is never a float.
type Cents int64

// ErrOverflow is returned when a sum does not fit in int64 cents.
var ErrOverflow = errors.New("amount overflows int64 cents")

// Add returns a+b or ErrOverflow.
func Add(a, b Cents) (Cents, error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, ErrOverflow
	}
	return a + b, nil
}

// Sum adds every amount in order; an empty list is 0.
func Sum(amounts ...Cents) (Cents, error) {
	var total Cents
	for _, a := range amounts {
		var err error
		if total, err = Add(total, a); err != nil {
			return 0, err
		}
	}
	return total, nil
}

// ARCA formats with a dot and two decimals and no thousands separator, e.g. "1234.56", "-0.50".
func (c Cents) ARCA() string {
	neg, whole, frac := split(c)
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	b.WriteString(strconv.FormatUint(whole, 10))
	b.WriteByte('.')
	b.WriteString(twoDigits(frac))
	return b.String()
}

// String formats for people in es-AR: "$ 1.234,56" and "-$ 1.234,56" (U+00A0 after "$").
func (c Cents) String() string {
	neg, whole, frac := split(c)
	digits := strconv.FormatUint(whole, 10)
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	b.WriteString("$ ")
	lead := len(digits) % 3
	if lead == 0 {
		lead = 3
	}
	b.WriteString(digits[:lead])
	for i := lead; i < len(digits); i += 3 {
		b.WriteByte('.')
		b.WriteString(digits[i : i+3])
	}
	b.WriteByte(',')
	b.WriteString(twoDigits(frac))
	return b.String()
}

func split(c Cents) (neg bool, whole, frac uint64) {
	var abs uint64
	if c < 0 {
		neg = true
		abs = uint64(-(c + 1)) + 1
	} else {
		abs = uint64(c)
	}
	return neg, abs / 100, abs % 100
}

func twoDigits(n uint64) string {
	return string([]byte{byte('0' + n/10), byte('0' + n%10)})
}
