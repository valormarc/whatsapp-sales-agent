// Package money holds the small numeric helpers shared across the
// agent: formatting a cedi amount the way the Node implementation's
// template strings rendered a JavaScript number, and rounding with
// JavaScript Math.round semantics, so messages and maths match the
// reference implementation exactly.
package money

import (
	"math"
	"strconv"
	"strings"
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

// GHS renders a cedi amount, for example GH₵850, GH₵6,800
// or GH₵10.95: the whole part is grouped in thousands with
// commas, the way sellers themselves write their figures.
func GHS(n float64) string {
	s := JSNumber(n)
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	sign := ""
	if strings.HasPrefix(intPart, "-") {
		sign, intPart = "-", intPart[1:]
	}
	if len(intPart) >= 4 {
		var groups []string
		for len(intPart) > 3 {
			groups = append([]string{intPart[len(intPart)-3:]}, groups...)
			intPart = intPart[:len(intPart)-3]
		}
		groups = append([]string{intPart}, groups...)
		intPart = strings.Join(groups, ",")
	}
	return "GH₵" + sign + intPart + frac
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
