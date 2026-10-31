package seevwebhook

import (
	"fmt"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-3 record: a signed failed event for a sessionless
// OPEN order passed the old conditional guard vacuously
// and flipped the order to failed, restocking it. With no
// recorded session, no failed event can bind: a no-op.
func TestFailedEventForASessionlessOpenOrderIsANoOp(t *testing.T) {
	t.Setenv("SEEV_WEBHOOK_SECRET", "whsec_test")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	seedOrder("TEST-R3F1", nil) // no ProviderRef: sessionless

	body := fmt.Sprintf(`{"reference":%q,"status":"failed","meta":{"order_reference":"TEST-R3F1"}}`, "sess-other")
	ts, sig := sign(body, "whsec_test")
	if code := Handle(body, ts, sig); code != 200 {
		t.Fatalf("the event is still answered 200, got %d", code)
	}
	if o := orders.Default.GetOrder("TEST-R3F1"); o == nil || o.Status != "pending" {
		t.Fatalf("a sessionless order must stand untouched by a failed event, got %+v", o)
	}
}

// Run-3 record: a payment completed on a superseded link
// session was silently dropped — the paid road only ever
// read back the order's CURRENT session. The event's own
// session is now verified on the same terms and settles.
func TestPaidEventOnASupersededSessionSettlesAfterVerification(t *testing.T) {
	t.Setenv("SEEV_WEBHOOK_SECRET", "whsec_test")
	t.Setenv("SEEV_SECRET_KEY", "test-seev-key")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	newSession := "sess-new-r3"
	seedOrder("TEST-R3S2", &newSession)

	restore := verifyPaymentFn
	verified := ""
	verifyPaymentFn = func(ref string) *checkout.Verification {
		verified = ref
		// The recorded (new) session has not been paid —
		// the buyer never used the fresh link. The money
		// sits on the superseded session the event names.
		if ref != "sess-old-r3" {
			return &checkout.Verification{Status: "pending"}
		}
		amt := 100.0
		return &checkout.Verification{Status: "completed", AmountMajor: &amt}
	}
	defer func() { verifyPaymentFn = restore }()

	body := paidEvent("sess-old-r3", "TEST-R3S2", 100)
	ts, sig := sign(body, "whsec_test")
	if code := Handle(body, ts, sig); code != 200 {
		t.Fatalf("the event is answered 200, got %d", code)
	}
	if verified != "sess-old-r3" {
		t.Fatalf("the event's own session must be the one verified, got %q", verified)
	}
	if o := orders.Default.GetOrder("TEST-R3S2"); o == nil || o.Status != "paid" {
		t.Fatalf("a verified payment on the superseded session must settle, got %+v", o)
	}
}

// The same road, unverifiable: the superseded session's
// read-back does not show the charged amount paid, so
// nothing settles and the order stands open.
func TestPaidEventOnASupersededSessionUnverifiedIsHeld(t *testing.T) {
	t.Setenv("SEEV_WEBHOOK_SECRET", "whsec_test")
	t.Setenv("SEEV_SECRET_KEY", "test-seev-key")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	newSession := "sess-new-r3b"
	seedOrder("TEST-R3S3", &newSession)

	restore := verifyPaymentFn
	verifyPaymentFn = func(ref string) *checkout.Verification {
		if ref == "sess-old-r3b" {
			amt := 40.0 // paid something, not this order's amount
			return &checkout.Verification{Status: "completed", AmountMajor: &amt}
		}
		return &checkout.Verification{Status: "pending"}
	}
	defer func() { verifyPaymentFn = restore }()

	body := paidEvent("sess-old-r3b", "TEST-R3S3", 100)
	ts, sig := sign(body, "whsec_test")
	if code := Handle(body, ts, sig); code != 200 {
		t.Fatalf("the event is answered 200, got %d", code)
	}
	if o := orders.Default.GetOrder("TEST-R3S3"); o == nil || o.Status != "pending" {
		t.Fatalf("an unverifiable superseded-session event must settle nothing, got %+v", o)
	}
}
