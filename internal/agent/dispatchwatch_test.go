package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

// The dispatch watch pushes a paid order that has waited
// too long to go out to its merchant, once: the order is
// work Stall pursues, not a line that waits for her to ask.
// Too fresh, already sent, disputed (frozen work, not
// dispatch work) and unpaid orders are never pushed.
func TestDispatchWatchPushesAStalePaidOrderOnce(t *testing.T) {
	merchant := "233550007001"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	now := time.Date(2026, 11, 1, 15, 0, 0, 0, time.UTC)
	seed := func(ref string, o orders.Order) {
		o.MerchantID = MerchantID
		o.ProductName = "Handmade Beaded Bag"
		o.Price = 850
		o.Total = 850
		orders.Default.RecordOrder(ref, o)
	}
	seed("TEST-DISP-STALE", orders.Order{From: "233550007002", Status: "paid",
		CreatedAt: now.Add(-5 * time.Hour).Format(time.RFC3339), Location: "Osu"})
	seed("TEST-DISP-FRESH", orders.Order{From: "233550007003", Status: "paid",
		CreatedAt: now.Add(-1 * time.Hour).Format(time.RFC3339)})
	seed("TEST-DISP-SENT", orders.Order{From: "233550007004", Status: "paid", DeliveryState: "sent",
		CreatedAt: now.Add(-6 * time.Hour).Format(time.RFC3339)})
	seed("TEST-DISP-DISPUTED", orders.Order{From: "233550007005", Status: "paid", DisputeState: "open",
		CreatedAt: now.Add(-6 * time.Hour).Format(time.RFC3339)})
	seed("TEST-DISP-UNPAID", orders.Order{From: "233550007006", Status: "pending",
		CreatedAt: now.Add(-6 * time.Hour).Format(time.RFC3339)})

	CheckDispatchNudges(now)

	joined := strings.Join(c.replies(t, merchant), "\n")
	if !strings.Contains(joined, "TEST-DISP-STALE") || !strings.Contains(joined, "still waiting to go out") {
		t.Fatalf("the stale paid order must be pushed to the merchant, got %q", joined)
	}
	for _, ref := range []string{"TEST-DISP-FRESH", "TEST-DISP-SENT", "TEST-DISP-DISPUTED", "TEST-DISP-UNPAID"} {
		if strings.Contains(joined, ref) {
			t.Fatalf("%s must never be pushed, got %q", ref, joined)
		}
	}
	if o := orders.Default.GetOrder("TEST-DISP-STALE"); o == nil || !o.DispatchNudged {
		t.Fatalf("the pushed order is marked so it is never pushed twice: %+v", o)
	}

	before := len(c.replies(t, merchant))
	CheckDispatchNudges(now.Add(time.Hour))
	if after := len(c.replies(t, merchant)); after != before {
		t.Fatalf("the push happens once and never nags: %d messages before, %d after", before, after)
	}
}
