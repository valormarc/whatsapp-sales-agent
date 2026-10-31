package agent

import (
	"fmt"
	"testing"
	"time"
)

// Run-6 record: the step-out road re-acquired the turn
// stripe without restoring its registration, so an
// interleaving claim on the same stripe during the
// step-out overwrote and cleared the caller's ownership,
// and the caller's next cross call in the same turn
// deadlocked on the symmetric road against the stripe it
// already held. The contract: after a same-stripe claim
// runs to completion during a step-out, the stepped-out
// turn's next cross call completes.
func TestCrossLockStepBackInRestoresRegistration(t *testing.T) {
	num := func(i int) string { return fmt.Sprintf("23355999%04d", i) }
	var self, other, third string
	for i := 0; i < 2000 && self == ""; i++ {
		cand := num(i)
		for j := 0; j < 2000; j++ {
			if oc := num(j); stripeIndex(oc) < stripeIndex(cand) {
				self, other = cand, oc
				break
			}
		}
	}
	for k := 0; k < 2000 && third == ""; k++ {
		if tc := num(k); stripeIndex(tc) != stripeIndex(self) && stripeIndex(tc) != stripeIndex(other) {
			third = tc
		}
	}
	if self == "" || other == "" || third == "" {
		t.Fatal("could not find speakers on the needed stripes")
	}

	// B holds the other stripe, so A's step-out blocks
	// acquiring it with self's stripe standing free.
	bHolding := make(chan struct{})
	bRelease := make(chan struct{})
	go func() {
		releaseB := claimTurn(other)
		close(bHolding)
		<-bRelease
		releaseB()
	}()
	<-bHolding

	done := make(chan struct{})
	go func() {
		defer close(done)
		releaseA := claimTurn(self)
		withCrossLock(self, other, func() {})
		// The second cross call in the same turn is the
		// one the record deadlocks.
		withCrossLock(self, third, func() {})
		releaseA()
	}()

	// Wait until A has stepped out: self's stripe stands
	// free while A blocks on B's.
	deadline := time.Now().Add(5 * time.Second)
	for {
		lock := turnLockFor(self)
		if lock.TryLock() {
			lock.Unlock()
			break
		}
		if time.Now().After(deadline) {
			close(bRelease)
			t.Fatal("A never stepped out of its stripe")
		}
		time.Sleep(time.Millisecond)
	}
	// The interleaving claim on self's stripe runs to
	// completion during the step-out.
	releaseC := claimTurn(self)
	releaseC()
	close(bRelease)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the stepped-out turn's next cross call deadlocked: the step-back-in did not restore its registration")
	}
}
