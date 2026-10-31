package agent

// Work in progress survives a restart. Sessions used to
// live only in memory, so every restart — a deploy, a
// crash, the platform's own recovery — dropped every open
// listing, every kept photo batch and every buyer mid-haggle
// back to the front door, and the person had to start again
// or reconstruct her place herself. The durable books never
// had this problem; the conversations did.
//
// The fix is a snapshot, not a re-architecture: the whole
// session state (sessions, shop bindings, and the two small
// memories that ride with them) is written to sessions.json
// in the data directory on a short timer and again when the
// process is told to stop, and read back when the process
// starts. A snapshot is written to a temporary file and
// renamed over the old one, so a crash mid-write can never
// leave a torn file behind; a snapshot that cannot be read
// is simply not restored, and everyone starts fresh, which
// is the worst case and the old behaviour. The loss window
// is the timer's interval: seconds of conversation, never
// the conversation.

import (
	"encoding/json"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
)

// sessionSnapshotInterval is how often the session state is
// written out while the process runs. Ten seconds keeps the
// loss window to a breath while the file stays small.
const sessionSnapshotInterval = 10 * time.Second

// sessionSnapshot is the whole in-memory conversation state
// of the process, in one file.
type sessionSnapshot struct {
	Sessions     map[string]*Session `json:"sessions"`
	Bindings     map[string]string   `json:"bindings"`
	DoorShown    map[string]bool     `json:"doorShown"`
	PackageNames map[string]string   `json:"packageNames"`
}

var sessionSnapshotsOnce sync.Once

// StartSessionSnapshots restores any snapshot the last run
// left behind, then keeps the snapshot current: a write
// every interval, and a final write when the process is
// told to stop, after which it exits as it would have.
func StartSessionSnapshots() {
	sessionSnapshotsOnce.Do(func() {
		restoreSessions()
		go func() {
			for {
				time.Sleep(sessionSnapshotInterval)
				SnapshotSessionsNow()
			}
		}()
		stops := make(chan os.Signal, 1)
		signal.Notify(stops, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-stops
			SnapshotSessionsNow()
			os.Exit(0)
		}()
	})
}

// sessionIdleLimit bounds the session state's lifetime
// cardinality: a resting session untouched for this long is
// dropped at snapshot time, so the state of a shared number
// cannot grow forever on one-off conversations. A session
// is resting only when nothing is in flight in it — no
// product in hand, no basket, no pending question, no open
// sale, listing walk or signup — so an eviction can never
// cut across live work.
const sessionIdleLimit = 30 * 24 * time.Hour

func sessionResting(s *Session) bool {
	if s == nil {
		return true
	}
	return (s.Stage == "" || s.Stage == "browsing" || s.Stage == "unbound") &&
		s.Product == nil && len(s.Basket) == 0 &&
		s.PendingAdd == nil && s.PaymentURL == "" &&
		s.Signup == nil && s.Onboarding == nil &&
		!s.PendingClose && s.YesNoQ == nil
}

// copySession deep-copies one session through JSON, the
// same encoding the snapshot file uses, so the copy is
// exactly what would be written.
func copySession(s *Session) *Session {
	if s == nil {
		return nil
	}
	data, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	var out Session
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return &out
}

// snapshotSessionsCopy returns a deep copy of the whole
// session state, safe to marshal with nothing held. Each
// session is copied under its own speaker's turn lock, so
// the copy never races the turn that owns the session, and
// locking is per speaker: one conversation's copy never
// waits behind another's turn. Idle eviction is decided on
// the copies and applied under the same per-speaker locks.
func snapshotSessionsCopy(now time.Time) map[string]*Session {
	out := map[string]*Session{}
	for key := range sessions.snapshot() {
		speaker := key
		if i := strings.LastIndex(key, "|"); i >= 0 {
			speaker = key[i+1:]
		}
		lock := turnLockFor(speaker)
		lock.Lock()
		live := sessions.get(key)
		if live == nil {
			lock.Unlock()
			continue
		}
		// A session created during a turn — often by
		// another speaker, a merchant's answer arming her
		// buyer's session — never passes the turn-start
		// stamp, and a zero TouchedAt exempted it from
		// idle eviction forever (the second audit). Stamp
		// it here, under its own speaker's lock, the first
		// time a snapshot sees it: from then on the
		// ordinary idle clock runs.
		if live.TouchedAt.IsZero() {
			live.TouchedAt = now
		}
		cp := copySession(live)
		if cp != nil && !cp.TouchedAt.IsZero() && now.Sub(cp.TouchedAt) >= sessionIdleLimit && sessionResting(cp) {
			// Idle and resting: evicted, with her binding
			// to that shop (she re-enters by her link).
			sessions.del(key)
			if i := strings.LastIndex(key, "|"); i >= 0 {
				if bindings.get(speaker) == key[:i] {
					bindings.del(speaker)
				}
			}
			lock.Unlock()
			continue
		}
		lock.Unlock()
		if cp != nil {
			out[key] = cp
		}
	}
	return out
}

