package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// Clearing the basket in its canonical words empties
// everything that stands — settled items and picks
// alike — and answers with the cleared line, not the
// list. Before this road existed the command was
// answered with the basket view itself and nothing
// was dropped.
func TestClearBasketCommandEmptiesWhatStands(t *testing.T) {
	owner := "233550009201"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550009202"
	c.say(t, buyer, "SHOP-TEST")
	bagv := catalogue.ListProducts(MerchantID)[0]
	bag := &bagv
	sessions.put(sessionKey(MerchantID, buyer), &Session{
		Stage:       "browsing",
		MerchantID:  MerchantID,
		Basket:      []BasketItem{{Product: bag, AgreedPrice: 730}},
		ShelfPicks:  []string{bag.ID},
		ShelfSettling: true,
		Product:     bag,
	})
	c.say(t, buyer, "clear basket")
	got := c.last(t, buyer)
	if !strings.Contains(got, "Cleared. Your basket is empty now.") {
		t.Fatalf("clear basket reply = %q, want the cleared line", got)
	}
	s := sessions.get(sessionKey(MerchantID, buyer))
	if s == nil || len(s.Basket) != 0 || len(s.ShelfPicks) != 0 || s.Product != nil || s.ShelfSettling {
		t.Fatalf("the basket must stand empty after the command, session = %+v", s)
	}
}

// With nothing standing, the command is answered with
// the same plain truth the view gives: the basket is
// empty. Nothing is invented and nothing breaks.
func TestClearBasketWhenEmptySaysSo(t *testing.T) {
	owner := "233550009203"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550009204"
	c.say(t, buyer, "SHOP-TEST")
	c.say(t, buyer, "clear my basket")
	got := c.last(t, buyer)
	if !strings.Contains(got, "Your basket is empty at the moment.") {
		t.Fatalf("clearing an empty basket = %q, want the empty line", got)
	}
}

// The model-read act empties the basket the same way,
// for the phrasings the net does not own.
func TestBasketClearActEmpties(t *testing.T) {
	owner := "233550009205"
	setupChat(t, owner, shopWithBag)
	buyer := "233550009206"
	bagv := catalogue.ListProducts(MerchantID)[0]
	bag := &bagv
	s := &Session{Stage: "browsing", MerchantID: MerchantID, Basket: []BasketItem{{Product: bag, AgreedPrice: 730}}}
	sessions.put(sessionKey(MerchantID, buyer), s)
	u := &understood{Intent: intentBasketClear, Words: "remove everything I picked"}
	if !actOnBuyerUnderstanding(buyer, s, catalogue.GetMerchant(MerchantID), MerchantID, u) {
		t.Fatal("the basket_clear act must be carried out")
	}
	got := sessions.get(sessionKey(MerchantID, buyer))
	if got == nil || len(got.Basket) != 0 {
		t.Fatalf("the act must empty the basket, session = %+v", got)
	}
}

// The net's shape: the command in either word order,
// and never a question about the basket.
func TestBasketClearNetShape(t *testing.T) {
	for _, yes := range []string{"clear basket", "clear my basket", "empty the basket", "please clear the basket now", "basket clear"} {
		if !basketClearAsked(yes) {
			t.Fatalf("%q should read as the clearing command", yes)
		}
	}
	for _, no := range []string{"did we clear the basket?", "show me my basket", "what is in my basket?", "add to my basket", "check basket"} {
		if basketClearAsked(no) {
			t.Fatalf("%q must not read as the clearing command", no)
		}
	}
}
