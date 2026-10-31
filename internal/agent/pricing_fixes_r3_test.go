package agent

import (
	"strings"
	"testing"
)

// Run-3 record: a fraction quantity was read as its
// denominator — "1/2 yards" became 2 yards.
func TestFractionQuantityReadsAsAFraction(t *testing.T) {
	q, ok := parseQuantity("I need 1/2 yards of it")
	if !ok || q.Qty != 0.5 || q.Unit != "yard" {
		t.Fatalf("1/2 yards must read as half a yard, got %+v (%v)", q, ok)
	}
	q, ok = parseQuantity("3/4 litre")
	if !ok || q.Qty != 0.75 || q.Unit != "litre" {
		t.Fatalf("3/4 litre must read as 0.75, got %+v (%v)", q, ok)
	}
}

// Run-3 record: an advert carrying two distinct marked
// figures (a delivery fee beside the price) bound the
// FIRST as the product's price, undoubted, because marked
// figures were exempt from doubt however many competed.
func TestAdvertWithCompetingMarkedFiguresDoubtsThePrice(t *testing.T) {
	draft, _, ok := parsePastedAdvert("Ankara Cloth\nRich colours, 6 yards\nDelivery GH₵50 within Accra\nPrice GH₵500")
	if !ok {
		t.Fatal("the advert shape must parse")
	}
	if !draft.PriceDoubted {
		t.Fatalf("competing marked figures must doubt the read, got %+v", draft)
	}
}

// Run-3 record: a sole trailing bare figure on a
// description line of a short multi-line advert (a year)
// became the list price with no doubt.
func TestAdvertLoneTrailingYearIsDoubted(t *testing.T) {
	draft, _, ok := parsePastedAdvert("Ankara Cloth\nQuality cotton print\n2019")
	if !ok {
		t.Fatal("the advert shape must parse")
	}
	if draft.ListPrice == 2019 && !draft.PriceDoubted {
		t.Fatalf("a trailing year must never settle as an undoubted price, got %+v", draft)
	}
}

// Run-3 record: the advert's measure came from the first
// quantity phrase anywhere in the text — a "cut 3 yards"
// note settled a 6-yard piece's measure at 3.
func TestAdvertConflictingQuantitiesLeaveTheMeasureUnsettled(t *testing.T) {
	draft, _, ok := parsePastedAdvert("Ankara Cloth, 6 yards\nRich colours\nWe can also cut 3 yards\nGH₵450")
	if !ok {
		t.Fatal("the advert shape must parse")
	}
	if draft.BaseQty != 0 {
		t.Fatalf("conflicting quantities must leave the measure unsettled, got %+v", draft)
	}
}

// The settled advert shape still settles: one marked
// price, one quantity, no doubt, the measure carried.
func TestAdvertSingleQuantityStillSettles(t *testing.T) {
	draft, _, ok := parsePastedAdvert("Ankara Cloth, 6 yards\nRich colours\nGH₵450")
	if !ok {
		t.Fatal("the advert shape must parse")
	}
	if draft.PriceDoubted || draft.ListPrice != 450 || draft.BaseQty != 6 || draft.Unit != "yard" {
		t.Fatalf("a clean advert must settle whole, got %+v", draft)
	}
}

// Run-3 record: the recap's figure pool was unit-blind —
// a size in the pool licensed an invented fee of the same
// value at the confirmation step.
func TestRecapFiguresKeepMoneyAndBareApart(t *testing.T) {
	moneySet := []float64{850}
	bareSet := []float64{42}
	if recapFiguresClean("The Handmade Beaded Bag, size 42, comes to GH₵850.", moneySet, bareSet) != true {
		t.Fatal("an honest recap (bare size, real price) must pass")
	}
	if recapFiguresClean("The Handmade Beaded Bag, size 42, comes to GH₵850, plus a GH₵42 delivery fee.", moneySet, bareSet) {
		t.Fatal("a fee invented at the size's value must be caught")
	}
	if recapFiguresClean("The Handmade Beaded Bag comes to GH₵900.", moneySet, bareSet) {
		t.Fatal("an invented price must be caught")
	}
}

// Run-3 record: the plain buy-intent road ignored a
// quantity remembered earlier in the conversation —
// "Ankara 3 yards" quoted GH₵225, then "I'll take it"
// confirmed at the whole piece's GH₵450. The remembered
// quantity now prices the confirmation, as the measure
// road's own buy-intent already did.
func TestBuyIntentPricesTheRememberedQuantity(t *testing.T) {
	merchant := "233550011002"
	buyer := "233550012002"
	_, c := setupMeasureShop(t, merchant)
	enterMeasureShop(t, c, buyer)

	c.say(t, buyer, "Ankara 3 yards")
	c.replies(t, buyer) // the GH₵225 quote, quantity remembered

	c.say(t, buyer, "I'll take it")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "225") {
		t.Fatalf("the buy-intent confirmation must price the remembered 3 yards (GH₵225), got %q", got)
	}
	if strings.Contains(got, "450") {
		t.Fatalf("the whole piece's price must not appear, got %q", got)
	}
}

// Run-3 record: discovery resolved a name shared by two
// shops to the first in catalogue order.
func TestDiscoveryNameSharedByTwoShopsResolvesToNeither(t *testing.T) {
	setupChat(t, stockOwner, `{"merchants":{
		"efua-a":{"id":"efua-a","name":"Efua's Cloth","shopCode":"EFUA1","ownerWhatsApp":"233550000201","products":[]},
		"efua-b":{"id":"efua-b","name":"Efua's Cloth","shopCode":"EFUA2","ownerWhatsApp":"233550000202","products":[]}
	}}`)
	if shop := resolveDiscoveryShop("Efua's Cloth"); shop != nil {
		t.Fatalf("an ambiguous name must resolve to no shop, got %v", shop.Name)
	}
	if !discoveryNameAmbiguous("Efua's Cloth") {
		t.Fatal("the ambiguity must be reportable to the binding caller")
	}
	if shop := resolveDiscoveryShop("EFUA2"); shop == nil || shop.ID != "efua-b" {
		t.Fatalf("a shop code still resolves exactly, got %+v", shop)
	}
}
