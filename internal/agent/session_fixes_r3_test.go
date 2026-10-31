package agent

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
)

// Run-3 record: withCrossLock took the second turn stripe
// in caller order, so two turns running in opposite
// directions (each holding its own stripe, each reaching
// for the other's) deadlocked permanently — and one
// wedged pair halted session snapshotting process-wide.
// A first fix serialised cross sections on one mutex and
// the plain suite passed, but the race run still wedged
// the pair: the serialiser never ordered the second
// stripe against the plain turn holding it. The protocol
// now steps out of the caller's own stripe and takes
// both in stripe order — this test is the contract, and
// it is run under the race detector, where the wedge
// actually showed.
func TestCrossLockOppositeDirectionsComplete(t *testing.T) {
	a, b := "233550010001", ""
	for i := 2; i < 500; i++ {
		cand := "233550010000"[:10] + string(rune('0'+i/100)) + string(rune('0'+(i/10)%10)) + string(rune('0'+i%10))
		if turnLockFor(cand) != turnLockFor(a) {
			b = cand
			break
		}
	}
	if b == "" {
		t.Fatal("need two speakers on different stripes")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for _, pair := range [][2]string{{a, b}, {b, a}} {
			wg.Add(1)
			go func(self, other string) {
				defer wg.Done()
				// Turns are taken through inTurn (the
				// registered claim) as production takes
				// them: the run-5 protocol reads the
				// registry to know the caller holds its
				// stripe, and a raw lock is invisible to
				// it — the symmetric road would wait on
				// the caller's own stripe forever.
				for i := 0; i < 100; i++ {
					inTurn(self, func() bool {
						withCrossLock(self, other, func() {})
						return true
					})
				}
			}(pair[0], pair[1])
		}
		wg.Wait()
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("opposite-direction cross locks must complete, not deadlock")
	}
}

// Run-3 record: concurrent SnapshotSessionsNow calls
// shared one temp path with no ordering, and durable
// state regressed under continuous writers. Serialised,
// every snapshot leaves a complete, parseable file.
func TestConcurrentSnapshotsLeaveAValidFile(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	sessions.put(sessionKey("demo", stockOwner), &Session{Stage: "merchant", MerchantID: "demo"})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 15; j++ {
				SnapshotSessionsNow()
			}
		}()
	}
	wg.Wait()
	raw, err := os.ReadFile(datadir.Path("sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("the surviving snapshot must be whole JSON, got %v", err)
	}
}

// Run-3 record: beginFulfilment reminted the sale
// identity unconditionally, discarding the identity the
// completion journal had rebound onto a restored session,
// so the restored sale completed as a twin order. The
// rebound identity now survives; a session with none
// still gets a fresh one.
func TestBeginFulfilmentKeepsTheReboundSaleID(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	buyer := "233550010777"
	s := &Session{Stage: "agreed", MerchantID: "demo", Product: bag, AgreedPrice: 800, SaleID: "sale-rebound-1"}
	beginFulfilment(buyer, s, 800)
	if s.SaleID != "sale-rebound-1" {
		t.Fatalf("a journal-rebound identity must survive, got %q", s.SaleID)
	}
	s2 := &Session{Stage: "agreed", MerchantID: "demo", Product: bag, AgreedPrice: 800}
	beginFulfilment(buyer, s2, 800)
	if s2.SaleID == "" || s2.SaleID == "sale-rebound-1" {
		t.Fatalf("a session with no identity must be minted a fresh one, got %q", s2.SaleID)
	}
}
