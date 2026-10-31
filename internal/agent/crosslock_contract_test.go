package agent

import (
	"strings"
	"testing"
	"time"
)

// The cross-lock contract tests. Run-4 stated the
// step-out precondition and asserted it with a mutex
// probe; run-5 showed the probe could not tell a stripe
// held by the caller from one held by another turn (Go
// mutexes are unowned), and added two more shapes: a
// panicking closure stranding the other stripe, and a
// nested call deadlocking on the outer section's holds.
// The protocol now reads the turn registry instead of
// probing, takes a symmetric road when no turn for self
// is registered, unwinds exceptionally, and refuses
// nesting by name.

func crossPair(t *testing.T) (string, string) {
	t.Helper()
	a, b := "233550009901", "233550009902"
	if stripeIndex(a) == stripeIndex(b) {
		b = "233550009903"
	}
	if stripeIndex(a) == stripeIndex(b) {
		t.Fatal("need two speakers on different stripes")
	}
	return a, b
}

// An outside-turn call made while another goroutine
// holds self's stripe must not steal it: the cross work
// waits for the holder to finish, runs after, and the
// caller returns holding nothing it did not take.
func TestWithCrossLockOutsideATurnWaitsForTheHolder(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	a, b := crossPair(t)
	release := claimTurn(a) // another goroutine's turn, as far as the registry knows

	done := make(chan struct{})
	go func() {
		defer close(done)
		withCrossLock(a, b, func() {})
	}()
	select {
	case <-done:
		t.Fatal("the outside-turn call ran while the holder still held the stripe — the lock was stolen")
	case <-time.After(150 * time.Millisecond):
	}
	release()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the outside-turn call must run once the holder lets go")
	}
	// It returned holding nothing: a fresh turn for the
	// same speaker takes its stripe at once.
	release2 := claimTurn(a)
	release2()
}

// A panic inside the cross closure must restore the
// lock state the caller entered with: the caller's own
// turn goes on holding its stripe, and the OTHER
// speaker's stripe is free again — not stranded for
// the life of the process.
func TestWithCrossLockPanicRestoresBothStripes(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	a, b := crossPair(t)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("the panic must propagate")
			}
		}()
		inTurn(a, func() bool {
			withCrossLock(a, b, func() { panic("closure panic") })
			return true
		})
	}()
	// The other stripe is free: its owner can take a
	// turn at once.
	releaseB := claimTurn(b)
	releaseB()
	// And no cross registration leaked.
	turnRegistryMu.Lock()
	marked := crossOwner[stripeIndex(a)] != 0 || crossOwner[stripeIndex(b)] != 0
	turnRegistryMu.Unlock()
	if marked {
		t.Fatal("a panicking section must clear its cross registrations")
	}
}

// A nested cross call — the closure calling
// withCrossLock again for its own speaker — is refused
// by name instead of deadlocking on the outer section's
// holds, and the outer section's state unwinds clean.
func TestWithCrossLockNestingIsRefusedByName(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	a, b := crossPair(t)
	var innerErr string
	inTurn(a, func() bool {
		withCrossLock(a, b, func() {
			defer func() {
				if r := recover(); r != nil {
					if msg, ok := r.(string); ok {
						innerErr = msg
					}
				}
			}()
			withCrossLock(a, b, func() {})
		})
		return true
	})
	if !strings.Contains(innerErr, "nested") {
		t.Fatalf("the nested call must be refused by name, got %q", innerErr)
	}
	releaseB := claimTurn(b)
	releaseB()
}

