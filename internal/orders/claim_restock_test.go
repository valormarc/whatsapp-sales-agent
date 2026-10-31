package orders

import (
	"path/filepath"
	"sync"
	"testing"
)

// Run-2 regression: the restock decision was taken from a
// snapshot copy before the ending update, so two roads
// ending the same order at once each gave its units back.
// The claim is decided inside the book's lock now, and
// exactly one claimant ever wins.
func TestClaimRestockIsClaimedOnce(t *testing.T) {
	b := New(filepath.Join(t.TempDir(), "orders.json"))
	b.RecordOrder("TEST-CLAIM-1", Order{
		Reference: "TEST-CLAIM-1", Status: "pending", From: "233550000001",
		StockLines: []StockLine{{Name: "Bag", Units: 2}},
	})
	lines, ok := b.ClaimRestock("TEST-CLAIM-1")
	if !ok || len(lines) != 1 || lines[0].Units != 2 {
		t.Fatalf("the first claim wins the lines, got %v %v", lines, ok)
	}
	if _, ok := b.ClaimRestock("TEST-CLAIM-1"); ok {
		t.Fatal("a second claim must get nothing")
	}

	b.RecordOrder("TEST-CLAIM-2", Order{
		Reference: "TEST-CLAIM-2", Status: "pending", From: "233550000001",
		StockLines: []StockLine{{Name: "Bag", Units: 1}},
	})
	wins := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := b.ClaimRestock("TEST-CLAIM-2"); ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("exactly one of ten concurrent claims may win, got %d", wins)
	}
}
