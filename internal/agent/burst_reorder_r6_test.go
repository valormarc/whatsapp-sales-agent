package agent

import (
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-6 records: a redelivered burst photo became a
// second batch item when its re-judgement was not
// 'same', and buy-again dropped by-measure lines whose
// stored name is a quantity label.

func TestBurstDedupesByMediaID(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	s := &Session{MerchantID: "demo"}
	draft := throwDraft{Name: "Leather Bag", ListPrice: 500}
	addToBurst(stockOwner, s, draft, "media-dup-1", "", "", "", "")
	addToBurst(stockOwner, s, draft, "media-dup-1", "", "", "", "")
	if len(s.BurstBatch) != 1 {
		t.Fatalf("the same media id must hold exactly one batch item, got %d", len(s.BurstBatch))
	}
	addToBurst(stockOwner, s, draft, "media-dup-2", "", "", "", "")
	if len(s.BurstBatch) != 2 {
		t.Fatalf("a different photo must stand as its own item, got %d", len(s.BurstBatch))
	}
}

func TestBareItemNameStripsTheQuantityLabel(t *testing.T) {
	if got := bareItemName("3 yards of Ankara Fabric", 3, "yard"); got != "Ankara Fabric" {
		t.Fatalf("got %q", got)
	}
	if got := bareItemName("Ankara Fabric", 0, ""); got != "Ankara Fabric" {
		t.Fatalf("a bare name must pass through, got %q", got)
	}
	if got := bareItemName("3 yards of Ankara Fabric", 5, "yard"); got != "3 yards of Ankara Fabric" {
		t.Fatalf("a disagreeing quantity must not strip, got %q", got)
	}
}

const shopWithFabric = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Beaded Bag","description":"A bag.","listPrice":850,"floorPrice":680,"stock":3},{"id":"ankara","name":"Ankara Fabric","description":"Cloth.","listPrice":100,"floorPrice":80,"stock":10,"unit":"yard","baseQty":1,"stockUnits":40}]}}}`

func TestReorderKeepsTheMeasuredLine(t *testing.T) {
	setupChat(t, stockOwner, shopWithFabric)
	buyer := "233550000931"
	orders.Default.RecordOrder("DEMO-4001", orders.Order{
		From: buyer, CreatedAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		ProductName: "3 yards of Ankara Fabric and Beaded Bag", Price: 1150, Total: 1150,
		Status: "paid", MerchantID: "demo",
		Items: []orders.OrderItem{
			{Name: "3 yards of Ankara Fabric", Price: 300, Quantity: 3, Unit: "yard"},
			{Name: "Beaded Bag", Price: 850},
		},
	})
	merchant := catalogue.GetMerchant("demo")
	s := &Session{Stage: "browsing", MerchantID: "demo"}
	sessions.put(sessionKey("demo", buyer), s)
	took := inTurn(buyer, func() bool {
		return startReorder(buyer, s, merchant, "demo", "buy again DEMO-4001")
	})
	if !took {
		t.Fatal("the reorder must take the turn")
	}
	found := false
	if s.Product != nil && s.Product.Name == "Ankara Fabric" {
		found = true
	}
	for _, it := range s.Basket {
		if it.Product != nil && it.Product.Name == "Ankara Fabric" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the measured line must survive the reorder, session product=%+v basket=%+v", s.Product, s.Basket)
	}
}
