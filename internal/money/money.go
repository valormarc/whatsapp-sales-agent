// Package money holds the small numeric helpers shared across the
// agent: formatting a cedi amount the way the Node implementation's
// template strings rendered a JavaScript number, and rounding with
// JavaScript Math.round semantics, so messages and maths match the
// reference implementation exactly.
package money

import (
	"math"
	"strconv"
)

// JSNumber renders n the way JavaScript renders a number in a template
// string: whole numbers without a decimal point, fractions with the
// shortest decimal that round-trips.
func JSNumber(n float64) string {
	if n == math.Trunc(n) && math.Abs(n) < 1e15 {
		return strconv.FormatInt(int64(n), 10)
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// GHS renders a cedi amount, for example GH₵850 or GH₵10.95.
func GHS(n float64) string {
	return "GH₵" + JSNumber(n)
}

// Round rounds half toward positive infinity, exactly like
// JavaScript's Math.round, and returns the result as a float64.
func Round(x float64) float64 {
	return math.Floor(x + 0.5)
}

// RoundInt is Round for callers working in whole cedis.
func RoundInt(x float64) int64 {
	return int64(math.Floor(x + 0.5))
}
