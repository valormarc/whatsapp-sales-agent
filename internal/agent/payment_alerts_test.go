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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/seevwebhook"
)

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
	t.Setenv("SEEV_SECRET_KEY", "")
	t.Setenv("SEEV_WEBHOOK_SECRET", "")
	buyer := "233550008101"
	c, _, placed := buyTilapiaOnAma(t, buyer)
	ref := placed["reference"].(string)
	agreed := int(placed["total"].(float64))

	body := `{"data":{"reference":"SEEVSIM9","status":"completed","amount":` + strconv.Itoa(agreed) + `},"meta":{"order_reference":"` + ref + `"}}`
	if status := seevwebhook.Handle(body, "", ""); status != 200 {
		t.Fatalf("webhook status %d, want 200", status)
	}
	notes := c.replies(t, ownerAma)
	if len(notes) != 1 {
		t.Fatalf("merchant payment alerts = %v, want the one success alert", notes)
	}
	note := notes[0]
	for _, want := range []string{
		"Payment received\n",
		"*Smoked Tilapia*",
		"paid in full by the buyer.",
		"Seev fee GH₵",
		"settles to your MoMo tomorrow.",
		"\nRef: " + ref,
		"\nBuyer: " + buyer,
	} {
		if !strings.Contains(note, want) {
			t.Fatalf("success alert missing %q:\n%s", want, note)
		}
	}
}

func TestLinkFailureAlertsBothSides(t *testing.T) {
	t.Setenv("SEEV_SECRET_KEY", "")
	t.Setenv("SEEV_WEBHOOK_SECRET", "")
	buyer := "233550008102"
	c, _, placed := buyTilapiaOnAma(t, buyer)
	ref := placed["reference"].(string)
	agreed := int(placed["total"].(float64))

	body := `{"data":{"reference":"SEEVSIM9","status":"failed","amount":` + strconv.Itoa(agreed) + `},"meta":{"order_reference":"` + ref + `"}}`
	if status := seevwebhook.Handle(body, "", ""); status != 200 {
		t.Fatalf("webhook status %d, want 200", status)
	}
	if got := c.last(t, buyer); !strings.Contains(got, "did not go through") {
		t.Fatalf("buyer failure alert = %q", got)
	}
	notes := c.replies(t, ownerAma)
	if len(notes) != 1 || !strings.Contains(notes[0], "Payment failed\n") ||
		!strings.Contains(notes[0], "*Smoked Tilapia*") || !strings.Contains(notes[0], "Nothing was charged.") {
		t.Fatalf("merchant failure alert = %v", notes)
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
	if len(buyerNotes) != 1 || !strings.Contains(buyerNotes[0], "Payment received, thank you") {
		t.Fatalf("buyer notes = %v", buyerNotes)
	}
	merchantNotes := c.replies(t, merchant)
	if len(merchantNotes) != 1 || !strings.Contains(merchantNotes[0], "Payment received") {
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
