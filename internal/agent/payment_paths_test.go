package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/seevwebhook"
)

// One payment door. The buyer pays through the payment button
// Stall issues for her order, and Stall confirms it itself
// against Seev. There is no second route: the shop's payout
// number is settlement-only and is never shown to a buyer, a
// payment she says went somewhere else is one Stall cannot
// see or confirm, and a screenshot settles nothing — no claim
// is opened, no merchant is asked to confirm anything, and
// no one's word moves an order to paid. These tests pin the
// whole discipline: the single offer, the refused number, the
// unconfirmable report, the screenshot that changes nothing,
// and the shop walls around all of it.

// sayImage delivers one inbound image message, the way a buyer
// sends her transfer screenshot.
func sayImage(t *testing.T, from, mediaID, caption string) {
	t.Helper()
	HandleInbound(Inbound{From: from, ImageID: mediaID, Caption: caption})
}

type rawOutbox struct {
	To       string `json:"to"`
	Body     string `json:"body"`
	Kind     string `json:"kind"`
	MediaRef string `json:"mediaRef"`
}

func rawOutboxRecords(t *testing.T, dir string) []rawOutbox {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "data", "outbox.jsonl"))
	if err != nil {
		return nil
	}
	var out []rawOutbox
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec rawOutbox
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("outbox line not JSON: %v", err)
		}
		out = append(out, rec)
	}
	return out
}

func outboxCountContaining(t *testing.T, dir, to, substr string) int {
	t.Helper()
	n := 0
	for _, rec := range rawOutboxRecords(t, dir) {
		if rec.To == to && strings.Contains(rec.Body, substr) {
			n++
		}
	}
	return n
}

// closePickupSale walks a buyer through a purchase at the list
// price, out the pickup door, in the Twitter shops: the agreement,
// the fulfilment answer, and the completion pieces. It returns
// the completion pieces joined.
func closePickupSale(t *testing.T, c *chat, buyer, entry, view string, listPrice string) string {
	t.Helper()
	c.say(t, buyer, entry)
	c.replies(t, buyer)
	c.say(t, buyer, view)
	c.last(t, buyer)
	c.say(t, buyer, listPrice)
	c.replies(t, buyer)
	c.say(t, buyer, "I will pick it up myself")
	c.finishDetails(t, buyer)
	return strings.Join(c.replies(t, buyer), "\n")
}

func TestSaleCompletionOffersOnlyThePaymentButton(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550008001"

	got := closePickupSale(t, c, buyer, "SHOP-EFUA", "Shea butter", "120")
	// One door only: the button. The shop's payout number
	// appears nowhere in anything the buyer is sent.
	if !strings.Contains(got, "Pay here") {
		t.Fatalf("completion = %q, want the payment button", got)
	}
	for _, rec := range rawOutboxRecords(t, dir) {
		if rec.To == buyer && strings.Contains(rec.Body, ownerEfua) {
			t.Fatalf("the payout number reached the buyer: %+v", rec)
		}
	}
	c.replies(t, ownerEfua) // drain the New order notice

	// She asks for the number: it is refused, plainly, and the
	// button is sent again. The order stays open and unsettled.
	c.say(t, buyer, "I will pay to the number")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "only through the payment button") || !strings.Contains(got, "still open") {
		t.Fatalf("number request answer = %q", got)
	}
	if strings.Contains(got, ownerEfua) {
		t.Fatalf("the number was given out after all: %q", got)
	}
	o := ordersOnDisk(t, dir)[0]
	if o["status"] != "pending" || o["paymentPath"] != nil {
		t.Fatalf("order after the number request = %v, want open with no path settled", o)
	}

	// The screenshot arrives: it confirms nothing, is forwarded
	// to no one, and the button is what she is given again.
	sayImage(t, buyer, "MEDIA-TEST-1", "")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "cannot confirm a payment here") || !strings.Contains(got, "Click to pay") {
		t.Fatalf("screenshot answer = %q", got)
	}
	if got := c.replies(t, ownerEfua); len(got) != 0 {
		t.Fatalf("the merchant was troubled with a screenshot: %q", got)
	}
	for _, rec := range rawOutboxRecords(t, dir) {
		if rec.To == ownerEfua && rec.Kind == "image" {
			t.Fatalf("the screenshot was forwarded to the merchant: %+v", rec)
		}
	}
	if o := ordersOnDisk(t, dir)[0]; o["status"] != "pending" {
		t.Fatalf("order after the screenshot = %v, want still open", o)
	}

	// Her word alone settles nothing either: the report goes to
	// be checked against the payment service. This harness walk
	// minted no real session, so she is told there is no session
	// to check — the point either way is that nothing settles
	// on her word.
	c.say(t, buyer, "I have paid")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "no payment session to check") {
		t.Fatalf("paid report answer = %q", got)
	}
	if o := ordersOnDisk(t, dir)[0]; o["status"] != "pending" {
		t.Fatalf("order after the word-only report = %v, want still open", o)
	}
	if got := c.replies(t, ownerEfua); len(got) != 0 {
		t.Fatalf("the merchant was asked to confirm a payment: %q", got)
	}
}

