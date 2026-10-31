package agent

import "testing"

// Run-2 regressions for the figure gates.

func TestSpelledFigureInGhcIsVerified(t *testing.T) {
	if figuresClean("The delivery is fifty ghc", 0) {
		// allowed 0 only: fifty must be caught as money.
		t.Fatal("a spelled figure priced in ghc must be verified, not skipped")
	}
	if figuresClean("The delivery is fifty ghc") {
		t.Fatal("fifty ghc with no allowances must fail")
	}
	if !figuresClean("The delivery is fifty ghc", 50) {
		t.Fatal("fifty ghc at an allowed 50 must pass")
	}
}

func TestSpelledFigureCurrencyTwoTokensAwayIsVerified(t *testing.T) {
	if figuresClean("Delivery is fifty in ghc, pay on arrival") {
		t.Fatal("a currency word two tokens away must still mark the figure as money")
	}
	if !figuresClean("Delivery is fifty in ghc, pay on arrival", 50) {
		t.Fatal("the allowed figure must pass")
	}
}

func TestTwoFiftyIsNotFiftyTwo(t *testing.T) {
	if figuresClean("It is two fifty cedis", 52) {
		t.Fatal("'two fifty' must never verify as 52")
	}
	if figuresClean("It is two fifty cedis", 2.50) {
		t.Fatal("'two fifty cedis' carries a spelled 50 that 2.50 does not allow")
	}
	if !figuresClean("It is fifty two cedis", 52) {
		t.Fatal("canonical order must still parse as 52")
	}
}

func TestNumberPhraseShapesStillParse(t *testing.T) {
	cases := map[string]float64{
		"one hundred and fifty": 150,
		"fifty two thousand":     52000,
		"two thousand twenty":    2020,
		"six hundred":            600,
	}
	for text, want := range cases {
		wfs := wordFigures(text)
		if len(wfs) != 1 || wfs[0].value != want {
			t.Fatalf("%q must parse as one figure %v, got %+v", text, want, wfs)
		}
	}
}