// SnapshotSessionsNow writes the current session state to
// the data directory, atomically. The state is deep-copied
// per speaker (see snapshotSessionsCopy) and marshalled
// afterwards with nothing held, so a snapshot never stalls
// turns behind an encoder, however large the state grows
// (the 2026-10-30 audit measured the old marshal under the
// global lock at 224ms for 20,000 sessions, every ten
// seconds). A copy can catch a session between turns; that
// is the same freshness the periodic cadence always had,
// and completions snapshot themselves.
// snapshotMu serialises whole snapshots against each
// other. Concurrent calls — the periodic cadence, the
// goroutines completions fire, restores — shared the one
// fixed temp path with no ordering, so an older copy
// could rename over a newer sessions.json and the durable
// state went BACKWARDS under continuous writers (the
// third audit watched a marker rise to 234 and regress).
// A snapshot is cheap insurance taken often; taking them
// one at a time loses nothing.
var snapshotMu sync.Mutex

func SnapshotSessionsNow() {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	snap := sessionSnapshot{
		Sessions:     snapshotSessionsCopy(time.Now()),
		Bindings:     bindings.snapshot(),
		DoorShown:    doorShown.snapshot(),
		PackageNames: packageNames.snapshot(),
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return
	}
	tmp := datadir.Path("sessions.json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, datadir.Path("sessions.json"))
}

// The erasure tombstones. Erasure deletes a party's
// sessions from memory at once, but the durable snapshot
// is written asynchronously (it cannot be taken inside
// her turn: the snapshot copies each session under its
// own speaker's stripe, and her stripe is the one her
// turn is holding) — so a crash in the window between
// the deletion and the next snapshot restored her
// sessions whole at boot (the fifth audit). The lane now
// also writes a tombstone, synchronously, before it
// confirms: her number and the erasure time, kept in
// erased.json. Restore drops any of her sessions whose
// last touch is not after the tombstone — a session she
// starts fresh after erasing carries a later touch and
// is untouched by it. Tombstones are never cleared: one
// small line per erasure, and they double as the erasure
// record.
type erasedParty struct {
	Number string `json:"number"`
	At     string `json:"at"`
}

var erasedMu sync.Mutex
var erasedParties []erasedParty
var erasedLoaded bool

func loadErasedLocked() {
	path := datadir.Path("erased.json")
	// The load latches only when it completes (the sixth
	// audit): a failed load — a poisoned book — is retried
	// on the next call, so a file repaired on disk is
	// picked up in-process, its poison clears through the
	// ordinary load, and the tombstones this process
	// holds in memory are saved again by the next
	// markErased instead of being refused for the
	// process's whole life.
	if erasedLoaded && !bookPathPoisoned(path) {
		return
	}
	// A retry loads into a scratch slice and merges: the
	// in-memory list may carry tombstones appended while
	// the book was poisoned and their saves refused, and
	// replacing the list with the file's older contents
	// would drop exactly those.
	var fromFile []erasedParty
	if loadBookJSON(path, &fromFile) || !bookPathPoisoned(path) {
		seen := map[erasedParty]bool{}
		merged := make([]erasedParty, 0, len(fromFile)+len(erasedParties))
		for _, e := range fromFile {
			seen[e] = true
			merged = append(merged, e)
		}
		for _, e := range erasedParties {
			if !seen[e] {
				merged = append(merged, e)
			}
		}
		erasedParties = merged
		erasedLoaded = true
	}
}

// markErased records the tombstone for a party whose
// data is being deleted now.
func markErased(number string) {
	erasedMu.Lock()
	defer erasedMu.Unlock()
	loadErasedLocked()
	// The tombstone carries sub-second precision (the
	// sixth audit): at second precision the stored time
	// truncated BELOW the real erasure instant, and a
	// session touched earlier in the same second — still
	// pre-erasure — read as touched after it and survived
	// the restore filter.
	erasedParties = append(erasedParties, erasedParty{Number: number, At: time.Now().UTC().Format(time.RFC3339Nano)})
	saveBookJSON(datadir.Path("erased.json"), erasedParties)
}

// erasedAt returns the most recent erasure time recorded
// for a party, if any.
func erasedAt(number string) (time.Time, bool) {
	erasedMu.Lock()
	defer erasedMu.Unlock()
	loadErasedLocked()
	var latest time.Time
	found := false
	for _, e := range erasedParties {
		if e.Number != number {
			continue
		}
		if at, err := time.Parse(time.RFC3339Nano, e.At); err == nil && (!found || at.After(latest)) {
			latest, found = at, true
		}
	}
	return latest, found
}

// restoreSessions reads the last snapshot back into memory
// at boot. A missing or unreadable snapshot is not an
// error: the process simply starts with empty state, as
// it always did before snapshots existed.
func restoreSessions() {
	// The completion journal is reconciled first, while
	// nothing has spoken yet: a sale that took stock and
	// died before its order reached the book gives its
	// units back here (see sale_journal.go).
	reconcileSaleJournal()
	data, err := os.ReadFile(datadir.Path("sessions.json"))
	if err != nil {
		return
	}
	var snap sessionSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return
	}
	if snap.Sessions != nil {
		sessions.replaceAll(snap.Sessions)
	}
	if snap.Bindings != nil {
		bindings.replaceAll(snap.Bindings)
	}
	if snap.DoorShown != nil {
		doorShown.replaceAll(snap.DoorShown)
	}
	if snap.PackageNames != nil {
		packageNames.replaceAll(snap.PackageNames)
	}
	// Erasure tombstones are applied before anything is
	// revalidated or spoken into: a session the snapshot
	// still holds for a party erased after that snapshot
	// was taken does not come back (see markErased). Her
	// binding goes with her sessions when none survive.
	sessions.rangeAll(func(key string, s *Session) bool {
		speaker := key
		if i := strings.LastIndex(key, "|"); i >= 0 {
			speaker = key[i+1:]
		}
		if at, erased := erasedAt(speaker); erased && (s == nil || !s.TouchedAt.After(at)) {
			sessions.del(key)
		}
		return true
	})
	for speaker := range bindings.snapshot() {
		has := false
		sessions.rangeAll(func(key string, s *Session) bool {
			if strings.HasSuffix(key, "|"+speaker) {
				has = true
			}
			return true
		})
		if !has {
			if _, erased := erasedAt(speaker); erased {
				bindings.del(speaker)
			}
		}
	}
	// The tombstone covers every snapshot map, not only
	// sessions and bindings (the sixth audit): a package
	// name remembered for an erased speaker and her
	// door-shown mark both revive from a stale snapshot
	// otherwise — the lane deletes them in memory, but a
	// crash before the async snapshot lands left the old
	// file holding them. Package-name entries carry no
	// touch time, so any entry for an erased speaker goes.
	for key := range packageNames.snapshot() {
		speaker := key
		if i := strings.LastIndex(key, "|"); i >= 0 {
			speaker = key[i+1:]
		}
		if _, erased := erasedAt(speaker); erased {
			packageNames.del(key)
		}
	}
	for speaker := range doorShown.snapshot() {
		if _, erased := erasedAt(speaker); erased {
			doorShown.del(speaker)
		}
	}
	// Foreign references get the same sweep the live lane
	// performs (the sixth audit): a surviving session —
	// the merchant's — can hold an armed in-hand question
	// whose Ref names an erased party, and restore must
	// not resurrect her through somebody else's session.
	sessions.rangeAll(func(key string, s *Session) bool {
		if s != nil && s.YesNoQ != nil {
			// The tombstone stands at or after the
			// session's last touch: the question was
			// armed before the erasure and names a party
			// who no longer exists here.
			if at, erased := erasedAt(s.YesNoQ.Ref); erased && !at.Before(s.TouchedAt) {
				s.YesNoQ = nil
			}
		}
		return true
	})
	// The shelf may have moved while the process was down —
	// a product removed, a price changed — and a restored
	// session must never complete a sale against a shelf
	// that no longer stands. Every restored session is
	// revalidated against the live catalogue before anyone
	// speaks into it.
	sessions.rangeAll(func(key string, s *Session) bool {
		rebindSaleIDFromJournal(key, s)
		revalidateRestoredSession(s)
		return true
	})
}

