package agent

import (
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Regression for the audit finding that a restored session
// completed its sale against a shelf that no longer stood:
// the merchant removed the product while the process was
// down, and the restored buyer bought it anyway. Restored
// sessions are now revalidated against the live catalogue,
// and the mint itself re-resolves every line.
func TestRestoredSaleForRemovedProductIsPutDown(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233554444444"
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	s := &Session{Stage: "fulfilment", MerchantID: "demo", Product: bag, AgreedPrice: 850, FulfilStep: "choice", SaleID: "sale-removed-1"}
	sessions.put(sessionKey("demo", buyer), s)
	bindings.put(buyer, "demo")
	SnapshotSessionsNow()

	if _, err := catalogue.RemoveProduct("demo", "Handmade Beaded Bag"); err != nil {
		t.Fatal(err)
	}
	dropInMemoryState()
	restoreSessions()

	restored := sessions.get(sessionKey("demo", buyer))
	if restored == nil {
		t.Fatal("the buyer's session should survive the restart")
	}
	if restored.Product != nil {
		t.Fatalf("a sale for a removed product must be put down at restore, got %+v", restored.Product)
	}
	if restored.Stage == "fulfilment" {
		t.Fatal("the restored session must not stand in fulfilment for a removed product")
	}
}

// The mint is the second line of defence: a session that
// somehow still names a removed product completes nothing.
func TestCompleteSaleRefusesARemovedProduct(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	if _, err := catalogue.RemoveProduct("demo", "Handmade Beaded Bag"); err != nil {
		t.Fatal(err)
	}
	completeSaleFor(t, "demo", "233554444444", bag, 0)
	if n := orders.Default.CountFor("demo"); n != 0 {
		t.Fatalf("no order may be minted for a removed product, got %d", n)
	}
}

// Regression for the audit finding that a stale snapshot
// replayed a completed sale into a second order: the
// snapshot from just before a completion was restored, the
// buyer answered again, and a twin order was minted. A sale
// now carries an identity from its fulfilment to its order,
// and a completion that finds its own order resumes it.
func TestCompletionReplayResumesTheStandingOrder(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233554444444"
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")

	complete := func() *Session {
		s := &Session{Stage: "fulfilment", MerchantID: "demo", Product: bag, AgreedPrice: 850, InHandOK: true, SaleID: "sale-replay-1"}
		sessions.put(sessionKey("demo", buyer), s)
		return s
	}
	s := complete()
	completeSale(buyer, s, 850, fulfilment{Mode: "pickup"})
	if n := orders.Default.CountFor("demo"); n != 1 {
		t.Fatalf("the first completion must mint one order, got %d", n)
	}
	if got := catalogue.FindByName("demo", "Handmade Beaded Bag"); got == nil || got.Stock != 2 {
		t.Fatalf("the first completion takes one from the shelf, got %+v", got)
	}

	// The stale restore: the same session, answered again.
	s2 := complete()
	completeSale(buyer, s2, 850, fulfilment{Mode: "pickup"})
	if n := orders.Default.CountFor("demo"); n != 1 {
		t.Fatalf("a replayed completion must not mint a twin, got %d orders", n)
	}
	if got := catalogue.FindByName("demo", "Handmade Beaded Bag"); got == nil || got.Stock != 2 {
		t.Fatalf("a replayed completion must not take stock twice, got %+v", got)
	}
}
