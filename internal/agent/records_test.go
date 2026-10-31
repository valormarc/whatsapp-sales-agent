package agent

// Clean records: the four money messages print on fixed
// paper. These tests pin the exact shapes from the demo
// the seller approved: the quote, the total (both purses),
// and the seller's notice in its fixed order.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

func TestQuoteRecordCarriesTheMeasure(t *testing.T) {
	fabric := &catalogue.Product{Name: "Ankara fabric", ListPrice: 450, Unit: "yard", BaseQty: 6}
	if got, want := quoteRecord(fabric), "Ankara fabric, 6 yards - GH₵450"; got != want {
		t.Fatalf("quote = %q, want %q", got, want)
	}
	plain := &catalogue.Product{Name: "Ankle Strap Heels", ListPrice: 120}
	if got, want := quoteRecord(plain), "Ankle Strap Heels - GH₵120"; got != want {
		t.Fatalf("quote = %q, want %q", got, want)
	}
}

func TestTotalRecordMatchesTheDemoShape(t *testing.T) {
	items := []orders.OrderItem{{Name: "Ankara fabric", Price: 380}}
	got := totalRecord(items, 25, 25, "delivery", "Osu", "merchant", "")
	want := "Ankara fabric - GH₵380\nDelivery to Osu: GH₵25\nTotal: GH₵405"
	if got != want {
		t.Fatalf("total = %q, want %q", got, want)
	}
}

func TestTotalRecordCarrierKeepsThePursesApart(t *testing.T) {
	items := []orders.OrderItem{{Name: "Ankle Strap Heels", Price: 110}}
	got := totalRecord(items, 25, 0, "delivery", "Osu", "carrier", "Sent by bus")
	for _, want := range []string{"Delivery to Osu: GH₵25 (paid to the station on arrival)", "Total: GH₵110", "That is GH₵135 in all, with the delivery."} {
		if !strings.Contains(got, want) {
			t.Fatalf("carrier total = %q, want %q on it", got, want)
		}
	}
}

func TestSellerNoticeMatchesTheDemoOrder(t *testing.T) {
	o := &orders.Order{
		Reference: "EFUA-0007", From: "233550001111", ProductName: "Ankara fabric",
		Price: 380, Total: 405, Fulfilment: "delivery", Location: "Osu, Accra",
		DeliveryFee: 25, DeliveryPaidTo: "merchant", Status: "paid",
	}
	merchant := &catalogue.Merchant{Name: "Efua's Store", DeliveryFee: 25}
	lines := orderNoticeLines(o, merchant)
	lines = append(lines, "Total paid: "+strings.TrimPrefix("GH₵405", ""))
	joined := strings.Join(lines, "\n")
	order := []string{"New order EFUA-0007", "Item: Ankara fabric", "Agreed price: GH₵380", "Delivery:", "Total paid: GH₵405"}
	at := -1
	for _, want := range order {
		i := strings.Index(joined, want)
		if i < 0 || i < at {
			t.Fatalf("notice lines out of order or missing %q:\n%s", want, joined)
		}
		at = i
	}
}

func TestCompletionSendsATapToPayButton(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550009777"
	agreeBagAtList(t, c, buyer)
	c.say(t, buyer, "Delivery please, to Koforidua")
	c.replies(t, buyer)
	c.finishDetails(t, buyer)

	data, err := os.ReadFile(filepath.Join(dir, "data", "outbox.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec outboxRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.To == buyer && rec.Kind == "cta_url" {
			found = true
			if len(rec.Buttons) != 1 || rec.Buttons[0] != "Click to pay GH₵875" {
				t.Fatalf("pay button = %v, want the total on its face", rec.Buttons)
			}
			if rec.URL == "" || !strings.Contains(rec.Body, "Pay here to complete your order.") {
				t.Fatalf("pay button record = %+v", rec)
			}
		}
		if rec.To == buyer && strings.Contains(rec.Body, "pay.seevplus.com") && rec.Kind != "cta_url" {
			t.Fatalf("a raw payment link still reaches the buyer as text: %q", rec.Body)
		}
	}
	if !found {
		t.Fatal("no tap-to-pay button was sent for the completed sale")
	}
}
