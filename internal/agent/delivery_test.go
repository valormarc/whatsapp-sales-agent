package agent

// The delivery loop: after the money lands, the seller's
// word that the goods have gone reaches the buyer, the
// buyer's word that they arrived reaches the seller, and a
// buyer whose goods never came is passed to the seller with
// her number. The acts are driven directly here, the way the
// merchant thinking tests drive theirs; the readings that
// name them are proven live.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

const deliveryCatalogue = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","ownerWhatsApp":"233550002001","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":3}]}}}`

func seedPaidOrder(t *testing.T, ref, buyer string) {
	t.Helper()
	orders.Default.RecordOrder(ref, orders.Order{
		From: buyer, Status: "paid", ProductName: "Handmade Beaded Bag",
		Price: 850, Total: 850, MerchantID: MerchantID,
		Fulfilment: "delivery", Location: "Accra",
	})
}

func TestDeliveryLoopClosesTheSale(t *testing.T) {
	owner := "233550002001"
	dir := setupChat(t, owner, deliveryCatalogue)
	c := &chat{dir: dir}
	shop := catalogue.GetMerchant(MerchantID)
	buyer := "233550002002"
	seedPaidOrder(t, "EFUA-1001", buyer)

	// The seller says it has gone; the buyer hears it.
	if !inTurn(owner, func() bool { return actOnMerchantUnderstanding(owner, shop, MerchantID, "I have sent the bag order, it is with the rider", &understood{Intent: intentOrderSent}) }) {
		t.Fatalf("the sent report should be served")
	}
	o := orders.Default.GetOrder("EFUA-1001")
	if o.DeliveryState != "sent" || o.SentAt == "" {
		t.Fatalf("the order should stand sent with a stamp, got %+v", o)
	}
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "has sent your order EFUA-1001") {
		t.Fatalf("the buyer should be told, got %q", got)
	}
	// The readback now narrates the goods, not only the money.
	rb := orderReadback(shop, o)
	if !strings.Contains(rb, "on its way") {
		t.Fatalf("the readback should say the goods are moving, got %q", rb)
	}

	// The buyer confirms arrival; the seller hears the loop close.
	if !actOnBuyerUnderstanding(buyer, &Session{Stage: "browsing"}, shop, MerchantID, &understood{Intent: intentOrderReceived}) {
		t.Fatalf("the arrival confirmation should be served")
	}
	o = orders.Default.GetOrder("EFUA-1001")
	if o.DeliveryState != "received" || o.ReceivedAt == "" {
		t.Fatalf("the order should stand received, got %+v", o)
	}
	if got := strings.Join(c.replies(t, owner), "\n"); !strings.Contains(got, "confirmed she received") {
		t.Fatalf("the seller should be told, got %q", got)
	}
}

func TestDeliveryMissingIsPassedToTheSeller(t *testing.T) {
	owner := "233550002001"
	dir := setupChat(t, owner, deliveryCatalogue)
	c := &chat{dir: dir}
	shop := catalogue.GetMerchant(MerchantID)
	buyer := "233550002003"
	seedPaidOrder(t, "EFUA-1002", buyer)

	// The seeded order was never dispatched. The report
	// is still passed to the seller at once, with the
	// buyer's number — but as a chase in the order's true
	// position (the seventh audit): the "missing" state
	// is dispatch-gated, so an undispatched order keeps
	// its place on the waiting-to-send lists.
	if !actOnBuyerUnderstanding(buyer, &Session{Stage: "browsing"}, shop, MerchantID, &understood{Intent: intentOrderMissing}) {
		t.Fatalf("the missing report should be served")
	}
	o := orders.Default.GetOrder("EFUA-1002")
	if o.DeliveryState != "" {
		t.Fatalf("an undispatched order must not advance to missing, got %+v", o)
	}
	got := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "chasing") || !strings.Contains(got, "not been marked as sent") || !strings.Contains(got, buyer) {
		t.Fatalf("the seller should get the chase with the buyer's number, got %q", got)
	}

	// Once the order is dispatched, the same report does
	// record missing, in the original words.
	orders.Default.Update("EFUA-1002", func(x *orders.Order) {
		x.DeliveryState = "sent"
		x.SentAt = "2026-10-31T00:00:00Z"
	})
	_ = c.replies(t, owner)
	if !actOnBuyerUnderstanding(buyer, &Session{Stage: "browsing"}, shop, MerchantID, &understood{Intent: intentOrderMissing}) {
		t.Fatalf("the dispatched missing report should be served")
	}
	o = orders.Default.GetOrder("EFUA-1002")
	if o.DeliveryState != "missing" {
		t.Fatalf("a dispatched order's report should stand missing, got %+v", o)
	}
	got = strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "has not arrived") || !strings.Contains(got, buyer) {
		t.Fatalf("the seller should get the missing report with the buyer's number, got %q", got)
	}
}

func TestDeliveryGuards(t *testing.T) {
	owner := "233550002001"
	dir := setupChat(t, owner, deliveryCatalogue)
	c := &chat{dir: dir}
	shop := catalogue.GetMerchant(MerchantID)
	buyer := "233550002004"
	// An unpaid order cannot be marked sent, even named.
	orders.Default.RecordOrder("EFUA-1003", orders.Order{
		From: buyer, Status: "pending", ProductName: "Handmade Beaded Bag",
		Price: 850, Total: 850, MerchantID: MerchantID,
	})
	if !inTurn(owner, func() bool { return actOnMerchantUnderstanding(owner, shop, MerchantID, "I have sent EFUA-1003", &understood{Intent: intentOrderSent}) }) {
		t.Fatalf("the named unpaid order should be answered")
	}
	if got := strings.Join(c.replies(t, owner), "\n"); !strings.Contains(got, "has not been paid yet") {
		t.Fatalf("the refusal should be plain, got %q", got)
	}
	if o := orders.Default.GetOrder("EFUA-1003"); o.DeliveryState != "" {
		t.Fatalf("an unpaid order must not move, got %+v", o)
	}
	// A stranger cannot confirm or complain about an order
	// that is not hers.
	seedPaidOrder(t, "EFUA-1004", buyer)
	stranger := "233550002099"
	if actOnBuyerUnderstanding(stranger, &Session{Stage: "browsing"}, shop, MerchantID, &understood{Intent: intentOrderReceived}) {
		t.Fatalf("a stranger's confirmation must not be served")
	}
	if o := orders.Default.GetOrder("EFUA-1004"); o.DeliveryState != "" {
		t.Fatalf("the order must not move on a stranger's word, got %+v", o)
	}
}
