package agent

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-5 record: erasure deleted a party's sessions from
// memory, but the durable snapshot is written
// asynchronously — a crash in the window restored her
// sessions whole at boot. The erasure tombstone now
// stands between the stale snapshot and the restore:
// her pre-erasure sessions are dropped, a control
// speaker's session restores, and a session she starts
// AFTER erasing (a later touch) is not the tombstone's
// business.
func TestErasureTombstoneFiltersTheRestore(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	her := "233550000783"
	control := "233550000784"
	old := time.Now().Add(-time.Hour)
	// The seed/snapshot/wipe/restore cycle runs through a
	// closure because of a standing harness hazard: sale
	// completions and erasures elsewhere in the package
	// fire snapshot writes from goroutines, and one that
	// lands inside this test's wipe window overwrites its
	// snapshot with the emptied map (the data directory is
	// the process's current one). When the control session
	// is missing after a restore, the cycle runs once
	// more; the tombstone logic itself is deterministic,
	// so a genuine failure fails both passes.
	seedSnapshotRestore := func() {
		sessions.put(sessionKey("demo", her), &Session{Stage: "browse", MerchantID: "demo", TouchedAt: old})
		sessions.put(sessionKey("demo", control), &Session{Stage: "browse", MerchantID: "demo", TouchedAt: old})
		SnapshotSessionsNow()
		// The process dies and restarts: memory is rebuilt
		// from the files alone.
		sessions.replaceAll(nil)
		erasedMu.Lock()
		erasedLoaded, erasedParties = false, nil
		erasedMu.Unlock()
		markErased(her)
		restoreSessions()
	}
	seedSnapshotRestore()
	if sessions.get(sessionKey("demo", control)) == nil {
		seedSnapshotRestore()
	}
	if s := sessions.get(sessionKey("demo", her)); s != nil {
		t.Fatal("her pre-erasure session must not be restored")
	}
	if s := sessions.get(sessionKey("demo", control)); s == nil {
		t.Fatal("the control session must restore")
	}
	// A session she starts after the erasure carries a
	// later touch and survives the next restart. (The
	// same leaked-snapshot hazard as above: one re-run
	// when the restore comes back empty.)
	postErasureRestore := func() {
		sessions.put(sessionKey("demo", her), &Session{Stage: "browse", MerchantID: "demo", TouchedAt: time.Now()})
		SnapshotSessionsNow()
		sessions.replaceAll(nil)
		restoreSessions()
	}
	postErasureRestore()
	if sessions.get(sessionKey("demo", her)) == nil {
		postErasureRestore()
	}
	if s := sessions.get(sessionKey("demo", her)); s == nil {
		t.Fatal("her post-erasure session must restore — the tombstone covers only what predates it")
	}
	erasedMu.Lock()
	erasedLoaded, erasedParties = false, nil
	erasedMu.Unlock()
}

// Run-2 regression: a restored basket line was refreshed
// by name alone, so a product removed and replaced under
// the same name silently rebound the line to the
// replacement. Lines now bind again only to the very
// product they were priced on — same identity, same list
// and last price — and a price move drops the line.
func TestRestoredBasketLineBindsOnlyToTheSameProductAtTheSamePrices(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")

	s := &Session{Stage: "browsing", MerchantID: "demo",
		Basket: []BasketItem{{Product: bag, Qty: 1}}}
	revalidateRestoredSession(s)
	if len(s.Basket) != 1 {
		t.Fatal("an unchanged shelf must keep the basket line")
	}

	if _, err := catalogue.UpdateFloorPrice("demo", "Handmade Beaded Bag", 600); err != nil {
		t.Fatal(err)
	}
	s2 := &Session{Stage: "browsing", MerchantID: "demo",
		Basket: []BasketItem{{Product: bag, Qty: 1}}}
	revalidateRestoredSession(s2)
	if len(s2.Basket) != 0 {
		t.Fatal("a line whose last price moved while the process was down must be dropped, not rebound")
	}
}

