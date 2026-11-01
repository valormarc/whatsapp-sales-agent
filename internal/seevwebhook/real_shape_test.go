package seevwebhook

import (
	"fmt"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// A webhook in Seev's real delivery shape — the payment
// nested in data.transaction, the order reference in the
// transaction metadata's developer key — settles its
// order. This is the delivery the live agent received and
// answered 200 on without settling, because the parser
// read the fields one level above where Seev puts them.
func TestHandleSettlesARealShapedDelivery(t *testing.T) {
	t.Setenv("SEEV_WEBHOOK_SECRET", whSecret)
	t.Setenv("SEEV_SECRET_KEY", "test-seev-key")
	t.Setenv("WHATSAPP_TOKEN", "")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	session := "PAY-REAL-1"
	seedOrder("TEST-REAL-1", &session)
	restore := verifyPaymentFn
	amt := 100.0
	verifyPaymentFn = func(string) *checkout.Verification {
		return &checkout.Verification{Status: "completed", AmountMajor: &amt}
	}
	defer func() { verifyPaymentFn = restore }()

	body := fmt.Sprintf(`{"event":"payment.succeeded","createdAt":"2026-11-01T05:59:50Z","env":"sandbox","data":{"transaction":{"reference":%q,"status":"completed","amount":100,"currency":"GHS","type":"payment","metadata":{"developer":{"order_reference":"TEST-REAL-1"}}}}}`, session)
	ts, sig := sign(body, whSecret)
	if code := Handle(body, ts, sig); code != 200 {
		t.Fatalf("a verified real-shaped delivery is acknowledged, got %d", code)
	}
	if o := orders.Default.GetOrder("TEST-REAL-1"); o == nil || o.Status != "paid" {
		t.Fatalf("a real-shaped paid delivery settles its order, got %+v", o)
	}
}
