package agent

import (
	"sync"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-2 regression (medium): stock was checked in one
// loop and taken in another that discarded the results,
// so two buyers completing at once each passed the check
// on the same last unit and both were sold it. The take
// is the check now: exactly one completion wins the unit.
func TestParallelCompletionsCannotOversellTheLastUnit(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	if _, err := catalogue.SetStock("demo", "Handmade Beaded Bag", 1); err != nil {
		t.Fatal(err)
	}
	buyers := []string{"233556666661", "233556666662"}
	var wg sync.WaitGroup
	for _, b := range buyers {
		wg.Add(1)
		go func(buyer string) {
			defer wg.Done()
			// completeSale runs inside the buyer's turn
			// in production, holding her stripe; the
			// completion's own snapshot copies her
			// session under that same stripe. Calling it
			// bare raced the copy (the run-4 proof's race
			// run caught what earlier scheduling hid), so
			// the calls take the turn like production.
			inTurn(buyer, func() bool {
				s := &Session{Stage: "fulfilment", MerchantID: "demo", Product: bag, AgreedPrice: bag.ListPrice, InHandOK: true}
				sessions.put(sessionKey("demo", buyer), s)
				completeSale(buyer, s, bag.ListPrice, fulfilment{Mode: "pickup"})
				return true
			})
		}(b)
	}
	wg.Wait()
	if n := orders.Default.CountFor("demo"); n != 1 {
		t.Fatalf("exactly one completion may mint an order for the last unit, got %d", n)
	}
	if got := catalogue.FindByName("demo", "Handmade Beaded Bag"); got == nil || got.Stock != 0 {
		t.Fatalf("the shelf must stand at zero after the one sale, got %+v", got)
	}
}

// Run-2 regression: the completion journal reconciles a
// sale that took stock and died before its order reached
// the book. The units come back at the next boot, once.
func TestJournalReconciliationReturnsOrphanedStock(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	if _, err := catalogue.SetStock("demo", "Handmade Beaded Bag", 2); err != nil {
		t.Fatal(err)
	}
	// The shape of the crash window: the intent was
	// journalled, one unit was taken (and the take marked
	// in the journal, as the completion does), no order
	// stands.
	recordSaleCompletion(sessionKey("demo", "233556666663"), "sale-orphan-1", "TEST-ORPHAN-1", bag.ID, "demo",
		[]orders.StockLine{{Name: "Handmade Beaded Bag", Units: 1}})
	if !catalogue.TakeStock("demo", "Handmade Beaded Bag", 1) {
		t.Fatal("the take the crash interrupted must succeed in the setup")
	}
	recordJournalTake("TEST-ORPHAN-1", orders.StockLine{Name: "Handmade Beaded Bag", Units: 1})

	reconcileSaleJournal()
	if got := catalogue.FindByName("demo", "Handmade Beaded Bag"); got == nil || got.Stock != 2 {
		t.Fatalf("the orphaned unit must come back at reconciliation, got %+v", got)
	}
	reconcileSaleJournal()
	if got := catalogue.FindByName("demo", "Handmade Beaded Bag"); got == nil || got.Stock != 2 {
		t.Fatalf("reconciliation must give back once only, got %+v", got)
	}
}

// Run-2 regression: an order whose completion stands in
// the book is never a reconciliation candidate — its
// stock stays taken.
func TestJournalReconciliationLeavesStandingOrdersAlone(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	completeSaleFor(t, "demo", "233556666664", bag, 0)
	before := catalogue.FindByName("demo", "Handmade Beaded Bag").Stock
	reconcileSaleJournal()
	if got := catalogue.FindByName("demo", "Handmade Beaded Bag"); got == nil || got.Stock != before {
		t.Fatalf("a standing order's stock must not move, before %d got %+v", before, got)
	}
}
