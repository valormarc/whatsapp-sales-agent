package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-3 record: area-fee selection matched the place by
// raw substring, so a place containing a cheap area's name
// ("Bosuso" containing "Osu") was charged that area's fee.
func TestAreaFeeMatchesWholePhrasesOnly(t *testing.T) {
	m := &catalogue.Merchant{DeliveryFee: 40, DeliveryAreas: []catalogue.AreaFee{
		{Area: "Osu", Fee: 15}, {Area: "Kasoa", Fee: 80},
	}}
	if got := deliveryFeeFor(m, "Bosuso"); got != 40 {
		t.Fatalf("Bosuso must pay the standard fee, got %v", got)
	}
	if got := deliveryFeeFor(m, "Osu, Accra"); got != 15 {
		t.Fatalf("Osu must pay its own fee, got %v", got)
	}
	if got := deliveryFeeFor(m, "Kasoa"); got != 80 {
		t.Fatalf("Kasoa must pay its own fee, got %v", got)
	}
}

// Run-3 record: remembered fulfilment re-priced a
// returning buyer's delivery at the flat fee, ignoring
// her place's area fee.
func TestRememberedFulfilmentChargesTheAreaFee(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	merchant := catalogue.GetMerchant("demo")
	merchant.DeliveryFee = 40
	merchant.DeliveryAreas = []catalogue.AreaFee{{Area: "Kasoa", Fee: 80}}
	orders.Default.RecordOrder("TEST-REM-1", orders.Order{
		Reference: "TEST-REM-1", Status: "paid", From: "233550009501",
		ProductName: "Handmade Beaded Bag", Price: 850, MerchantID: "demo",
		Fulfilment: "delivery", Location: "Kasoa",
	})
	f, ok := rememberedFulfilment("demo", "233550009501", merchant)
	if !ok || f.Fee != 80 {
		t.Fatalf("a Kasoa redelivery must carry the Kasoa fee of 80, got %+v (ok=%v)", f, ok)
	}
}

// Run-3 record: the delivery 'where' step settled an
// unparseable answer as a fee-bearing delivery to an
// empty place.
func TestWhereStepReasksOnAnUnreadablePlace(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550009502"
	agreeBagAtList(t, c, buyer)
	tap(t, buyer, "ff_delivery")
	if got := c.last(t, buyer); !strings.Contains(got, "Where should it go?") {
		t.Fatalf("the delivery choice must ask where, got %q", got)
	}
	c.say(t, buyer, "to my house")
	if got := c.last(t, buyer); !strings.Contains(got, "Where should it go?") {
		t.Fatalf("an unreadable place must be re-asked, not settled, got %q", got)
	}
}

// Run-3 record: a stale fulfilment tap was translated
// into an answer for whatever step was currently open.
func TestStaleFulfilmentTapIsSwallowed(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233550009503"
	bindings.put(buyer, "demo")
	sessions.put(sessionKey("demo", buyer), &Session{Stage: "fulfilment", MerchantID: "demo", FulfilStep: "where"})
	if text, handled := routeFulfilmentTap(buyer, "ff_delivery"); !handled || text != "" {
		t.Fatalf("a delivery tap at the where step must be swallowed, got (%q, %v)", text, handled)
	}
	if text, handled := routeFulfilmentTap(buyer, "ff_self"); !handled || text != "" {
		t.Fatalf("a self tap at the where step must be swallowed, got (%q, %v)", text, handled)
	}
}

// Run-3 record: a buyer's receipt confirmation ignored
// the order she named and marked her newest sent order.
func TestConfirmReceivedBindsToTheNamedOrder(t *testing.T) {
	setupChat(t, trustOwner, shopTrust)
	merchant := catalogue.GetMerchant("efua")
	buyer := "233550009504"
	paidOrderFor("efua", "EFUA-1001", buyer, 130)
	paidOrderFor("efua", "EFUA-1002", buyer, 130)
	for _, ref := range []string{"EFUA-1001", "EFUA-1002"} {
		orders.Default.Update(ref, func(o *orders.Order) {
			o.DeliveryState = "sent"
			o.SentAt = nowStamp()
		})
	}
	if !buyerConfirmReceived(buyer, merchant, "efua", "I have received order EFUA-1001, thank you") {
		t.Fatal("the named confirmation must be handled")
	}
	if o := orders.Default.GetOrder("EFUA-1001"); o == nil || o.DeliveryState != "received" {
		t.Fatalf("the named order must stand received, got %+v", o)
	}
	if o := orders.Default.GetOrder("EFUA-1002"); o == nil || o.DeliveryState != "sent" {
		t.Fatalf("the unnamed newest order must stand untouched, got %+v", o)
	}
	// A reference that is not hers changes nothing.
	if !buyerConfirmReceived(buyer, merchant, "efua", "I have received order EFUA-9999") {
		t.Fatal("an unresolvable named report is still answered")
	}
	if o := orders.Default.GetOrder("EFUA-1002"); o == nil || o.DeliveryState != "sent" {
		t.Fatalf("an unresolvable reference must substitute nothing, got %+v", o)
	}
}

// Run-3 record: the unpaid sweep's failed branch flipped
// and messaged from its pass-start snapshot, overwriting
// a concurrent cancellation. The stored record now
// decides: cancelled while the pass verified, the order
// stands cancelled.
func TestSweepFailedBranchRespectsAConcurrentCancellation(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	ref := "TEST-SWEEP-1"
	sess := "PAY-SWEEP-1"
	orders.Default.RecordOrder(ref, orders.Order{
		Reference: ref, Status: "pending", From: "233550009505",
		ProductName: "Handmade Beaded Bag", Price: 850, MerchantID: "demo",
		ProviderRef: &sess, CreatedAt: time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The cancellation lands while the sweep is
		// verifying — the exact window the record names.
		orders.Default.Update(ref, func(o *orders.Order) { o.Status = "cancelled" })
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": "failed"}})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SEEV_SECRET_KEY", "sk_test_sweep")
	t.Setenv("SEEV_API_BASE", srv.URL)

	CheckUnpaidOrders(time.Now())

	if o := orders.Default.GetOrder(ref); o == nil || o.Status != "cancelled" {
		t.Fatalf("an order cancelled mid-pass must stand cancelled, got %+v", o)
	}
}
