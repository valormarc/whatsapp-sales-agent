package agent

import (
	"testing"
	"time"
)

// Run-4 record: the run-3 item cap bounded how many
// pre-pass items a sweep actions, but not how long they
// take — each can make a provider read-back or a paced
// send, and the order pass's clock had not even started.
// The pre-passes now share one time budget per sweep,
// checked between actions, and a budget-cut tail goes
// back to open in step with its unsent messages.
func TestPrePassBudgetCutLeavesQuotesOpenAndUntold(t *testing.T) {
	merchant := "233550005201"
	dir := setupChat(t, merchant, shopQuoteStyle)
	c := &chat{dir: dir}
	buyers := []string{"233550005202", "233550005203", "233550005204"}
	for _, b := range buyers {
		openFeeQuote(feeQuote{ShopID: "demo", Buyer: b, Item: "Handmade Beaded Bag", Price: 850, Place: "Osu"})
	}
	all := loadFeeQuotes()
	for i := range all {
		all[i].CreatedAt = time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	}
	saveFeeQuotes(all)

	prePassDeadline = time.Now().Add(-time.Minute) // the budget is already spent
	defer func() { prePassDeadline = time.Time{} }()
	CheckFeeQuotes(time.Now().UTC())

	for _, q := range loadFeeQuotes() {
		if q.Status != "open" {
			t.Fatalf("a budget-cut pass must leave its tail open, got status %q", q.Status)
		}
	}
	for _, b := range buyers {
		if got := c.replies(t, b); len(got) != 0 {
			t.Fatalf("a budget-cut pass must not message, buyer got %q", got)
		}
	}
}
