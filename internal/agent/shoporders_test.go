package agent

// The merchant's orders desk: setting a stock count, the
// "my orders" view, and cancelling an order from her side
// of the counter. Each test drives the real inbound path
// and reads the books on disk as the truth.

import (
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

const shopOrdersOwner = "233550004111"
const shopOrdersBuyer = "233550004112"

const shopOrdersSeed = `{"merchants":{"efua":{"id":"efua","name":"Efua's Store","shopCode":"EFUA","ownerWhatsApp":"` + shopOrdersOwner + `","payoutPhone":"` + shopOrdersOwner + `","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":1}]}}}`

func TestSetStockCommand(t *testing.T) {
	setupChat(t, shopOrdersOwner, shopOrdersSeed)
	c := &chat{seen: map[string]int{}}

	c.say(t, shopOrdersOwner, "set stock of Handmade Beaded Bag to 7")
	got := strings.Join(c.replies(t, shopOrdersOwner), "\n")
	if !strings.Contains(got, "now has 7 in stock") {
		t.Fatalf("stock reply = %q", got)
	}
	p := catalogue.FindByName("efua", "Handmade Beaded Bag")
	if p == nil || p.Stock != 7 {
		t.Fatalf("catalogue stock = %+v", p)
	}
	c.say(t, shopOrdersOwner, "my catalogue")
	got = strings.Join(c.replies(t, shopOrdersOwner), "\n")
	if !strings.Contains(got, "7 in stock") {
		t.Fatalf("catalogue readback lacks the count: %q", got)
	}
	c.say(t, shopOrdersOwner, "set stock of Handmade Beaded Bag to 1")
	got = strings.Join(c.replies(t, shopOrdersOwner), "\n")
	if !strings.Contains(got, "confirm you have it in hand") {
		t.Fatalf("one-in-stock reply should name the in-hand rule: %q", got)
	}
	c.say(t, shopOrdersOwner, "set stock of Walking Stick to 3")
	got = strings.Join(c.replies(t, shopOrdersOwner), "\n")
	if !strings.Contains(got, "Nothing changed") {
		t.Fatalf("unknown product reply = %q", got)
	}
}

func TestMyOrdersView(t *testing.T) {
	setupChat(t, shopOrdersOwner, shopOrdersSeed)
	c := &chat{seen: map[string]int{}}

	c.say(t, shopOrdersOwner, "my orders")
	got := strings.Join(c.replies(t, shopOrdersOwner), "\n")
	if !strings.Contains(got, "No orders yet") {
		t.Fatalf("empty view = %q", got)
	}

	paidOrderFor("efua", "EFUA-0001", shopOrdersBuyer, 730)
	orders.Default.RecordOrder("EFUA-0002", orders.Order{
		From: shopOrdersBuyer, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ProductName: "Handmade Beaded Bag", Price: 850, Total: 850,
		Status: "pending", MerchantID: "efua",
	})
	c.say(t, shopOrdersOwner, "my orders")
	got = strings.Join(c.replies(t, shopOrdersOwner), "\n")
	for _, want := range []string{"EFUA-0001", "paid, not sent yet", "EFUA-0002", "waiting for payment", "1 paid order is waiting to be sent."} {
		if !strings.Contains(got, want) {
			t.Fatalf("orders view lacks %q: %q", want, got)
		}
	}
}

func TestMerchantCancelsPendingOrder(t *testing.T) {
	setupChat(t, shopOrdersOwner, shopOrdersSeed)
	c := &chat{seen: map[string]int{}}

	orders.Default.RecordOrder("EFUA-0001", orders.Order{
		From: shopOrdersBuyer, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ProductName: "Handmade Beaded Bag", Price: 730, Total: 730,
		Status: "pending", MerchantID: "efua",
	})
	daybook.Default.RecordSale("efua", daybook.Sale{ListPrice: 850, AgreedPrice: 730, Allowance: 300})

	c.say(t, shopOrdersOwner, "cancel order EFUA-0001")
	got := strings.Join(c.replies(t, shopOrdersOwner), "\n")
	if !strings.Contains(got, "is cancelled") || !strings.Contains(got, "Today's book has been corrected") {
		t.Fatalf("merchant cancel reply = %q", got)
	}
	if o := orders.Default.GetOrder("EFUA-0001"); o == nil || o.Status != "cancelled" {
		t.Fatalf("order state = %+v", o)
	}
	buyerGot := strings.Join(c.replies(t, shopOrdersBuyer), "\n")
	if !strings.Contains(buyerGot, "has been cancelled by the shop") || !strings.Contains(buyerGot, "Nothing was paid") {
		t.Fatalf("buyer cancel notice = %q", buyerGot)
	}
	state := daybook.Default.GetDayState("efua", daybook.Options{Allowance: 300})
	if state.Sales != 0 || state.Revenue != 0 || state.GivenAway != 0 {
		t.Fatalf("day book after cancel = %+v", state)
	}

	c.say(t, shopOrdersOwner, "cancel order EFUA-0001")
	got = strings.Join(c.replies(t, shopOrdersOwner), "\n")
	if !strings.Contains(got, "already cancelled") {
		t.Fatalf("double cancel reply = %q", got)
	}
	c.say(t, shopOrdersOwner, "cancel order KOFI-0009")
	got = strings.Join(c.replies(t, shopOrdersOwner), "\n")
	if !strings.Contains(got, "cannot find order") {
		t.Fatalf("foreign order reply = %q", got)
	}
}

func TestMerchantCancelsPaidOrderAndBuyerCanStillDispute(t *testing.T) {
	setupChat(t, shopOrdersOwner, shopOrdersSeed)
	c := &chat{seen: map[string]int{}}

	paidOrderFor("efua", "EFUA-0001", shopOrdersBuyer, 730)
	c.say(t, shopOrdersOwner, "cancel order EFUA-0001")
	got := strings.Join(c.replies(t, shopOrdersOwner), "\n")
	if !strings.Contains(got, "refund is owed") {
		t.Fatalf("paid cancel reply = %q", got)
	}
	buyerGot := strings.Join(c.replies(t, shopOrdersBuyer), "\n")
	if !strings.Contains(buyerGot, "owes you that money back") {
		t.Fatalf("buyer paid-cancel notice = %q", buyerGot)
	}

	// The promise in that notice is real: her dispute words
	// still open a dispute on the cancelled order, because
	// the money moved.
	c.say(t, shopOrdersBuyer, "SHOP-EFUA")
	c.replies(t, shopOrdersBuyer)
	c.say(t, shopOrdersBuyer, "This is a scam. I paid for my bag, she cancelled it and kept my money")
	c.replies(t, shopOrdersBuyer)
	if o := orders.Default.GetOrder("EFUA-0001"); o == nil || o.DisputeState != "open" {
		t.Fatalf("dispute on cancelled-paid order = %+v", o)
	}
}

func TestCancelReceivedOrderRefuses(t *testing.T) {
	setupChat(t, shopOrdersOwner, shopOrdersSeed)
	c := &chat{seen: map[string]int{}}

	paidOrderFor("efua", "EFUA-0001", shopOrdersBuyer, 730)
	orders.Default.Update("EFUA-0001", func(o *orders.Order) { o.DeliveryState = "received" })
	c.say(t, shopOrdersOwner, "cancel order EFUA-0001")
	got := strings.Join(c.replies(t, shopOrdersOwner), "\n")
	if !strings.Contains(got, "already been received") {
		t.Fatalf("received cancel reply = %q", got)
	}
	if o := orders.Default.GetOrder("EFUA-0001"); o == nil || o.Status != "paid" {
		t.Fatalf("received order must stand: %+v", o)
	}
}
