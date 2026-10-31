package agent

import "testing"

// Run-4 records, the figure family: a spelled figure
// whose currency word stood more than two tokens away
// escaped verification entirely, and spelled fractions
// beyond "half" were unread as quantities.
func TestSpelledFigureBindsAcrossItsSentence(t *testing.T) {
	// The audit's payload: the currency word four tokens
	// off. The sentence names a currency and carries no
	// digit figure, so the spelled figure is its money
	// and must verify.
	if figuresClean("That will be fifty bags of rice in cedis", 850) {
		t.Fatal("an invented spelled price with a distant currency word must not pass")
	}
	if !figuresClean("That will be fifty bags of rice in cedis", 850, 50) {
		t.Fatal("the same sentence verifies when the figure is a fact")
	}
	// A digit-bearing money sentence keeps its spelled
	// words as ordinary English when they stand clear of
	// the currency word: the digit is the money. (Words
	// are the window's tokens — digits do not count as
	// distance — so the quantity here stands four words
	// from "cedis".)
	if !figuresClean("Two of the large bags come to 800 cedis in total", 800) {
		t.Fatal("a spelled quantity in a digit-bearing money sentence must not be held as a price")
	}
	// No currency anywhere: ordinary English, unverified
	// as before.
	if !figuresClean("One of the bags is ready when you are", 850) {
		t.Fatal("a spelled figure with no currency in its sentence stays ordinary English")
	}
}

func TestSpelledFractionsReadAsQuantities(t *testing.T) {
	cases := []struct {
		text string
		want float64
	}{
		{"a quarter yard", 0.25},
		{"quarter yard", 0.25},
		{"one quarter of a yard", 0.25},
		{"three quarters of a yard", 0.75},
		{"3 quarters of a yard", 0.75},
		{"half a yard", 0.5},
		{"1/2 yard", 0.5},
		{"3 yards", 3},
	}
	for _, tc := range cases {
		q, ok := parseQuantity(tc.text)
		if !ok || q.Qty != tc.want || q.Unit != "yard" {
			t.Errorf("parseQuantity(%q) = %+v, %v; want %v yard", tc.text, q, ok, tc.want)
		}
	}
}
