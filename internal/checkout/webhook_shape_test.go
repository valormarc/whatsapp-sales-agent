package checkout

import "testing"

// Seev's real webhook deliveries nest the payment inside
// data.transaction, and the order reference inside the
// transaction's metadata under the developer key — the
// shape a live delivery on 2026-11-01 carried, captured
// from the provider's own dashboard. The parser was
// written against the flat shape, so every real event
// read empty at the level it checked, the handler
// answered 200, and nothing ever settled by webhook.
const realSeevDelivery = `{
  "event": "payment.succeeded",
  "createdAt": "2026-11-01T05:59:50Z",
  "env": "sandbox",
  "data": {
    "transaction": {
      "id": "6ae6d52af8715d9c3c2d2255",
      "reference": "PAY-20261101-aaaa",
      "status": "completed",
      "amount": 245,
      "currency": "GHS",
      "type": "payment",
      "metadata": {
        "developer": {"order_reference": "LUXE-0007-4E8"},
        "origin": "developer_api",
        "sandbox": true,
        "selectedStatus": "success",
        "sessionStatus": "pending",
        "sessionType": "checkout"
      }
    }
  }
}`

func TestParseWebhookEventReadsSeevsRealShape(t *testing.T) {
	ev := ParseWebhookEvent([]byte(realSeevDelivery))
	if ev.Reference == nil || *ev.Reference != "PAY-20261101-aaaa" {
		t.Fatalf("the session reference lives in data.transaction: %+v", ev.Reference)
	}
	if ev.Status == nil || *ev.Status != "completed" {
		t.Fatalf("the status lives in data.transaction: %+v", ev.Status)
	}
	if ev.OrderReference == nil || *ev.OrderReference != "LUXE-0007-4E8" {
		t.Fatalf("the order reference lives in transaction.metadata.developer: %+v", ev.OrderReference)
	}
	if ev.AmountMajor == nil || *ev.AmountMajor != 245 {
		t.Fatalf("the amount lives in data.transaction, in major units: %+v", ev.AmountMajor)
	}
}

func TestParseWebhookEventStillReadsTheFlatShape(t *testing.T) {
	ev := ParseWebhookEvent([]byte(`{"reference":"seev-x","status":"completed","amount":100,"meta":{"order_reference":"TEST-1"}}`))
	if ev.Reference == nil || *ev.Reference != "seev-x" || ev.Status == nil || *ev.Status != "completed" ||
		ev.OrderReference == nil || *ev.OrderReference != "TEST-1" || ev.AmountMajor == nil || *ev.AmountMajor != 100 {
		t.Fatalf("the flat shape must keep parsing: %+v", ev)
	}
}