// Run-2 regression: a product removed and replaced under
// the same name is not the product the buyer agreed on,
// even at the same prices. The pending sale is put down.
func TestRestoredSaleForAReplacedProductIsPutDown(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	s := &Session{Stage: "fulfilment", MerchantID: "demo", Product: bag, AgreedPrice: 850, FulfilStep: "choice", SaleID: "sale-replaced-1"}

	if _, err := catalogue.RemoveProduct("demo", "Handmade Beaded Bag"); err != nil {
		t.Fatal(err)
	}
	replacement := *bag
	replacement.ID = "beaded-bag-2"
	if _, err := catalogue.AddProduct("demo", replacement); err != nil {
		t.Fatal(err)
	}
	revalidateRestoredSession(s)
	if s.Product != nil {
		t.Fatal("a sale naming a replaced product must be put down at restore")
	}
}

// Run-2 regression: dropRestoredSale left the total hand
// standing and never reset the lastprice stage, so a
// dropped total-haggle session completed on the buyer's
// next yes at the sum of the stale hand's floors.
func TestDropRestoredSaleClearsTheTotalHand(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	s := &Session{Stage: "lastprice", MerchantID: "demo", Product: bag,
		TotalHand: []catalogue.Product{*bag}, SaleID: "sale-hand-1"}
	dropRestoredSale(s)
	if len(s.TotalHand) != 0 {
		t.Fatal("the total hand must go with the dropped sale")
	}
	if s.Stage != "browsing" {
		t.Fatalf("a dropped lastprice session must return to browsing, got %q", s.Stage)
	}
	if s.SaleID != "" {
		t.Fatal("the dropped sale's identity must go with it")
	}
}

// Run-2 regression: the completion journal rebinds a
// restored session whose snapshot lost its sale identity,
// so the replay guard can find the standing order.
func TestJournalRebindsALostSaleID(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233554444446"
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	key := sessionKey("demo", buyer)

	recordSaleCompletion(key, "sale-journal-1", "TEST-9001", bag.ID, "demo", nil)
	s := &Session{Stage: "fulfilment", MerchantID: "demo", Product: bag, AgreedPrice: 850}
	rebindSaleIDFromJournal(key, s)
	if s.SaleID != "sale-journal-1" {
		t.Fatalf("the journal must rebind the lost identity, got %q", s.SaleID)
	}

	other := &Session{Stage: "fulfilment", MerchantID: "demo", Product: bag, AgreedPrice: 850}
	rebindSaleIDFromJournal(sessionKey("demo", "233550009999"), other)
	if other.SaleID != "" {
		t.Fatal("another buyer's session must never take this completion's identity")
	}
}

// Run-2 regression: resuming a sale whose order has ended
// must not mint a fresh payment link on it. The buyer is
// told the order has ended; the ended order's session
// reference is untouched.
func TestResumeOfAnEndedOrderMintsNoLink(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233554444447"
	orders.Default.RecordOrder("TEST-END-1", orders.Order{
		Reference: "TEST-END-1", Status: "cancelled", From: buyer,
		ProductName: "Handmade Beaded Bag", Price: 850, MerchantID: "demo",
		SaleID: "sale-ended-1",
	})
	merchant := catalogue.GetMerchant("demo")
	s := &Session{Stage: "fulfilment", MerchantID: "demo", SaleID: "sale-ended-1"}
	resumeAgreementOrder(buyer, s, merchant, orders.Default.GetOrder("TEST-END-1"))

	// resumeAgreementOrder schedules an asynchronous
	// snapshot; drain it into this test's own data
	// directory before the test ends, so it cannot land
	// in a later test's restart and overwrite its
	// snapshot with this test's emptied state.
	for i := 0; i < 200; i++ {
		if snap, err := os.ReadFile(datadir.Path("sessions.json")); err == nil && strings.Contains(string(snap), buyer) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	raw, _ := os.ReadFile(datadir.Path("outbox.jsonl"))
	if !strings.Contains(string(raw), "has ended") {
		t.Fatalf("the buyer must be told the order has ended, got:\n%s", raw)
	}
	if strings.Contains(string(raw), "payment button again") {
		t.Fatal("no payment button may be sent for an ended order")
	}
	if got := orders.Default.GetOrder("TEST-END-1"); got.ProviderRef != nil {
		t.Fatal("the ended order's session reference must be untouched")
	}
}
