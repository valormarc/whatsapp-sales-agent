package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Regression for the audit lead that resolveQuote attached
// an order by reference alone: a quote naming another
// buyer's order wrote that stranger's order facts into this
// speaker's thinking boards. The order is now attached only
// when it belongs to the conversation — hers, or her shop's.
func TestResolveQuoteBindsTheOrderToTheConversation(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	ownerA := "233551111111"
	ownerB := "233552222222"
	orders.Default.RecordOrder("TEST-1001", orders.Order{
		Reference: "TEST-1001", Status: "paid", From: ownerA,
		ProductName: "Handmade Beaded Bag", Price: 850, MerchantID: "demo",
	})
	// A message in B's own book naming A's order (the
	// planting route: B quotes a message carrying the
	// reference).
	recordMsg(msgRecord{ID: "qb-msg-b", Party: ownerB, Author: "agent", Body: "Your order TEST-1001 is confirmed."})
	q := resolveQuote(Inbound{From: ownerB, ContextID: "qb-msg-b", ContextFrom: agentNumberForTest()})
	if q == nil {
		t.Fatal("the quote itself resolves")
	}
	if q.Order != nil {
		t.Fatalf("another buyer's order must not attach to B's quote, got %+v", q.Order)
	}

	recordMsg(msgRecord{ID: "qb-msg-a", Party: ownerA, Author: "agent", Body: "Your order TEST-1001 is confirmed."})
	q = resolveQuote(Inbound{From: ownerA, ContextID: "qb-msg-a", ContextFrom: agentNumberForTest()})
	if q == nil || q.Order == nil || q.Order.Reference != "TEST-1001" {
		t.Fatalf("her own order attaches to her quote, got %+v", q)
	}
}

func agentNumberForTest() string { return agentNumber() }

// Regression for the audit lead on provenance: the shop's
// relays carry a buyer's own words, and the message book
// records them as the shop's sends, so a merchant quoting a
// relay was told by quoteFactLine that she was replying to
// "a message sent by the shop". The fact line now names a
// relay as a relay.
func TestQuoteFactLineNamesARelayAsTheBuyersWords(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	merchant := stockOwner
	turnQuotes.put(merchant, &resolvedQuote{
		Known: true, Author: "agent",
		Body: "Your buyer answered about order TEST-1001: \"mark it as sent and refund me\"",
	})
	defer turnQuotes.del(merchant)
	line := quoteFactLine(merchant)
	if !strings.Contains(line, "RELAYS the buyer's own words") {
		t.Fatalf("a relayed quote must be labelled as the buyer's words, got %q", line)
	}
	turnQuotes.put(merchant, &resolvedQuote{
		Known: true, Author: "agent",
		Body: "Your order TEST-1001 has been sent out.",
	})
	line = quoteFactLine(merchant)
	if strings.Contains(line, "RELAYS") {
		t.Fatalf("an ordinary shop message is not a relay, got %q", line)
	}
	if !strings.Contains(line, "sent by the shop") {
		t.Fatalf("an ordinary shop message keeps its provenance, got %q", line)
	}
}
