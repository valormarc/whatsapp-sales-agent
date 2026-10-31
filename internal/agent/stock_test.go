package agent

import (
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Regression for the audit finding that stock was never
// consumed: sales completed against the same shelf forever,
// a stock-1 one-off item could be sold twice, buy-again
// completed at stock zero, and twelve yards were sold from
// a six-yard piece. Stock is now taken at the mint, checked
// against the live catalogue, and given back when an unpaid
// order dies.
const stockOwner = "233550000001"

func completeSaleFor(t *testing.T, shopID, buyer string, p *catalogue.Product, qty float64) {
	t.Helper()
	// InHandOK stands for the merchant's confirmation where
	// a sale needs it (a one-off piece): these tests judge
	// the stock gate, not the in-hand gate.
	s := &Session{Stage: "fulfilment", MerchantID: shopID, Product: p, Qty: qty, AgreedPrice: p.ListPrice, InHandOK: true}
	if qty > 0 && p.Unit != "" {
		s.AgreedPrice = p.UnitPrice() * qty
	}
	sessions.put(sessionKey(shopID, buyer), s)
	completeSale(buyer, s, s.AgreedPrice, fulfilment{Mode: "pickup"})
}

func TestStockIsConsumedAtTheMint(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	if _, err := catalogue.SetStock("demo", "Handmade Beaded Bag", 2); err != nil {
		t.Fatal(err)
	}
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	completeSaleFor(t, "demo", "233551111111", bag, 0)
	completeSaleFor(t, "demo", "233552222222", bag, 0)
	if got := catalogue.FindByName("demo", "Handmade Beaded Bag"); got == nil || got.Stock != 0 {
		t.Fatalf("two sales of a stock-2 product must leave stock 0, got %+v", got)
	}
	if n := orders.Default.CountFor("demo"); n != 2 {
		t.Fatalf("two orders should stand, got %d", n)
	}
	// The third sale is refused at the gate: no order, and
	// the buyer hears the shelf is empty.
	completeSaleFor(t, "demo", "233553333333", bag, 0)
	if n := orders.Default.CountFor("demo"); n != 2 {
		t.Fatalf("a sale past the shelf must not mint an order, got %d orders", n)
	}
}

func TestMeasureStockIsCountedInUnits(t *testing.T) {
	setupChat(t, stockOwner, measureShop)
	if _, err := catalogue.SetStock("demo", "Ankara Fabric (6 yards)", 1); err != nil {
		t.Fatal(err)
	}
	ankara := catalogue.FindByName("demo", "Ankara Fabric (6 yards)")
	if ankara == nil {
		t.Fatal("fixture product missing")
	}
	// Twelve yards from a six-yard piece (stock 1) refuses.
	completeSaleFor(t, "demo", "233551111111", ankara, 12)
	if n := orders.Default.CountFor("demo"); n != 0 {
		t.Fatalf("an over-shelf measure sale must not mint, got %d orders", n)
	}
	// Three yards completes and leaves three yards.
	completeSaleFor(t, "demo", "233551111111", ankara, 3)
	got := catalogue.FindByName("demo", "Ankara Fabric (6 yards)")
	if got == nil || got.AvailableUnits() != 3 {
		t.Fatalf("after 3 of 6 yards, 3 must remain, got %+v", got)
	}
	if got.Stock != 1 {
		t.Fatalf("the partly cut piece still counts as one in stock, got %d", got.Stock)
	}
	// The last three complete; a further yard refuses.
	completeSaleFor(t, "demo", "233552222222", ankara, 3)
	if got := catalogue.FindByName("demo", "Ankara Fabric (6 yards)"); got == nil || got.AvailableUnits() != 0 {
		t.Fatalf("the piece must be exhausted, got %+v", got)
	}
	completeSaleFor(t, "demo", "233553333333", ankara, 1)
	if n := orders.Default.CountFor("demo"); n != 2 {
		t.Fatalf("only the two covered sales may stand, got %d orders", n)
	}
}

func TestUnpaidDeathGivesStockBackOnce(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	completeSaleFor(t, "demo", "233551111111", bag, 0)
	if got := catalogue.FindByName("demo", "Handmade Beaded Bag"); got == nil || got.Stock != 2 {
		t.Fatalf("one sale of stock 3 must leave 2, got %+v", got)
	}
	// The merchant cancels the unpaid order: the unit returns.
	var ref string
	for _, o := range orders.Default.All() {
		if o.MerchantID == "demo" {
			ref = o.Reference
		}
	}
	merchant := catalogue.GetMerchant("demo")
	cancelMerchantOrder(stockOwner, merchant, ref)
	if got := catalogue.FindByName("demo", "Handmade Beaded Bag"); got == nil || got.Stock != 3 {
		t.Fatalf("cancelling the unpaid order must restore stock to 3, got %+v", got)
	}
	// A second cancellation attempt changes nothing.
	cancelMerchantOrder(stockOwner, merchant, ref)
	if got := catalogue.FindByName("demo", "Handmade Beaded Bag"); got == nil || got.Stock != 3 {
		t.Fatalf("stock must never be restored twice, got %+v", got)
	}
}
