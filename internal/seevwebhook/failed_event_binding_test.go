package seevwebhook

import (
	"fmt"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Regression for the audit lead that a failed event was not
// bound to the order's own session: an order whose link was
// retried holds a live session, and the superseded session's
// later failed event — a completely normal Seev delivery —
// flipped the order to failed and told both parties a false
// failure story. A failed event now acts only on the order
// whose session it names.
func TestFailedEventMustNameTheOrdersOwnSession(t *testing.T) {
	t.Setenv("SEEV_WEBHOOK_SECRET", "whsec_test")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	live := "sess-live"
	seedOrder("TEST-BIND-1", &live)

	failedEvent := func(ref string) string {
		return fmt.Sprintf(`{"reference":%q,"status":"failed","meta":{"order_reference":"TEST-BIND-1"}}`, ref)
	}
	body := failedEvent("sess-superseded")
	ts, sig := sign(body, "whsec_test")
	if code := Handle(body, ts, sig); code != 200 {
		t.Fatalf("a foreign failed event is still answered 200, got %d", code)
	}
	if o := orders.Default.GetOrder("TEST-BIND-1"); o == nil || o.Status != "pending" {
		t.Fatalf("a failed event for a superseded session must not flip the order, got %+v", o)
	}

	body = failedEvent("sess-live")
	ts, sig = sign(body, "whsec_test")
	if code := Handle(body, ts, sig); code != 200 {
		t.Fatalf("the order's own failed event is answered 200, got %d", code)
	}
	if o := orders.Default.GetOrder("TEST-BIND-1"); o == nil || o.Status != "failed" {
		t.Fatalf("a failed event for the order's own session flips it, got %+v", o)
	}
}
