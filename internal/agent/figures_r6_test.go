package agent

import (
	"testing"
)

// Run-6 figure-grammar records: the caption grammar
// truncated figures the platform's own tokeniser reads
// in full, a spelled fraction's numerator outside the
// first tables misread, a caption's two marked figures
// bound undoubted, an advert's leading price escaped the
// competition test, a sole cut quantity settled a
// one-line advert's measure, a spelled money figure
// escaped verification through a function word, and the
// burst shape scrub disagreed with the tokeniser.

func TestFigureGrammarReadsWhatTheTokeniserReads(t *testing.T) {
	if vals := markedFigureValues("Shoes @12.345k"); len(vals) != 1 || vals[0] != 12345 {
		t.Fatalf("@12.345k must read 12345, got %v", vals)
	}
	if vals := markedFigureValues("Bag 1,200k cedis"); len(vals) != 1 || vals[0] != 1200000 {
		t.Fatalf("1,200k must read 1200000, got %v", vals)
	}
}

func TestSpelledFractionNumeratorsBeyondTheFirstTables(t *testing.T) {
	if q, ok := parseQuantity("twelve quarters of a yard"); !ok || q.Qty != 3 {
		t.Fatalf("twelve quarters must read 3, got %+v ok=%v", q, ok)
	}
	if q, ok := parseQuantity("three thirds of a yard"); !ok || q.Qty != 1 {
		t.Fatalf("three thirds must read 1, got %+v ok=%v", q, ok)
	}
	if q, ok := parseQuantity("two quarters of a yard"); !ok || q.Qty != 0.5 {
		t.Fatalf("two quarters must still read 0.5, got %+v ok=%v", q, ok)
	}
}

func TestCaptionWithTwoMarkedFiguresDoubts(t *testing.T) {
	d := throwDraftFromCaption("Leather Bag GH₵700 delivery GH₵50")
	if !d.PriceDoubted {
		t.Fatal("a caption with two distinct marked figures must doubt its price")
	}
	d = throwDraftFromCaption("Leather Bag GH₵700")
	if d.PriceDoubted || d.ListPrice != 700 {
		t.Fatalf("a single marked figure must still bind clean, got %+v", d)
	}
}

func TestAdvertLeadingAndParenthesisedPricesDoubt(t *testing.T) {
	for _, text := range []string{
		"500 Leather Bag\nDelivery GH₵50",
		"Leather Bag (500) new\nDelivery GH₵50",
	} {
		d, _, ok := parsePastedAdvert(text)
		if !ok {
			t.Fatalf("%q must parse as an advert", text)
		}
		if !d.PriceDoubted {
			t.Fatalf("%q: the item line's own price must doubt the delivery line's marked figure", text)
		}
	}
	// The flagship woven model number stays name material.
	d, _, ok := parsePastedAdvert("HP EliteBook 1030 G8 x360\nPrice Ghc 1,500")
	if !ok || d.PriceDoubted {
		t.Fatalf("the flagship advert must still bind undoubted, got ok=%v doubted=%v", ok, d.PriceDoubted)
	}
}

func TestOneLineAdvertCutQuantityDoesNotSettleMeasure(t *testing.T) {
	text := "Quality Ankara Fabric for all your lovely dresses and shirts, cut 50 yards, GH₵500"
	d, _, ok := parsePastedAdvert(text)
	if !ok {
		t.Fatal("the one-line advert must parse")
	}
	if d.Unit != "" || d.BaseQty != 0 {
		t.Fatalf("a sole cut quantity must not settle the measure, got unit=%q base=%v", d.Unit, d.BaseQty)
	}
}

func TestSpelledMoneyFigureSurvivesTheFunctionWord(t *testing.T) {
	// "five in total" is a money figure in a sentence
	// that also carries a digit figure.
	found := false
	for _, wf := range wordFigures("You owe 200 cedis for the delivery last week and five in total") {
		if wf.value == 5 && wf.currencyAdjacent {
			found = true
		}
	}
	if !found {
		t.Fatal("'five in total' must read as a money figure")
	}
	// A genuine goods count in the same skeleton stays a
	// count.
	for _, wf := range wordFigures("You owe 200 cedis for the delivery last week and two bags in the box") {
		if wf.value == 2 && wf.currencyAdjacent {
			t.Fatal("'two bags' is a count of goods, not money")
		}
	}
}

func TestBurstScrubAgreesWithTheTokeniser(t *testing.T) {
	// "900k5" reads as two figures (900, then 5) to the
	// tokeniser; scrubbed the same way, its residue holds
	// no figure-shaped word the shape test could price
	// differently. A single "900k" scrubs to nothing.
	if burstAnswerShape("900k5") {
		t.Fatal("900k5 is two figures, not one price answer")
	}
	if !burstAnswerShape("900k") {
		t.Fatal("a lone 900k is a price answer")
	}
}
