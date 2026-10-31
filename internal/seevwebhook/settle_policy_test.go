package seevwebhook

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Regression for the audit finding that SettleVerified
// applied the paid transition unconditionally: duplicate
// verified events re-told both sides, and a late paid event
// resurrected a cancelled order. The policy now lives at
// the convergence point: settle only from an open state,
// no-op when already paid, refuse the ended states and tell
// the merchant once that real money arrived for a dead order.
func outboxLinesTo(t *testing.T, who, needle string) int {
	t.Helper()
	data, err := os.ReadFile("data/outbox.jsonl")
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"to":"`+who+`"`) && strings.Contains(line, needle) {
			n++
		}
	}
	return n
}

func TestSettleVerifiedIsIdempotentAndStateBound(t *testing.T) {
	t.Setenv("MERCHANT_WHATSAPP", whOwner)

	buyerBase := outboxLinesTo(t, whBuyer, "Payment received, thank you.")
	merchantBase := outboxLinesTo(t, whOwner, "Payment received")
	seedOrder("TEST-SETTLE-1", nil)
	o := orders.Default.GetOrder("TEST-SETTLE-1")
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			SettleVerified(o, "Payment received, thank you.")
		}()
	}
	wg.Wait()
	if got := orders.Default.GetOrder("TEST-SETTLE-1"); got == nil || got.Status != "paid" {
		t.Fatalf("the order must stand paid, got %+v", got)
	}
	if n := outboxLinesTo(t, whBuyer, "Payment received, thank you.") - buyerBase; n != 1 {
		t.Fatalf("the buyer must be told exactly once, got %d", n)
	}
	if n := outboxLinesTo(t, whOwner, "Payment received") - merchantBase; n != 1 {
		t.Fatalf("the merchant must be told exactly once, got %d", n)
	}

	seedOrder("TEST-SETTLE-2", nil)
	orders.Default.SetStatus("TEST-SETTLE-2", "cancelled")
	o2 := orders.Default.GetOrder("TEST-SETTLE-2")
	SettleVerified(o2, "Payment received, thank you.")
	SettleVerified(o2, "Payment received, thank you.")
	if got := orders.Default.GetOrder("TEST-SETTLE-2"); got == nil || got.Status != "cancelled" {
		t.Fatalf("a late paid event must not resurrect a cancelled order, got %+v", got)
	}
	if n := outboxLinesTo(t, whOwner, "already stands cancelled"); n != 1 {
		t.Fatalf("the merchant must be alerted exactly once about the stranded payment, got %d", n)
	}

	seedOrder("TEST-SETTLE-3", nil)
	orders.Default.SetStatus("TEST-SETTLE-3", "failed")
	SettleVerified(orders.Default.GetOrder("TEST-SETTLE-3"), "Payment received, thank you.")
	if got := orders.Default.GetOrder("TEST-SETTLE-3"); got == nil || got.Status != "failed" {
		t.Fatalf("a failed order must not settle, got %+v", got)
	}
}
