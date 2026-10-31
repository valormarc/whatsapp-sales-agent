package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-4 records, the delivery lane: naming an order by
// its goods fell back to the newest sent order, and the
// delivery form's town overwrote the place while the fee
// and recap stayed bound to the old one.

func seedSentOrder(ref, buyer, product string) {
	orders.Default.RecordOrder(ref, orders.Order{
		Reference: ref, Status: "paid", From: buyer, MerchantID: "demo",
		ProductName: product, Price: 100, DeliveryState: "sent",
	})
}

func TestProductNamedConfirmationBindsThatOrder(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233550006101"
	seedSentOrder("TEST-DN-1", buyer, "Handmade Beaded Bag")
	seedSentOrder("TEST-DN-2", buyer, "Leather Sandals")
	merchant := catalogue.GetMerchant("demo")

	inTurn(buyer, func() bool {
		return buyerConfirmReceived(buyer, merchant, "demo", "the handmade beaded bag has arrived, thank you")
	})
	if o := orders.Default.GetOrder("TEST-DN-1"); o == nil || o.DeliveryState != "received" {
		t.Fatalf("the named order must be the one confirmed, got %+v", o)
	}
	if o := orders.Default.GetOrder("TEST-DN-2"); o == nil || o.DeliveryState != "sent" {
		t.Fatalf("the newer order must stay untouched, got %+v", o)
	}
}

func TestProductNamedConfirmationAsksWhenTwoMatch(t *testing.T) {
	dir := setupChat(t, stockOwner, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550006102"
	seedSentOrder("TEST-DN-3", buyer, "Handmade Beaded Bag")
	seedSentOrder("TEST-DN-4", buyer, "Handmade Beaded Bag")
	merchant := catalogue.GetMerchant("demo")

	inTurn(buyer, func() bool {
		return buyerConfirmReceived(buyer, merchant, "demo", "the handmade beaded bag has arrived")
	})
	for _, ref := range []string{"TEST-DN-3", "TEST-DN-4"} {
		if o := orders.Default.GetOrder(ref); o == nil || o.DeliveryState != "sent" {
			t.Fatalf("an ambiguous naming must change nothing, %s is %+v", ref, o)
		}
	}
	joined := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(joined, "TEST-DN-3") || !strings.Contains(joined, "TEST-DN-4") {
		t.Fatalf("the ask must name both references, got %q", joined)
	}
}

func TestFlowTownChangeRebindsTheFee(t *testing.T) {
	setupChat(t, stockOwner, `{"merchants":{"demo":{"id":"demo","name":"Test Shop","ownerPhone":"`+stockOwner+`","deliveryFee":20,"deliveryAreas":[{"area":"Kasoa","fee":40}],"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"x","listPrice":850,"floorPrice":680,"stock":3}]}}}`)
	buyer := "233550006103"
	s := &Session{
		MerchantID:    "demo",
		Stage:         "fulfilment",
		FulfilStep:    "pkgname",
		PendingFulfil: fulfilment{Mode: "delivery", Location: "Osu", Fee: 20},
	}
	sessions.put(sessionKey("demo", buyer), s)
	inTurn(buyer, func() bool {
		completeDeliveryFlow(buyer, s, flowAnswers{BuyerName: "Ama Serwaa", Town: "Kasoa", Phone: "0551234567"})
		return true
	})
	got, ok := sessions.lookup(sessionKey("demo", buyer))
	if !ok {
		t.Fatal("the session must stand")
	}
	if got.PendingFulfil.Location != "Kasoa" {
		t.Fatalf("the town must move, got %q", got.PendingFulfil.Location)
	}
	if got.PendingFulfil.Fee != 40 {
		t.Fatalf("the fee must rebind to the new town's own fee, got %v", got.PendingFulfil.Fee)
	}
}
