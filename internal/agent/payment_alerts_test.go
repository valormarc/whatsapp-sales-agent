package agent

// Payment outcomes reach the people they belong to. A link
// payment that lands tells the merchant, in the same laid-out
// lines as her sale notice, with the Seev fee and what settles
// to her MoMo. A payment that fails tells both sides. And a
// payment that is simply never made, the abandoned page Seev
// never reports, is surfaced by the unpaid watch once the grace
// period passes: the buyer is reminded, the merchant is told,
// and neither is nagged twice.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/seevwebhook"
)

// fakeSeevMintAndReadback stands Seev in whole for a
// settlement test: link creation answers with a checkout
// URL and its session reference, and the session read-back
// reports that session completed at the very amount it was
// minted for. The success-alert test used to settle on the
// webhook event's word alone, against an order whose mint
// had never produced a session — the exact road the
// second audit closed — so the test now walks the real
// road: mint, session, read-back, settle.
func fakeSeevMintAndReadback(t *testing.T) {
	t.Helper()
	amounts := map[string]float64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/developer/payments" {
			var body struct {
				Amount float64 `json:"amount"`
				Meta   struct {
					OrderReference string `json:"order_reference"`
				} `json:"meta"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			ref := "PAY-" + body.Meta.OrderReference
			amounts[ref] = body.Amount
			fmt.Fprintf(w, `{"data":{"checkout_url":"https://pay.seevplus.test/x","reference":%q}}`, ref)
			return
		}
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/developer/payments/") {
			ref := strings.TrimPrefix(r.URL.Path, "/api/v1/developer/payments/")
			if amt, ok := amounts[ref]; ok {
				fmt.Fprintf(w, `{"data":{"status":"completed","amount":%v}}`, amt)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SEEV_API_BASE", srv.URL)
}

// buyTilapiaOnAma drives one link-path purchase on Ama's shop
// and returns the chat, the buyer and the placed order as read
// from disk, with both sides' inboxes drained.
func buyTilapiaOnAma(t *testing.T, buyer string) (*chat, string, map[string]any) {
	t.Helper()
	dir, c := setupTwoShops(t)
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
	c.replies(t, buyer)
	placed := ordersOnDisk(t, dir)[0]
	c.replies(t, ownerAma)
	return c, dir, placed
}

func TestLinkSuccessAlertsTheMerchantInLines(t *testing.T) {
	t.Setenv("SEEV_SECRET_KEY", "sk_test_alerts")
	t.Setenv("SEEV_WEBHOOK_SECRET", "whsec_alerts")
	fakeSeevMintAndReadback(t)
	buyer := "233550008101"
	c, _, placed := buyTilapiaOnAma(t, buyer)
	ref := placed["reference"].(string)
	agreed := int(placed["total"].(float64))

	body := `{"data":{"reference":"SEEVSIM9","status":"completed","amount":` + strconv.Itoa(agreed) + `},"meta":{"order_reference":"` + ref + `"}}`
	ts, sig := signSeevWebhook(t, body)
	if status := seevwebhook.Handle(body, ts, sig); status != 200 {
		t.Fatalf("webhook status %d, want 200", status)
	}
	notes := c.replies(t, ownerAma)
	if len(notes) != 1 {
		t.Fatalf("merchant payment alerts = %v, want the one success alert", notes)
	}
	note := notes[0]
	for _, want := range []string{
		"New order " + ref,
		"Item: Smoked Tilapia",
		"Agreed price: GH₵",
		"Total paid: GH₵",
		"Payment: confirmed",
		"Seev fee GH₵",
		"settles to your MoMo tomorrow.",
		"\nBuyer: " + buyer,
	} {
		if !strings.Contains(note, want) {
			t.Fatalf("success alert missing %q:\n%s", want, note)
		}
	}
}

// Run-3 overturned the posture this test used to assert
// (it was TestLinkFailureAlertsBothSides): the purchase's
// mint never produced a session, so the order stands
// link_failed with no ProviderRef, and the failed event
// that arrives naming some session cannot be bound to
// this order at all. The third audit confirmed that
// flipping and alerting on such an event is the defect —
// it restocked and closed orders on events that cannot
// be shown to be about them (the same shape as an
// anonymised order's struck session). A sessionless
// order's failed event is now a no-op: acknowledged, the
// order untouched, and neither side alerted — the buyer
// and merchant were already told the truth at sale time,
// when the link failed to come up.
func TestFailedEventForASessionlessOrderAlertsNobody(t *testing.T) {
	t.Setenv("SEEV_SECRET_KEY", "sk_test_alerts")
	t.Setenv("SEEV_WEBHOOK_SECRET", "whsec_alerts")
	buyer := "233550008102"
	c, _, placed := buyTilapiaOnAma(t, buyer)
	ref := placed["reference"].(string)
	agreed := int(placed["total"].(float64))

	body := `{"data":{"reference":"SEEVSIM9","status":"failed","amount":` + strconv.Itoa(agreed) + `},"meta":{"order_reference":"` + ref + `"}}`
	ts, sig := signSeevWebhook(t, body)
	if status := seevwebhook.Handle(body, ts, sig); status != 200 {
		t.Fatalf("webhook status %d, want 200", status)
	}
	if o := orders.Default.GetOrder(ref); o == nil || o.Status != "link_failed" {
		t.Fatalf("the sessionless order must stand link_failed, untouched, got %+v", o)
	}
	for _, got := range c.replies(t, buyer) {
		if strings.Contains(got, "did not go through") {
			t.Fatalf("no buyer failure alert may ride an unbindable event, got %q", got)
		}
	}
	notes := c.replies(t, ownerAma)
	if len(notes) != 0 {
		t.Fatalf("no merchant alert may ride an unbindable event, got %v", notes)
	}
}

func TestUnpaidWatchAlertsOnceAndOnlyWhenDue(t *testing.T) {
	merchant := "233550001701"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	ref := "PAY-WATCH-1"
	mk := func(reference, from, status, createdAt string, provider *string) {
		orders.Default.RecordOrder(reference, orders.Order{
			From: from, Status: status, CreatedAt: createdAt,
			ProductName: "Handmade Beaded Bag", Price: 850, Total: 850,
			ProviderRef: provider, MerchantID: MerchantID,
		})
	}
	mk("TEST-1001", "233550001702", "pending", now.Add(-2*time.Hour).Format(time.RFC3339), &ref)
	mk("TEST-1002", "233550001703", "pending", now.Format(time.RFC3339), &ref)
	mk("TEST-1003", "233550001704", "paid", now.Add(-2*time.Hour).Format(time.RFC3339), &ref)
	mk("TEST-1004", "233550001705", "pending", "", &ref)

	CheckUnpaidOrders(now)

	buyerNotes := c.replies(t, "233550001702")
	if len(buyerNotes) != 1 || !strings.Contains(buyerNotes[0], "has not come through yet") || !strings.Contains(buyerNotes[0], "TEST-1001") {
		t.Fatalf("the overdue buyer should be reminded once, got %v", buyerNotes)
	}
	merchantNotes := c.replies(t, merchant)
	if len(merchantNotes) != 1 || !strings.Contains(merchantNotes[0], "Payment not received yet") ||
		!strings.Contains(merchantNotes[0], "*Handmade Beaded Bag*") || !strings.Contains(merchantNotes[0], "TEST-1001") {
		t.Fatalf("the merchant should be alerted once, got %v", merchantNotes)
	}
	for _, other := range []string{"233550001703", "233550001704", "233550001705"} {
		if got := c.replies(t, other); len(got) != 0 {
			t.Fatalf("buyer %s should have heard nothing, got %v", other, got)
		}
	}
	// The legacy order was stamped, not alerted on sight.
	legacy := orders.Default.GetOrder("TEST-1004")
	if legacy == nil || legacy.CreatedAt == "" || legacy.UnpaidAlerted {
		t.Fatalf("legacy order = %+v, want it stamped and unalerted", legacy)
	}

	// A second pass an hour later never repeats the first
	// alert. By then the other two pending orders have also
	// been waiting past the grace: the fresh one has aged, and
	// the legacy one was stamped on the first pass. They alone
	// come due, and nobody hears about TEST-1001 again.
	CheckUnpaidOrders(now.Add(time.Hour))
	if got := c.replies(t, "233550001702"); len(got) != 0 {
		t.Fatalf("the buyer was nagged twice: %v", got)
	}
	for buyer, ref := range map[string]string{"233550001703": "TEST-1002", "233550001705": "TEST-1004"} {
		got := c.replies(t, buyer)
		if len(got) != 1 || !strings.Contains(got[0], ref) {
			t.Fatalf("buyer %s should be reminded about %s, got %v", buyer, ref, got)
		}
	}
	merchantNotes2 := c.replies(t, merchant)
	joined := strings.Join(merchantNotes2, "\n")
	if len(merchantNotes2) != 2 || !strings.Contains(joined, "TEST-1002") || !strings.Contains(joined, "TEST-1004") || strings.Contains(joined, "TEST-1001") {
		t.Fatalf("the merchant's second alerts should cover the two newly due orders only, got %v", merchantNotes2)
	}
}

func TestUnpaidWatchHealsALandedPaymentInsteadOfReminding(t *testing.T) {
	merchant := "233550001801"
	dir := setupChat(t, merchant, shopWithBag)
	t.Setenv("SEEV_SECRET_KEY", "sk_test_fake")
	fakeSeevReadback(t, "completed", 87500)
	c := &chat{dir: dir}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	ref := "PAY-WATCH-HEAL"
	orders.Default.RecordOrder("TEST-2001", orders.Order{
		From: "233550001802", Status: "pending", MerchantID: MerchantID,
		CreatedAt:   now.Add(-2 * time.Hour).Format(time.RFC3339),
		ProductName: "Handmade Beaded Bag", Price: 875, Total: 875,
		ProviderRef: &ref, PaymentPath: "seev",
	})

	CheckUnpaidOrders(now)

	// The payment had landed; only its news was missing. The
	// watch settles it the way any verified payment settles,
	// and nobody is told the money never came.
	if got := orders.Default.GetOrder("TEST-2001"); got == nil || got.Status != "paid" {
		t.Fatalf("order = %+v, want paid", got)
	}
	buyerNotes := c.replies(t, "233550001802")
	if len(buyerNotes) != 1 || !strings.Contains(buyerNotes[0], "Payment confirmed") || !strings.Contains(buyerNotes[0], "Amount: GH\u20b5875") || !strings.Contains(buyerNotes[0], "Thank you for buying from") {
		t.Fatalf("buyer notes = %v", buyerNotes)
	}
	merchantNotes := c.replies(t, merchant)
	if len(merchantNotes) != 1 || !strings.Contains(merchantNotes[0], "Payment: confirmed") {
		t.Fatalf("merchant notes = %v", merchantNotes)
	}
	for _, notes := range [][]string{buyerNotes, merchantNotes} {
		for _, n := range notes {
			if strings.Contains(n, "has not come through") || strings.Contains(n, "not received yet") {
				t.Fatalf("a landed payment must never draw the unpaid alert: %q", n)
			}
		}
	}
}
