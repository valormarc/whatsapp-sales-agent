package agent

// The close, after the seller set its shape (2026-10-28):
// before the payment link the order is read back and the
// buyer confirms it, corrections going back to the step they
// belong to; once the payment lands, the confirmation follows
// the post-payment format from the order's own facts, and a
// buyer's thanks closes the conversation warmly instead of
// re-opening the sale.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

func TestRecapConfirmsAndCorrectionsGoBack(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550009101"
	agreeBagAtList(t, c, buyer)
	tap(t, buyer, "ff_delivery")
	c.say(t, buyer, "Osu")
	c.say(t, buyer, "Akosua Mensah")
	c.say(t, buyer, "no")

	// The recap reads the order back and asks for her word.
	if got := c.last(t, buyer); !strings.Contains(got, "Please confirm your order") ||
		!strings.Contains(got, "Handmade Beaded Bag - GH₵850") ||
		!strings.Contains(got, "Delivery to Osu") {
		t.Fatalf("recap = %q", got)
	}
	if got := orders.Default.All(); len(got) != 0 {
		t.Fatalf("no order may exist before she confirms: %v", got)
	}

	// A correction about the name goes back to the name step,
	// with everything else held.
	c.say(t, buyer, "the name is wrong")
	if got := c.last(t, buyer); !strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("name correction = %q", got)
	}
	c.say(t, buyer, "Efua Owusu")
	c.say(t, buyer, "no")
	if got := c.last(t, buyer); !strings.Contains(got, "Efua Owusu") {
		t.Fatalf("second recap = %q, want the corrected name read back", got)
	}

	// Her yes completes the sale exactly once.
	c.say(t, buyer, "yes")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Pay here to complete your order") {
		t.Fatalf("completion = %q", got)
	}
	got := ordersOnDisk(t, dir)
	if len(got) != 1 || got[0]["packageName"] != "Efua Owusu" {
		t.Fatalf("orders = %v, want one order under the corrected name", got)
	}
}

func TestSettleRecordIsTheFixedPaper(t *testing.T) {
	setupChat(t, "233550009101", shopWithBag)
	o := orders.Order{
		Reference: "TEST-9001", ProductName: "Handmade Beaded Bag", Price: 850, Total: 850,
		Fulfilment: "delivery", Location: "Osu", MerchantID: MerchantID,
	}
	want := "Payment confirmed\nItem: Handmade Beaded Bag\nAmount: GH\u20b5850\nDelivery to: Osu\nOrder: TEST-9001\nThank you for buying from Test Shop."
	if got := ComposeBuyerSettleText(&o, ""); got != want {
		t.Fatalf("settlement record = %q, want exactly %q", got, want)
	}
	// The record is printed from the order's facts, so it can
	// never carry an invented figure or an unmade promise.
	if strings.Contains(strings.ToLower(ComposeBuyerSettleText(&o, "")), "rider") {
		t.Fatal("the record must never promise a rider")
	}
}

func TestSettleRecordCarriesTheOrderFacts(t *testing.T) {
	setupChat(t, "233550009102", shopWithBag)
	o := orders.Order{
		Reference: "TEST-9002", ProductName: "Handmade Beaded Bag", Price: 850, Total: 850,
		Fulfilment: "delivery", Location: "Osu", DeliveryFee: 25, DeliveryPaidTo: "carrier", MerchantID: MerchantID,
	}
	got := ComposeBuyerSettleText(&o, "")
	for _, want := range []string{"Payment confirmed", "Item: Handmade Beaded Bag", "Amount: GH\u20b5850", "Delivery to: Osu", "Delivery fee: GH\u20b525 to pay on arrival", "Order: TEST-9002", "Thank you for buying from Test Shop."} {
		if !strings.Contains(got, want) {
			t.Fatalf("settlement = %q, want it to carry %q", got, want)
		}
	}
}

func TestThanksAfterAPurchaseClosesWarmly(t *testing.T) {
	merchant := "233550009103"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550009104"
	orders.Default.RecordOrder("TEST-9003", orders.Order{
		Reference: "TEST-9003", From: buyer, Status: "paid", MerchantID: MerchantID,
		ProductName: "Handmade Beaded Bag", Price: 850, Total: 850,
	})
	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Thank you")
	got := c.last(t, buyer)
	if !strings.Contains(got, "You are welcome") || !strings.Contains(got, "TEST-9003") {
		t.Fatalf("thanks after a purchase = %q, want a warm close naming her order", got)
	}
	if strings.Contains(got, "Here is what we have") || strings.Contains(got, "outside my work") {
		t.Fatalf("a closing buyer was pitched or steered: %q", got)
	}
}
