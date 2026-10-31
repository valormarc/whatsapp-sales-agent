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

// Run-2 regression (medium): a paid read-back that states
// no amount — a contractually expected shape — fell into
// the sweep's mismatch branch, which dereferenced the nil
// amount and panicked the watch goroutine; the process
// then crash-looped on the same order after every restart.
// The sweep now holds the order, tells the merchant in the
// amount-absent words, and moves on.
func TestUnpaidWatchSurvivesAnAmountlessPaidReadback(t *testing.T) {
	merchant := "233550002001"
	dir := setupChat(t, merchant, shopWithBag)
	t.Setenv("SEEV_SECRET_KEY", "sk_test_fake")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		fmt.Fprint(w, `{"data":{"status":"completed"}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SEEV_API_BASE", srv.URL)
	c := &chat{dir: dir}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	ref := "PAY-WATCH-NIL"
	orders.Default.RecordOrder("TEST-NIL-1", orders.Order{
		From: "233550002002", Status: "pending", MerchantID: MerchantID,
		CreatedAt:   now.Add(-2 * time.Hour).Format(time.RFC3339),
		ProductName: "Handmade Beaded Bag", Price: 850, Total: 850,
		ProviderRef: &ref, PaymentPath: "seev",
	})

	CheckUnpaidOrders(now) // must not panic

	o := orders.Default.GetOrder("TEST-NIL-1")
	if o == nil || o.Status != "pending" {
		t.Fatalf("an amountless paid read-back settles nothing, got %+v", o)
	}
	if !o.UnpaidAlerted {
		t.Fatal("the order must be marked surfaced so the next pass does not repeat the notice")
	}
	notes := c.replies(t, merchant)
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "states no amount") || !strings.Contains(joined, "TEST-NIL-1") {
		t.Fatalf("the merchant must be told the amount was absent, got %v", notes)
	}
}