func TestPaymentReportWithNoUnpaidOrderTroublesNoMerchant(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550008004"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "I have paid")
	if got := c.last(t, buyer); !strings.Contains(got, "no unpaid order for you here just now") {
		t.Fatalf("no-order report reply = %q", got)
	}
	if got := c.replies(t, ownerEfua); len(got) != 0 {
		t.Fatalf("the merchant was troubled with a report that has no order: %q", got)
	}
	// A screenshot with a claiming caption and no order behind
	// it gets the same plain answer, and still no merchant
	// message.
	sayImage(t, buyer, "MEDIA-TEST-9", "I have paid")
	if got := c.last(t, buyer); !strings.Contains(got, "no unpaid order for you here just now") {
		t.Fatalf("no-order image reply = %q", got)
	}
	if got := c.replies(t, ownerEfua); len(got) != 0 {
		t.Fatalf("the merchant was troubled with an orderless screenshot: %q", got)
	}
}

func TestPaymentReportsNeverCrossShops(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550008005"

	// Her order is Ama's. Standing in Efua's shop, her report
	// attaches to nothing: it is answered as a report with no
	// order here, and neither merchant hears a word of it.
	closePickupSale(t, c, buyer, "SHOP-AMA", "Smoked Tilapia", "180")
	c.replies(t, ownerAma)
	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "I have paid")
	if got := c.last(t, buyer); !strings.Contains(got, "no unpaid order for you here just now") {
		t.Fatalf("report in the wrong shop = %q", got)
	}
	if got := c.replies(t, ownerAma); len(got) != 0 {
		t.Fatalf("Ama heard a report made in Efua's shop: %q", got)
	}
	if o := ordersOnDisk(t, dir)[0]; o["status"] != "pending" {
		t.Fatalf("Ama's order was touched from another shop: %v", o)
	}

	// Back in Ama's shop the report is answered by the agent's
	// own check of the link — never by asking the merchant.
	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "I have paid")
	c.replies(t, buyer)
	if got := c.replies(t, ownerAma); len(got) != 0 {
		t.Fatalf("the merchant was asked to confirm a report: %q", got)
	}
	if o := ordersOnDisk(t, dir)[0]; o["status"] != "pending" {
		t.Fatalf("order after the in-shop report = %v, want still open", o)
	}

	// Efua's yes settles nothing of Ama's: there is no claim
	// for it to land on, and it falls through to her ordinary
	// handling.
	c.say(t, ownerEfua, "yes")
	c.replies(t, ownerEfua)
	if o := ordersOnDisk(t, dir)[0]; o["status"] != "pending" {
		t.Fatalf("Efua's yes touched Ama's order: %v", o)
	}
	if got := c.replies(t, buyer); len(got) != 0 {
		t.Fatalf("the buyer was told something by another shop's yes: %q", got)
	}
}

func TestLinkSettlementStillSettlesAndMarksTheSeevPath(t *testing.T) {
	t.Setenv("SEEV_SECRET_KEY", "")
	t.Setenv("SEEV_WEBHOOK_SECRET", "whsec_paths")
	dir, c := setupTwoShops(t)
	buyer := "233550008006"

	c.say(t, buyer, "SHOP-AMA")
	c.last(t, buyer)
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.last(t, buyer)
	c.say(t, buyer, "160")
	c.replies(t, buyer)
	c.say(t, buyer, "162")
	c.replies(t, buyer)
	c.say(t, buyer, "Koforidua")
	c.finishDetails(t, buyer)
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Pay here") {
		t.Fatalf("completion = %q", got)
	}
	placed := ordersOnDisk(t, dir)[0]
	ref := placed["reference"].(string)
	c.replies(t, ownerAma)

	// The webhook must name what the buyer actually agreed: the
	// haggle carries a deliberate random trader variation, so the
	// closing price is read from the order, never assumed.
	agreed := int(placed["total"].(float64))
	t.Setenv("SEEV_SECRET_KEY", "sk_test_paths")
	fakeSeevReadback(t, "completed", float64(agreed*100))
	body := `{"data":{"reference":"SEEVSIM9","status":"completed","amount":` + strconv.Itoa(agreed) + `},"meta":{"order_reference":"` + ref + `"}}`
	ts, sig := signSeevWebhook(t, body)
	if status := seevwebhook.Handle(body, ts, sig); status != 200 {
		t.Fatalf("webhook status %d, want 200", status)
	}
	o := ordersOnDisk(t, dir)[0]
	if o["status"] != "paid" || o["paymentPath"] != "seev" {
		t.Fatalf("order after the webhook = %v, want paid on the seev path", o)
	}
	if got := c.last(t, buyer); !strings.Contains(got, "Payment confirmed") || !strings.Contains(got, "Amount: GH₵"+strconv.Itoa(agreed)) {
		t.Fatalf("buyer settlement message = %q", got)
	}
}