// revalidateRestoredSession brings one restored session
// back in line with the live catalogue: product pointers
// are refreshed, basket lines and picks whose product has
// gone are dropped, and a pending sale whose product was
// removed — or whose list or last price changed while the
// process was down — is put down, so the buyer starts that
// purchase again from the shelf as it stands. A sale that
// already completed (its payment link is on the session)
// is the order book's business and is left alone.
func revalidateRestoredSession(s *Session) {
	if s == nil || s.MerchantID == "" {
		return
	}
	if s.PendingRemove != "" {
		// A restored removal question is revalidated like
		// everything else the restore carries: the name's
		// current product must be the very product the
		// question was armed on. A replacement under the
		// same name — or a snapshot from before identities
		// were recorded, which cannot be verified at all
		// — puts the question down rather than letting a
		// later yes delete a product she never named for
		// removal (the fifth audit).
		live := catalogue.FindByName(s.MerchantID, s.PendingRemove)
		if s.PendingRemoveID == "" || live == nil || live.ID != s.PendingRemoveID {
			s.PendingRemove, s.PendingRemoveID = "", ""
			s.clearYesNo("remove")
		}
	}
	{
		out := make([]BasketItem, 0, len(s.Basket))
		for _, it := range s.Basket {
			if it.Product == nil {
				continue
			}
			// A basket line binds again only to the very
			// product it was priced on: same identity, same
			// list and last price. The second audit found
			// lines refreshed by name alone, so a product
			// removed and replaced under the same or a
			// containing name silently rebound the line to
			// the replacement, below its floor.
			live := liveProductFor(s.MerchantID, it.Product)
			if live == nil || live.ID != it.Product.ID ||
				live.ListPrice != it.Product.ListPrice || live.FloorPrice != it.Product.FloorPrice ||
				live.AvailableUnits() <= 0 {
				// Unavailable is unbindable (the seventh
				// audit): a line whose goods sold out
				// while the process was down is dropped
				// like a replaced line, never continued
				// as available.
				continue
			}
			it.Product = live
			out = append(out, it)
		}
		s.Basket = out
	}
	if len(s.ShelfPicks) > 0 {
		picks := make([]string, 0, len(s.ShelfPicks))
		for _, id := range s.ShelfPicks {
			if catalogue.FindByID(s.MerchantID, id) != nil {
				picks = append(picks, id)
			}
		}
		s.ShelfPicks = picks
	}
	if s.PendingAdd != nil {
		if live := liveProductFor(s.MerchantID, s.PendingAdd); live == nil || live.AvailableUnits() <= 0 {
			s.PendingAdd = nil
			s.clearYesNo("add")
		} else {
			s.PendingAdd = live
		}
	}
	if s.LastViewed != nil {
		if live := liveProductFor(s.MerchantID, s.LastViewed); live == nil || live.AvailableUnits() <= 0 {
			s.LastViewed = nil
		} else {
			s.LastViewed = live
		}
	}
	if s.Product == nil || s.PaymentURL != "" {
		return
	}
	live := liveProductFor(s.MerchantID, s.Product)
	if live == nil || live.ListPrice != s.Product.ListPrice || live.FloorPrice != s.Product.FloorPrice ||
		live.AvailableUnits() <= 0 || (s.Qty > 0 && live.ByMeasure() && live.AvailableUnits() < s.Qty) {
		// A pending sale whose goods are gone (or short
		// of the measured quantity) is put down whole
		// (the seventh audit), like a repriced one.
		dropRestoredSale(s)
		return
	}
	s.Product = live
}

