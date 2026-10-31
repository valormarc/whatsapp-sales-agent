package seevwebhook

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-4 records, one root: settlement-adjacent decisions
// taken from the caller's pass-start snapshot instead of
// the stored record inside the book's lock. The failed
// branch flipped unconditionally once its snapshot checks
// passed; the sessionless paid road read HeldAlerted from
// the snapshot, so concurrent deliveries both sent the
// held-alert. Both decisions now live inside the locked
// update that claims them.

func TestDuplicateFailedEventsFlipAndTellOnce(t *testing.T) {
	t.Setenv("SEEV_WEBHOOK_SECRET", whSecret)
	t.Setenv("WHATSAPP_TOKEN", "")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	ref := "sess-r4-once"
	seedOrder("TEST-R4F1", &ref)
	body := fmt.Sprintf(`{"reference":%q,"status":"failed","meta":{"order_reference":"TEST-R4F1"}}`, ref)
	ts, sig := sign(body, whSecret)
	for i := 0; i < 3; i++ {
		if code := Handle(body, ts, sig); code != 200 {
			t.Fatalf("delivery %d: got %d", i, code)
		}
	}
	if o := orders.Default.GetOrder("TEST-R4F1"); o == nil || o.Status != "failed" {
		t.Fatalf("the order must be failed exactly once over, got %+v", o)
	}
	// The outbox is shared across this package's tests, so
	// the count is scoped to this test's own reference.
	if n := strings.Count(merchantNotes(t), "TEST-R4F1"); n != 1 {
		t.Fatalf("the merchant must hear of the failure once, got %d notices", n)
	}
}

func TestConcurrentSessionlessPaidEventsSendOneHeldAlert(t *testing.T) {
	t.Setenv("SEEV_WEBHOOK_SECRET", whSecret)
	t.Setenv("WHATSAPP_TOKEN", "")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	// Checkout must be enabled for the paid road to reach
	// the sessionless hold; the seed harness's other tests
	// set the Seev key the same way.
	t.Setenv("SEEV_SECRET_KEY", "sk_test_r4")
	seedOrder("TEST-R4H1", nil)
	body := paidEvent("seev-r4h1", "TEST-R4H1", 100)
	ts, sig := sign(body, whSecret)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Handle(body, ts, sig)
		}()
	}
	wg.Wait()
	if n := strings.Count(merchantNotes(t), "TEST-R4H1"); n != 1 {
		t.Fatalf("concurrent deliveries must send the held-alert once, got %d", n)
	}
	if o := orders.Default.GetOrder("TEST-R4H1"); o == nil || o.Status == "paid" {
		t.Fatalf("a sessionless order must never settle on the event alone, got %+v", o)
	}
}
