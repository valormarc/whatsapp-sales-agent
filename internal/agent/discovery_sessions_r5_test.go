package agent

import (
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-5 cluster I: the discovery lane resolves a
// delivery report from the buyer's own words before any
// shop is pinned, an entry-selected view evicts like
// every other view, the sale journal's rebind asks for
// a post-agreement stage, and a restored removal
// question is revalidated by identity.

// The hint names one shop; her words name the other
// shop's goods. The named order — not the hint — pins
// the shop, and it is the named order that is marked.
func TestDiscoveryReportBindsTheNamedOrdersShop(t *testing.T) {
	setupChat(t, "233265993567", discoveryCatalogue)
	buyer := "233550006301"
	orders.Default.RecordOrder("EFUA-1001", orders.Order{
		Reference: "EFUA-1001", Status: "paid", From: buyer, MerchantID: "efua",
		ProductName: "Handmade Beaded Bag", Price: 850, DeliveryState: "sent",
	})
	orders.Default.RecordOrder("LUXE-1001", orders.Order{
		Reference: "LUXE-1001", Status: "paid", From: buyer, MerchantID: "luxe",
		ProductName: "HP EliteBook 1030 G8 x360", Price: 6800, DeliveryState: "sent",
	})
	u := &discoveryUnderstanding{Intent: "order_received", Shop: "EFUA"}
	if !actOnDiscovery(buyer, "my hp elitebook 1030 g8 x360 has arrived, thank you", u) {
		t.Fatal("the report should be served")
	}
	if o := orders.Default.GetOrder("LUXE-1001"); o == nil || o.DeliveryState != "received" {
		t.Fatalf("the named order must be the one marked received, got %+v", o)
	}
	if o := orders.Default.GetOrder("EFUA-1001"); o == nil || o.DeliveryState != "sent" {
		t.Fatalf("the hinted shop's order must be untouched, got %+v", o)
	}
	if got := bindings.get(buyer); got != "luxe" {
		t.Fatalf("the named order's shop must be the one bound, got %q", got)
	}
}

// An entry-selected product rides in LastViewed only,
// so the session it makes is an ordinary resting view
// and the idle eviction reaches it.
func TestEntryShapedBrowsingSessionEvictsWhenIdle(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	dropInMemoryState()
	key := sessionKey("demo", "233559999993")
	p := catalogue.FindProduct("demo", "beaded bag")
	sessions.put(key, &Session{Stage: "browsing", MerchantID: "demo", LastViewed: p, TouchedAt: time.Now().Add(-31 * 24 * time.Hour)})
	out := snapshotSessionsCopy(time.Now())
	if _, kept := out[key]; kept {
		t.Fatal("an entry-shaped browsing session idle past the limit must be evicted")
	}
}

// The journal rebind exists for a completed sale's own
// pre-completion snapshot: a session still haggling
// over the same product is a new purchase and never
// inherits the earlier sale's identity.
func TestRebindRequiresAPostAgreementStage(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233550006302"
	key := sessionKey("demo", buyer)
	bag := catalogue.FindProduct("demo", "beaded bag")
	recordSaleCompletion(key, "SALE-TEST-1", "TEST-1001", bag.ID, "demo", nil)

	haggling := &Session{Stage: "haggling", MerchantID: "demo", Product: bag}
	rebindSaleIDFromJournal(key, haggling)
	if haggling.SaleID != "" {
		t.Fatal("a pre-agreement session must not inherit a prior sale's identity")
	}
	agreed := &Session{Stage: "fulfilment", MerchantID: "demo", Product: bag}
	rebindSaleIDFromJournal(key, agreed)
	if agreed.SaleID != "SALE-TEST-1" {
		t.Fatal("a post-agreement snapshot must still recover its sale identity")
	}
}

// A restored removal question stands only while the
// name's current product is the very product it was
// armed on: a legacy snapshot with no identity
// recorded, and a replacement under the same name, both
// put the question down.
func TestRestoredPendingRemoveIsRevalidatedByIdentity(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")

	legacy := &Session{MerchantID: "demo", PendingRemove: "Handmade Beaded Bag"}
	revalidateRestoredSession(legacy)
	if legacy.PendingRemove != "" {
		t.Fatal("a restored removal with no recorded identity must be put down")
	}

	replaced := &Session{MerchantID: "demo", PendingRemove: "Handmade Beaded Bag", PendingRemoveID: "some-other-product"}
	revalidateRestoredSession(replaced)
	if replaced.PendingRemove != "" {
		t.Fatal("a restored removal whose product was replaced must be put down")
	}

	same := &Session{MerchantID: "demo", PendingRemove: "Handmade Beaded Bag", PendingRemoveID: bag.ID}
	revalidateRestoredSession(same)
	if same.PendingRemove == "" {
		t.Fatal("a restored removal for the same product must stand")
	}
}

// An unjudged burst photo does not fold on caption fact
// equality: with no photo to judge, there is no verdict,
// and no fold.
func TestUnjudgedBurstPhotoDoesNotFold(t *testing.T) {
	item := burstItem{Name: "Metal Rack", Description: "Wall mounted steel rack", ListPrice: 150}
	draft := throwDraft{Name: "Metal Rack", Description: "Wall mounted steel rack", ListPrice: 150}
	if burstFoldOK("", "Metal Rack\nWall mounted steel rack\nGH₵150", item, draft, "") {
		t.Fatal("an unjudged photo must not fold on caption fact equality alone")
	}
}
