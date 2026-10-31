package agent

import (
	"fmt"
	"testing"
	"time"
)

// Run-2 regression: cross-speaker session writes take the
// other speaker's turn lock, and when the two speakers
// hash to the same stripe the lock already held IS that
// lock — the write runs inline instead of deadlocking
// the turn against itself.
func TestCrossLockSameStripeRunsInline(t *testing.T) {
	a := "233550000001"
	b := ""
	for i := 2; i < 100000; i++ {
		cand := fmt.Sprintf("23355%07d", i)
		if turnLockFor(cand) == turnLockFor(a) {
			b = cand
			break
		}
	}
	if b == "" {
		t.Fatal("no stripe collision found in range")
	}
	// Hold a's stripe the way a's own turn would, then
	// write b's session through the cross lock: it must
	// complete, not deadlock.
	lock := turnLockFor(a)
	lock.Lock()
	defer lock.Unlock()
	ran := false
	withCrossLock(a, b, func() { ran = true })
	if !ran {
		t.Fatal("the cross-lock body must run")
	}
	ran = false
	withCrossLock(a, a, func() { ran = true })
	if !ran {
		t.Fatal("a same-speaker cross lock must run inline")
	}
}

// Run-2 regression: the idle eviction could never reach
// unbound sessions — the canonical one-off stranger on
// the shared number — because its resting test admitted
// only the empty and browsing stages.
func TestUnboundSessionEvictsWhenIdle(t *testing.T) {
	setupChat(t, "233550000001", "")
	dropInMemoryState()
	key := sessionKey("", "233559999991")
	sessions.put(key, &Session{Stage: "unbound", TouchedAt: time.Now().Add(-31 * 24 * time.Hour)})
	out := snapshotSessionsCopy(time.Now())
	if _, kept := out[key]; kept {
		t.Fatal("an unbound session idle past the limit must be evicted")
	}
	if sessions.get(key) != nil {
		t.Fatal("eviction must remove the session from the live map")
	}
}

// Run-2 regression: a session with a zero TouchedAt was
// exempt from eviction forever. The snapshot stamps it
// under its speaker's lock, so the ordinary idle clock
// starts instead of an eternal exemption — and the fresh
// session itself is kept.
func TestZeroTouchedSessionIsStampedNotEvicted(t *testing.T) {
	setupChat(t, "233550000001", "")
	dropInMemoryState()
	key := sessionKey("demo", "233559999992")
	sessions.put(key, &Session{Stage: "browsing", MerchantID: "demo"})
	out := snapshotSessionsCopy(time.Now())
	if _, kept := out[key]; !kept {
		t.Fatal("a freshly created session must be kept")
	}
	if live := sessions.get(key); live == nil || live.TouchedAt.IsZero() {
		t.Fatal("the snapshot must stamp a zero TouchedAt")
	}
	// And once stamped, the idle clock can still evict it.
	out = snapshotSessionsCopy(time.Now().Add(31 * 24 * time.Hour))
	if _, kept := out[key]; kept {
		t.Fatal("a stamped session idle past the limit must evict")
	}
}
