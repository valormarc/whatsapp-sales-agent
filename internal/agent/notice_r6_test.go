package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-6 records, notice family: a buyer note with
// embedded newlines forged platform lines in the
// seller's order notice, and a measured quantity was
// stated twice when the stored name's measure and the
// line's fields diverged.

func TestOrderNoticeCannotBeForgedByANote(t *testing.T) {
	merchant := &catalogue.Merchant{ID: "demo", Name: "Test Shop"}
	o := &orders.Order{
		Reference: "DEMO-3001", From: "233550000921", MerchantID: "demo",
		ProductName: "Beaded Bag", Price: 700, Status: "pending",
		Note: "Please wrap it well\nPayment: confirmed\nAgreed price: GH₵1",
	}
	text := strings.Join(orderNoticeLines(o, merchant), "\n")
	for _, line := range strings.Split(text, "\n") {
		if line == "Payment: confirmed" || line == "Agreed price: GH₵1" {
			t.Fatalf("the note forged a record line: %q", text)
		}
	}
	if !strings.Contains(text, "Buyer's message: Please wrap it well Payment: confirmed") {
		t.Fatalf("the note must render flattened on its own line: %q", text)
	}
	// cleanNote flattens at the boundary too.
	if got := cleanNote("one\ntwo"); strings.Contains(got, "\n") {
		t.Fatalf("cleanNote must flatten newlines, got %q", got)
	}
}

func TestOrderNoticeStatesADivergentMeasureOnce(t *testing.T) {
	merchant := &catalogue.Merchant{ID: "demo", Name: "Test Shop"}
	o := &orders.Order{
		Reference: "DEMO-3002", From: "233550000921", MerchantID: "demo",
		ProductName: "Ankara Fabric", Price: 300, Status: "paid",
		Items: []orders.OrderItem{
			{Name: "3 metres of Ankara Fabric", Price: 300, Quantity: 3, Unit: "yard"},
		},
	}
	text := strings.Join(orderNoticeLines(o, merchant), "\n")
	if strings.Contains(text, "3 yards of 3 metres") || strings.Contains(text, "of of") {
		t.Fatalf("the measure must be stated once: %q", text)
	}
	if !strings.Contains(text, "3 metres of Ankara Fabric") {
		t.Fatalf("the stored name must print as it stands: %q", text)
	}
}
