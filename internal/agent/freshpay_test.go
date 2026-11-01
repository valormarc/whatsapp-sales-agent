package agent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

// The fresh payment watch: a payment that lands without
// its webhook must still confirm itself, within the
// minute, while the buyer is at the counter — not when
// she comes back and reports it, and not at the unpaid
// watch's thirty minutes.
func TestFreshPaymentWatchSettlesAFreshPaidOrder(t *testing.T) {
	merchant := "233550008001"
	dir := setupChat(t, merchant, shopWithBag)
	t.Setenv("SEEV_SECRET_KEY", "sk_test_fake")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		fmt.Fprint(w, `{"data":{"status":"completed","amount":85000}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SEEV_API_BASE", srv.URL)
	c := &chat{dir: dir}
	now := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	ref := "PAY-FRESH-1"
	orders.Default.RecordOrder("TEST-FRESH-1", orders.Order{
		From: "233550008002", Status: "pending", MerchantID: MerchantID,
		CreatedAt:   now.Add(-2 * time.Minute).Format(time.RFC3339),
		ProductName: "Handmade Beaded Bag", Price: 850, Total: 850,
		ProviderRef: &ref, PaymentPath: "seev",
	})
	oldRef := "PAY-FRESH-OLD"
	orders.Default.RecordOrder("TEST-FRESH-OLD", orders.Order{
		From: "233550008002", Status: "pending", MerchantID: MerchantID,
		CreatedAt:   now.Add(-2 * time.Hour).Format(time.RFC3339),
		ProductName: "Handmade Beaded Bag", Price: 850, Total: 850,
		ProviderRef: &oldRef, PaymentPath: "seev",
	})

	CheckFreshPayments(now)

	if o := orders.Default.GetOrder("TEST-FRESH-1"); o == nil || o.Status != "paid" {
		t.Fatalf("a fresh order Seev reports paid must settle at once, got %+v", o)
	}
	joined := strings.Join(c.replies(t, "233550008002"), "\n")
	if !strings.Contains(joined, "Payment confirmed") {
		t.Fatalf("the buyer must be told without speaking first, got %q", joined)
	}
	if o := orders.Default.GetOrder("TEST-FRESH-OLD"); o == nil || o.Status != "pending" {
		t.Fatalf("an order past the fresh window belongs to the unpaid watch, not this pass, got %+v", o)
	}
}

func TestFreshPaymentWatchHoldsWhenTheAmountDiffers(t *testing.T) {
	merchant := "233550008011"
	setupChat(t, merchant, shopWithBag)
	t.Setenv("SEEV_SECRET_KEY", "sk_test_fake")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		fmt.Fprint(w, `{"data":{"status":"completed","amount":100}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SEEV_API_BASE", srv.URL)
	now := time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC)
	ref := "PAY-FRESH-2"
	orders.Default.RecordOrder("TEST-FRESH-2", orders.Order{
		From: "233550008012", Status: "pending", MerchantID: MerchantID,
		CreatedAt:   now.Add(-time.Minute).Format(time.RFC3339),
		ProductName: "Handmade Beaded Bag", Price: 850, Total: 850,
		ProviderRef: &ref, PaymentPath: "seev",
	})

	CheckFreshPayments(now)

	if o := orders.Default.GetOrder("TEST-FRESH-2"); o == nil || o.Status != "pending" {
		t.Fatalf("a paid read-back in the wrong amount settles nothing on this pass, got %+v", o)
	}
}