// dropRestoredSale puts down a restored session's pending
// sale: every field that priced or staged it is cleared and
// the buyer is back to browsing, with her basket, place and
// history untouched.
func dropRestoredSale(s *Session) {
	s.Product = nil
	s.Ask, s.Floor, s.AgreedPrice, s.ClosePrice = 0, 0, 0, 0
	s.Round, s.StepScale = 0, 0
	s.Qty, s.Unit = 0, ""
	s.PendingClose, s.CloseReasked, s.CloseFromView = false, false, false
	s.FulfilStep = ""
	s.PendingFulfil = fulfilment{}
	s.FeeQuotePlace = ""
	s.InHandOK = false
	// The second audit found two survivors: the total hand
	// (a dropped total-haggle session completed on the
	// buyer's next yes at the sum of the stale hand's
	// floors) and the sale identity itself. Both go.
	s.TotalHand = nil
	s.SaleID = ""
	s.clearYesNo("close")
	s.clearYesNo("add")
	if s.Stage == "haggling" || s.Stage == "agreed" || s.Stage == "fulfilment" || s.Stage == "lastprice" {
		s.Stage = "browsing"
	}
}

// liveProductFor resolves a session's remembered product
// against the live catalogue by identity where the
// remembered product carries one, falling back to the
// name only for products minted before identities rode
// along. A name match alone is never treated as the same
// product when an identity exists to check.
func liveProductFor(shopID string, p *catalogue.Product) *catalogue.Product {
	if p == nil {
		return nil
	}
	if p.ID != "" {
		return catalogue.FindByID(shopID, p.ID)
	}
	return catalogue.FindByName(shopID, p.Name)
}
