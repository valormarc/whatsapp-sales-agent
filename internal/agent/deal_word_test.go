package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// A handshake word said as the whole reply to a standing
// counter — "deal" — closes the sale at the counter's
// price on the spot. His live journey: the counter stood
// at GH₵65, he said "Deal", and the agent still asked
// "Shall I send the payment link?" before dealing, a
// step with nothing left to protect.
func TestDealWordClosesAStandingCounter(t *testing.T) {
	owner := "233550009301"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550009302"
	c.say(t, buyer, "SHOP-TEST")
	bagv := catalogue.ListProducts(MerchantID)[0]
	bag := &bagv
	sessions.put(sessionKey(MerchantID, buyer), &Session{
		Stage: "haggling", MerchantID: MerchantID, Product: bag,
		Ask: 800, Round: 1, Floor: 680,
	})
	c.say(t, buyer, "deal")
	got := c.joined(t, buyer)
	if !strings.Contains(got, "is yours for GH₵800") {
		t.Fatalf("deal replies = %q, want the sale closed at the standing counter", got)
	}
	s := sessions.get(sessionKey(MerchantID, buyer))
	if s == nil || s.Stage != "fulfilment" || s.AgreedPrice != 800 {
		t.Fatalf("after the handshake the sale must stand in fulfilment at 800, session = %+v", s)
	}
}

// A weak word does not take this road: "ok" after a
// counter is read by the thinking or held, never closed
// by the net — the confirmation insurance stays exactly
// where the ambiguity is real.
func TestWeakWordDoesNotCloseByTheNet(t *testing.T) {
	owner := "233550009303"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550009304"
	c.say(t, buyer, "SHOP-TEST")
	bagv := catalogue.ListProducts(MerchantID)[0]
	bag := &bagv
	sessions.put(sessionKey(MerchantID, buyer), &Session{
		Stage: "haggling", MerchantID: MerchantID, Product: bag,
		Ask: 800, Round: 1, Floor: 680,
	})
	c.say(t, buyer, "ok")
	got := c.joined(t, buyer)
	if strings.Contains(got, "is yours for") {
		t.Fatalf("a bare ok must not close the sale through the net, replies = %q", got)
	}
	s := sessions.get(sessionKey(MerchantID, buyer))
	if s == nil || s.Stage == "fulfilment" {
		t.Fatalf("the haggle must still stand after a bare ok, session = %+v", s)
	}
}

// The net's shape: the handshake whole, and nothing else.
func TestDealWordNetShape(t *testing.T) {
	for _, yes := range []string{"deal", "Deal.", "agreed", "it's a deal", "done deal", "sold", "i'll take it", "I will take it", "take it"} {
		if !dealWordRe.MatchString(strings.ToLower(yes)) {
			t.Fatalf("%q should read as the handshake", yes)
		}
	}
	for _, no := range []string{"ok", "fine", "deal or no deal", "what is the deal?", "a good deal", "deal with it", "i'll take it tomorrow", "yes deal but"} {
		if dealWordRe.MatchString(strings.ToLower(no)) {
			t.Fatalf("%q must not read as the handshake", no)
		}
	}
}
