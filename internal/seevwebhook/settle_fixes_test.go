package seevwebhook

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-2 regression: the failed branch ran with no status
// precondition, so for a sessionless order — where the
// session binding is vacuous — a failed event flipped
// even a cancelled order to failed, terminal for that
// order's payment.
func TestFailedEventCannotFlipACancelledSessionlessOrder(t *testing.T) {
	t.Setenv("SEEV_WEBHOOK_SECRET", whSecret)
	t.Setenv("WHATSAPP_TOKEN", "")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	orders.Default.RecordOrder("TEST-C001", orders.Order{
		From: whBuyer, ProductName: "Test Bag", Price: 100,
		MerchantID: "demo", Status: "cancelled",
	})
	body := fmt.Sprintf(`{"reference":"sess-x","status":"failed","meta":{"order_reference":"TEST-C001"}}`)
	ts, sig := sign(body, whSecret)
	if code := Handle(body, ts, sig); code != 200 {
		t.Fatalf("the event is acknowledged, got %d", code)
	}
	if o := orders.Default.GetOrder("TEST-C001"); o == nil || o.Status != "cancelled" {
		t.Fatalf("a cancelled order must stand, got %+v", o)
	}
}

// The hold's one exemption, kept narrow on purpose: an
// order the book marks dry-run was minted by the
// disabled-checkout factory, holds no session by design,
// and settles on its verified event as it always has.
func TestPaidEventStillSettlesADryRunOrder(t *testing.T) {
	t.Setenv("SEEV_WEBHOOK_SECRET", whSecret)
	t.Setenv("SEEV_SECRET_KEY", "test-seev-key")
	t.Setenv("WHATSAPP_TOKEN", "")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	orders.Default.RecordOrder("TEST-C004", orders.Order{
		From: whBuyer, ProductName: "Test Bag", Price: 100,
		MerchantID: "demo", Status: "pending", DryRun: true,
	})
	body := paidEvent("seev-c4", "TEST-C004", 100)
	ts, sig := sign(body, whSecret)
	if code := Handle(body, ts, sig); code != 200 {
		t.Fatalf("a verified event is acknowledged, got %d", code)
	}
	if o := orders.Default.GetOrder("TEST-C004"); o == nil || o.Status != "paid" {
		t.Fatalf("a dry-run order settles on its verified event, got %+v", o)
	}
}

// Run-3 overturned the posture this test used to assert.
// It began as a run-2 regression (an anonymised order's
// failure notice was addressed to the literal string
// "deleted") and asserted the order may still close on
// its failed event. The third audit confirmed the deeper
// defect: the order is sessionless — anonymisation struck
// its ProviderRef — so NO failed event can be bound to
// it, and closing it restocked goods on an event that
// cannot be shown to be about this order at all. Like
// the paid road, a sessionless order's failed event is
// now a no-op: the order stands, and trivially nobody is
// messaged at "deleted" either.
func TestFailedEventNeverMessagesADeletedBuyer(t *testing.T) {
	t.Setenv("SEEV_WEBHOOK_SECRET", whSecret)
	t.Setenv("WHATSAPP_TOKEN", "")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	orders.Default.RecordOrder("TEST-C002", orders.Order{
		From: "deleted", ProductName: "Test Bag", Price: 100,
		MerchantID: "demo", Status: "pending",
	})
	body := fmt.Sprintf(`{"reference":"sess-y","status":"failed","meta":{"order_reference":"TEST-C002"}}`)
	ts, sig := sign(body, whSecret)
	if code := Handle(body, ts, sig); code != 200 {
		t.Fatalf("the event is acknowledged, got %d", code)
	}
	if o := orders.Default.GetOrder("TEST-C002"); o == nil || o.Status != "pending" {
		t.Fatalf("a sessionless order must not close on an unbindable failed event, got %+v", o)
	}
	raw, _ := os.ReadFile("data/outbox.jsonl")
	if strings.Contains(string(raw), `"to":"deleted"`) {
		t.Fatal("no message may ever be addressed to the literal string deleted")
	}
}

// Run-2 regression: SettleVerified spoke from the
// caller's pre-transition copy. A buyer who deleted her
// data inside the caller's window was still messaged at
// her struck number, and the merchant notice re-disclosed
// it. Settlement now speaks from the stored record: the
// anonymised order still settles (the money is real), and
// nobody is told anything about the deleted buyer.
func TestSettleVerifiedSpeaksFromTheFreshRecord(t *testing.T) {
	t.Setenv("WHATSAPP_TOKEN", "")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	session := "sess-fresh"
	orders.Default.RecordOrder("TEST-C003", orders.Order{
		From: whBuyer, ProductName: "Test Bag", Price: 100,
		MerchantID: "demo", Status: "pending", ProviderRef: &session,
	})
	stale := orders.Default.GetOrder("TEST-C003")
	orders.Default.AnonymiseBuyer(whBuyer)

	// The package outbox is shared across tests, so the
	// assertions count this buyer's lines before and
	// after: the settle under test must add none.
	count := func(needle string) int {
		raw, _ := os.ReadFile("data/outbox.jsonl")
		return strings.Count(string(raw), needle)
	}
	toBuyerBefore := count(`"to":"` + whBuyer + `"`)
	disclosedBefore := count("Buyer: " + whBuyer)

	SettleVerified(stale, "Payment received, thank you.")

	o := orders.Default.GetOrder("TEST-C003")
	if o == nil || o.Status != "paid" {
		t.Fatalf("the anonymised order still settles, got %+v", o)
	}
	if count(`"to":"`+whBuyer+`"`) != toBuyerBefore {
		t.Fatal("the deleted buyer must not be messaged from the stale copy")
	}
	if count("Buyer: "+whBuyer) != disclosedBefore {
		t.Fatal("the merchant notice must not re-disclose the deleted buyer's number")
	}
}
