package agent

import (
	"math"
	"testing"
)

// Run-5 cluster G: the advert residuals and the figure
// grammar's narrowed escapes.

// A one-line advert gets the advert's doubt rule too:
// two distinct marked figures in the single line mean
// the first may be the delivery fee, and the walk must
// confirm the price rather than bind it undoubted.
func TestSingleLineAdvertDoubtsCompetingMarkedFigures(t *testing.T) {
	text := "Beautiful hand woven kente cloth for sale today, very bright colours and fine weaving, GH₵500 for the whole piece, delivery is GH₵50 within Accra, call or message now"
	d, _, ok := parsePastedAdvert(text)
	if !ok {
		t.Fatal("the one-line write-up should read as an advert")
	}
	if !d.PriceDoubted {
		t.Fatal("competing marked figures in a one-line advert must doubt the price")
	}
}

// And its measure gets the multi-line discipline: a
// conflicting quantity anywhere in the text leaves the
// measure unsettled, so the walk asks instead of
// settling the piece at the first quantity it met.
func TestSingleLineAdvertMeasureYieldsToAConflict(t *testing.T) {
	text := "Lovely Ankara fabric for your dresses and shirts, the full piece is 6 yards long and very soft, we can also cut 3 yards if you prefer, GH₵240 for it, message me today"
	d, _, ok := parsePastedAdvert(text)
	if !ok {
		t.Fatal("the one-line write-up should read as an advert")
	}
	if d.Unit != "" || d.BaseQty != 0 {
		t.Fatalf("conflicting quantities must leave the measure unsettled, got %v %v", d.BaseQty, d.Unit)
	}
}

// The item line's own figures doubt a price harvested
// from another line when they stand in price positions
// — trailing the line, or set off by a comma as their
// own clause — not only when one happens to stand last.
func TestAdvertItemLinePricePositionsDoubt(t *testing.T) {
	d, _, ok := parsePastedAdvert("Leather Bag 500, was 650\nPrice GH₵700")
	if !ok {
		t.Fatal("the advert should read")
	}
	if !d.PriceDoubted {
		t.Fatal("the item line's own price-position figures must doubt a price bound from another line")
	}
}

// Spelled fractions divide by their numerator word:
// two quarters is a half, and thirds stand in the
// grammar beside quarters.
func TestSpelledFractionsDivideByTheirNumerator(t *testing.T) {
	if q, found := parseQuantity("two quarters of a yard"); !found || q.Qty != 0.5 || q.Unit != "yard" {
		t.Fatalf("two quarters of a yard must read as half a yard, got %+v found=%v", q, found)
	}
	if q, found := parseQuantity("a third of a yard"); !found || math.Abs(q.Qty-1.0/3.0) > 1e-9 {
		t.Fatalf("a third of a yard must read as a third, got %+v found=%v", q, found)
	}
	if q, found := parseQuantity("two thirds of a litre"); !found || math.Abs(q.Qty-2.0/3.0) > 1e-9 {
		t.Fatalf("two thirds of a litre must read as two thirds, got %+v found=%v", q, found)
	}
	if q, found := parseQuantity("three quarters of a yard"); !found || q.Qty != 0.75 {
		t.Fatalf("three quarters must still read as 0.75, got %+v found=%v", q, found)
	}
}

// A digit figure in the sentence no longer switches the
// spelled-figure rule off wholesale: a spelled figure in
// a money position is verified as money, while a plain
// count of goods stays ordinary English.
func TestSpelledFigureInAMoneyPositionIsVerified(t *testing.T) {
	if figuresClean("The cloth is sixty, and with the 50 cedis delivery the parcel is ready.", 50) {
		t.Fatal("a spelled price in a money position must be verified even when a digit figure shares its sentence")
	}
	if !figuresClean("I will bring two bags, and the 800 cedis covers them.", 800) {
		t.Fatal("a plain count of goods must stay unverified English, not a price")
	}
}
