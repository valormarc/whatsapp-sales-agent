package agent

import "testing"

// Run-4 records, the advert family: competition and
// measure were still judged globally where the run-3
// fixes had only narrowed them — a marked figure on a
// delivery line beat the item line's own bare price
// undoubted, a cut note on a description line settled
// the piece's measure, and a lone bare figure on the
// name line (a model number) bound as the price.
func TestAdvertLineCompetitionDoubtsThePrice(t *testing.T) {
	draft, _, ok := parsePastedAdvert("Leather Bag 500\nQuality leather, handmade\nDelivery GH₵50 within Accra")
	if !ok {
		t.Fatal("the advert must parse")
	}
	if !draft.PriceDoubted {
		t.Fatalf("the item line's own figure (500) competing with the harvested delivery figure (50) must doubt the read, got %+v", draft)
	}
}

func TestAdvertMeasureDoesNotSettleFromADescriptionLine(t *testing.T) {
	draft, _, ok := parsePastedAdvert("Ankara Cloth\nRich colours\nWe can also cut 3 yards\nGH₵450")
	if !ok {
		t.Fatal("the advert must parse")
	}
	if draft.BaseQty != 0 || draft.Unit != "" {
		t.Fatalf("a cut note on a description line must not settle the measure, got unit %q base %v", draft.Unit, draft.BaseQty)
	}
	// The same quantity on the item's own line settles
	// as before.
	draft2, _, ok := parsePastedAdvert("Ankara Cloth 6 yards\nRich colours\nGH₵450")
	if !ok {
		t.Fatal("the advert must parse")
	}
	if draft2.BaseQty != 6 || draft2.Unit != "yard" {
		t.Fatalf("a quantity on the name line must still settle the measure, got unit %q base %v", draft2.Unit, draft2.BaseQty)
	}
}

func TestAdvertLoneNameLineFigureIsDoubted(t *testing.T) {
	draft, _, ok := parsePastedAdvert("HP EliteBook 840\nAvailable, like new")
	if !ok {
		t.Fatal("the advert must parse")
	}
	if draft.ListPrice == 840 && !draft.PriceDoubted {
		t.Fatalf("a lone bare figure on the name line must not bind undoubted, got %+v", draft)
	}
}
