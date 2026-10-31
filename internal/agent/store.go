package agent

import (
	"bytes"
	"hash/fnv"
	"runtime"
	"strconv"
	"sync"
)

// lockedMap is a small mutex-guarded map. The conversation
// state of the agent used to live in plain maps behind one
// global turn lock: every conversation waited behind every
// other conversation's slowest external call, and the maps
// themselves were only safe because nothing ever ran in
// parallel. Turns now lock per speaker (see turnLockFor),
// so the maps carry their own structural safety: this type
// guards the map itself, while the per-speaker turn lock
// guards the conversation's state machine. A Session value
// is only ever mutated by a turn holding its speaker's lock
// (or by a background pass that takes the same lock first).
type lockedMap[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

func newLockedMap[K comparable, V any]() *lockedMap[K, V] {
	return &lockedMap[K, V]{m: map[K]V{}}
}

// get returns the value for key, or the zero value.
func (l *lockedMap[K, V]) get(key K) V {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.m[key]
}

// lookup returns the value and whether the key is present.
func (l *lockedMap[K, V]) lookup(key K) (V, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	v, ok := l.m[key]
	return v, ok
}

func (l *lockedMap[K, V]) put(key K, v V) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m[key] = v
}

func (l *lockedMap[K, V]) del(key K) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, key)
}

func (l *lockedMap[K, V]) len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.m)
}

// snapshot returns a copy of the map, taken under the lock;
// the caller may then work on the copy without holding
// anything (session snapshots marshal this way, so a big
// state never stalls turns behind an encoder).
func (l *lockedMap[K, V]) snapshot() map[K]V {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make(map[K]V, len(l.m))
	for k, v := range l.m {
		out[k] = v
	}
	return out
}

// replaceAll swaps the whole contents (restore, test reset).
func (l *lockedMap[K, V]) replaceAll(m map[K]V) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if m == nil {
		m = map[K]V{}
	}
	l.m = m
}

// rangeAll visits every entry; returning false stops.
func (l *lockedMap[K, V]) rangeAll(fn func(K, V) bool) {
	for k, v := range l.snapshot() {
		if !fn(k, v) {
			return
		}
	}
}

// Turn locks: one mutex per speaker, striped so the table
// itself stays small. A turn holds its speaker's stripe for
// its whole run: the same person's messages can never race
// their own conversation, while different people proceed in
// parallel — one slow model call no longer queues the whole
// platform behind it (the 2026-10-30 audit measured a
// merchant's model-free command waiting 2.7s behind a single
// buyer's delayed turn under the old global lock).
var turnStripes [128]sync.Mutex

func stripeIndex(speaker string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(speaker))
	return int(h.Sum32() % uint32(len(turnStripes)))
}

func turnLockFor(speaker string) *sync.Mutex {
	return &turnStripes[stripeIndex(speaker)]
}

// Turn and cross-section registries. Go's sync.Mutex is
// unowned — it cannot say WHO holds it — so the lock
// discipline keeps its own facts, keyed by the one
// identity a goroutine does have: its runtime id.
// claimTurn registers the claiming goroutine as the
// turn's owner for its stripe; runCrossSection
// registers the running goroutine as a cross section's
// owner for both its stripes. withCrossLock compares
// its caller's id against those registrations instead
// of probing mutex state: the fourth audit's probe
// could not tell a stripe held by the caller from one
// held by another turn, and the fifth audit showed the
// registration-free version could not either — a count
// of turns says a turn stands, never that THIS caller
// is it. Brief direct holds that never call
// withCrossLock (a sweep pass writing one session, the
// snapshot's per-speaker copy) do not register; the
// cross lock's paths simply wait behind them like any
// other holder.
var turnRegistryMu sync.Mutex
var turnOwner [128]uint64
var crossOwner [128]uint64

// goID returns the calling goroutine's runtime id,
// parsed from its stack header — the only identity a
// goroutine can present. Zero means the parse failed;
// callers treat zero as "identity unknown" and take the
// safe road (a cross call that cannot prove it holds
// its turn takes the symmetric path, never the
// step-out).
func goID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	fields := bytes.Fields(buf[:n])
	if len(fields) < 2 || string(fields[0]) != "goroutine" {
		return 0
	}
	id, err := strconv.ParseUint(string(fields[1]), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// claimTurn takes a speaker's turn stripe and registers
// the claiming goroutine as its owner, returning the
// release. Every turn — the live door and the test
// harness's inTurn — takes its stripe through here, so
// the registry the cross lock consults is a fact about
// this goroutine, not an inference from mutex state.
func claimTurn(speaker string) func() {
	gid := goID()
	lock := turnLockFor(speaker)
	lock.Lock()
	turnRegistryMu.Lock()
	turnOwner[stripeIndex(speaker)] = gid
	turnRegistryMu.Unlock()
	return func() {
		turnRegistryMu.Lock()
		if turnOwner[stripeIndex(speaker)] == gid {
			turnOwner[stripeIndex(speaker)] = 0
		}
		turnRegistryMu.Unlock()
		lock.Unlock()
	}
}

// withCrossLock runs fn holding the OTHER speaker's turn
// lock, for the turns and passes that must write a session
// belonging to someone else (a merchant's answer advancing
// her buyer's sale, a buyer's wait arming the merchant's
// question). The second audit found five such paths
// writing bare: a buyer cancelling inside her own turn
// could have the cancellation silently undone by the
// merchant's concurrent yes replacing her session whole.
// When the two speakers hash to the same stripe, the lock
// the caller already holds IS the other speaker's lock —
// her turns are already excluded — so fn runs at once;
// taking the same mutex again would deadlock the turn
// against itself.
// Two turns running in opposite directions — a merchant's
// answer writing her buyer's session while the buyer's own
// message arms the merchant's — each hold their own stripe
// and reach for the other's; taken in caller order, the
// pair deadlock permanently, and because the snapshot pass
// walks every stripe in turn, one wedged pair halts
// session persistence process-wide until restart (the
// third audit). A first fix serialised the cross sections
// on one mutex and called the deadlock unreachable; it was
// not — the serialiser ordered the second stripe against
// other cross sections, but never against the plain turn
// already holding it, and the race run caught the pair
// still wedging (the plain suite's scheduling had hidden
// it). Serialising is not ordering.
//
// The sound protocol steps OUT before it steps across:
// the caller holds self's stripe for its turn, so it
// releases that stripe first, takes both stripes in
// stripe order, runs fn, releases both, and takes its own
// stripe back before returning. A cross section then only
// ever waits holding a lower stripe for a higher one, or
// holding nothing; plain turns never wait while holding
// theirs (their own cross call releases first), so no
// cycle can form. The contract fn runs under: it fetches
// the sessions it touches inside the call — every call
// site does — and a caller that uses its OWN session
// after the call re-fetches it, because during the
// step-out another turn of the same speaker may run, and
// the session store is the meeting point.
//
// HOW THE CALLER'S PLACE IS KNOWN, the fifth audit's
// question: the helper reads the registries above, and
// the answer decides the road.
//
//   - A turn for self is registered to THIS goroutine
//     (the ordinary case: every production caller runs
//     inside its own turn, and tests wrap direct handler
//     calls in inTurn for exactly this reason): the
//     step-out protocol above. The caller releases its
//     stripe, takes both in stripe order, runs fn,
//     releases both, takes its own stripe back.
//   - No turn for self is registered: the caller does
//     NOT hold self's stripe — another turn may, or
//     nobody does — so there is nothing to step out of,
//     and unlocking anything would steal a live turn's
//     exclusion. The caller takes both stripes in stripe
//     order like any newcomer, waits behind whoever
//     holds them, runs fn, and releases both, returning
//     holding nothing it did not take itself.
//   - Nesting is not supported: a cross closure must
//     not call withCrossLock. A cross section marks both
//     its stripes, and self's stripe can only be marked
//     by an ancestor call on this same goroutine (while
//     a section holds it, no other turn or section can
//     take it to mark it), so a call whose self is
//     marked is a nested one, and it is refused by name
//     before it touches a lock — where the old code
//     deadlocked re-locking the outer section's other
//     stripe, or silently dropped it. The remaining
//     shapes are excluded by this contract itself: cross
//     closures fetch sessions and send messages, and
//     neither of those calls back into a handler. No
//     production closure nests; the refusal keeps the
//     demonstrated shapes loud.
//
// Both roads are exception-safe: the releases and the
// step-back-in are deferred the moment the pair is held,
// so a panic inside fn restores the lock state the
// caller entered with before the panic propagates (the
// fifth audit found a panicking closure stranding the
// other speaker's stripe for the life of the process).
func withCrossLock(self, other string, fn func()) {
	if other == "" || other == self {
		fn()
		return
	}
	si, oi := stripeIndex(self), stripeIndex(other)
	if si == oi {
		fn()
		return
	}
	selfLock, otherLock := &turnStripes[si], &turnStripes[oi]
	gid := goID()
	turnRegistryMu.Lock()
	if gid != 0 && (crossOwner[si] == gid || crossOwner[oi] == gid) {
		turnRegistryMu.Unlock()
		panic("agent: withCrossLock nested inside a cross section for " + self + " — nesting is not supported; run the inner work inside the outer closure")
	}
	inTurn := gid != 0 && turnOwner[si] == gid
	turnRegistryMu.Unlock()
	first, second := selfLock, otherLock
	if oi < si {
		first, second = otherLock, selfLock
	}
	if !inTurn {
		// Symmetric road: take both in stripe order,
		// run, release both. Waits behind any live
		// holder, steals nothing.
		first.Lock()
		second.Lock()
		defer second.Unlock()
		defer first.Unlock()
		runCrossSection(si, oi, fn)
		return
	}
	// Step-out road: release self's stripe, take both in
	// stripe order, run, release both, step back in —
	// the release and the step-back-in deferred, so a
	// panic in fn restores the entry state. The
	// registration travels with the lock (the sixth
	// audit): it is released with the stripe, so another
	// turn claiming this stripe during the step-out
	// registers itself honestly, and restored on the
	// step-back-in — without the restore, an interleaved
	// claim's release cleared the slot, the caller
	// re-acquired its stripe unregistered, and its next
	// cross call in the same turn misread itself as
	// outside its turn and deadlocked on the symmetric
	// road against the stripe it already held.
	selfLock.Unlock()
	if gid != 0 {
		turnRegistryMu.Lock()
		if turnOwner[si] == gid {
			turnOwner[si] = 0
		}
		turnRegistryMu.Unlock()
	}
	first.Lock()
	second.Lock()
	defer func() {
		second.Unlock()
		first.Unlock()
		selfLock.Lock()
		if gid != 0 {
			turnRegistryMu.Lock()
			turnOwner[si] = gid
			turnRegistryMu.Unlock()
		}
	}()
	runCrossSection(si, oi, fn)
}

// runCrossSection registers the running goroutine as
// the owner of both stripes for the length of fn — the
// registration the nesting refusal reads — and clears
// it as it leaves, panic or not. The caller holds both
// stripes.
func runCrossSection(si, oi int, fn func()) {
	gid := goID()
	turnRegistryMu.Lock()
	crossOwner[si] = gid
	crossOwner[oi] = gid
	turnRegistryMu.Unlock()
	defer func() {
		turnRegistryMu.Lock()
		if crossOwner[si] == gid {
			crossOwner[si] = 0
		}
		if crossOwner[oi] == gid {
			crossOwner[oi] = 0
		}
		turnRegistryMu.Unlock()
	}()
	fn()
}
