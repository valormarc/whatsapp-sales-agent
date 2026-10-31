package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/valormarc/hackathon-agent/internal/datadir"
)

// The delivery ledger. WhatsApp delivers a webhook more
// than once when the answer is slow, and a photo turn is
// slow, so deliveries are deduplicated by message id —
// but a dedupe mark is a promise about the past, and it
// must only be made once it is true. The old rule read
// the message book, where a message is recorded when its
// turn STARTS (so the turn itself can quote it): the mark
// was durable before the turn ran, the server had already
// answered 200, and a crash mid-turn lost the message
// permanently and silently — no redelivery would come,
// and if one did, the mark would drop it (the fourth
// audit; the agent-runtime review made the same point
// from the essay side). The ledger keeps the three states
// apart:
//
//   - in flight: an in-memory claim, so a twin delivery
//     arriving while the first is still being handled is
//     dropped (the first will finish it);
//   - in the inbox: the delivery itself, written durably
//     BEFORE its turn runs, and removed only when the
//     turn completes — a crash leaves it there, and the
//     boot replay runs it again;
//   - handled: the durable set of ids whose turns
//     completed, committed only on the turn's normal
//     return. A redelivery of one of these is dropped
//     whole, before it can record, answer or act again —
//     the same photo must never open a second listing.
//
// A message with no id cannot be judged and is never
// dropped on any of these grounds.

var deliveryMu sync.Mutex
var inFlightIDs = map[string]bool{}
var handledLoaded bool
var handledOrder []string
var handledSet = map[string]bool{}
var inboxLoaded bool
var inboxPending []Inbound

// inboxPoisoned is set when the inbox file would not
// parse and no last-known-good copy could be restored:
// inbox saves are then refused rather than overwriting
// the uncommitted deliveries the ledger exists to replay
// (the fifth audit found the corrupt inbox read as empty
// and then silently overwritten by the next delivery).
var inboxPoisoned bool

const handledKeep = 5000

func handledFile() string { return datadir.Path("handled.json") }
func inboxFile() string   { return datadir.Path("inbox.json") }

// loadHandledLocked brings the handled set into memory.
// Its restore ladder is the platform discipline in the
// shape this set allows: the file itself, then its
// last-known-good copy (repairing the file), then — the
// fallback unique to this set — a seed from the message
// book, whose inbound records are durable evidence of
// deliveries already run. The seed EXCLUDES any delivery
// still standing in the inbox (the fifth audit: under a
// partial restore — messages and inbox present, handled
// missing — seeding from the book alone marked a crashed,
// never-completed delivery as handled, and the boot
// replay silently dropped the very message the ledger
// exists to recover). The caller holds deliveryMu.
func loadHandledLocked() {
	if handledLoaded {
		return
	}
	handledLoaded = true
	loadInboxLocked()
	data, readErr := os.ReadFile(handledFile())
	unreadablePresent := readErr != nil && !os.IsNotExist(readErr)
	if readErr != nil && unreadablePresent {
		// An existing handled file that cannot be read
		// tries its last known good copy before any seed
		// (the sixth audit): the seed path below would
		// otherwise cement its partial set over both the
		// file and its backup.
		if bak, berr := os.ReadFile(handledFile() + ".bak"); berr == nil {
			var ids []string
			if json.Unmarshal(bak, &ids) == nil {
				fmt.Fprintf(os.Stderr, "agent: %s could not be read (%v); restored from the last known good copy\n", handledFile(), readErr)
				_ = writeFileAtomic(handledFile(), bak)
				for _, id := range ids {
					if id != "" && !handledSet[id] {
						handledSet[id] = true
						handledOrder = append(handledOrder, id)
					}
				}
				return
			}
		}
		fmt.Fprintf(os.Stderr, "agent: %s could not be read (%v) and no good copy could be restored; seeding the handled set from the message book WITHOUT saving it over the unreadable file\n", handledFile(), readErr)
	}
	if readErr == nil {
		var ids []string
		if json.Unmarshal(data, &ids) == nil {
			for _, id := range ids {
				if id != "" && !handledSet[id] {
					handledSet[id] = true
					handledOrder = append(handledOrder, id)
				}
			}
			return
		}
		_ = os.WriteFile(handledFile()+".corrupt", data, 0o600)
		if bak, berr := os.ReadFile(handledFile() + ".bak"); berr == nil && json.Unmarshal(bak, &ids) == nil {
			fmt.Fprintf(os.Stderr, "agent: %s does not parse; restored from the last known good copy\n", handledFile())
			_ = writeFileAtomic(handledFile(), bak)
			for _, id := range ids {
				if id != "" && !handledSet[id] {
					handledSet[id] = true
					handledOrder = append(handledOrder, id)
				}
			}
			return
		}
		fmt.Fprintf(os.Stderr, "agent: %s does not parse and no good copy could be restored; seeding the handled set from the message book\n", handledFile())
	}
	pending := map[string]bool{}
	for _, e := range inboxPending {
		pending[e.MessageID] = true
	}
	msgMu.Lock()
	idx := loadMsgIndex()
	for _, id := range msgOrder {
		if rec := idx[id]; rec.Author == "user" && id != "" && !pending[id] && !handledSet[id] {
			handledSet[id] = true
			handledOrder = append(handledOrder, id)
		}
	}
	msgMu.Unlock()
	// The seed is saved back — except when the handled
	// file stands present but unreadable with no restore
	// (the sixth audit): saving there would cement the
	// partial seed over the very file, and its backup,
	// that a later repair could still recover.
	if !unreadablePresent {
		saveHandledLocked()
	}
}

// saveHandledLocked persists the handled set, keeping the
// most recent ids: redeliveries arrive near their
// original, and the set exists to catch those, not to be
// an archive. The same bytes refresh the last-known-good
// copy. The caller holds deliveryMu.
func saveHandledLocked() {
	if len(handledOrder) > handledKeep {
		trim := len(handledOrder) - handledKeep
		for _, id := range handledOrder[:trim] {
			delete(handledSet, id)
		}
		handledOrder = append([]string{}, handledOrder[trim:]...)
	}
	if data, err := json.Marshal(handledOrder); err == nil {
		if writeFileAtomic(handledFile(), data) {
			_ = writeFileAtomic(handledFile()+".bak", data)
		}
	}
}

// loadInboxLocked brings the durable inbox into memory
// under the platform's corrupt-file discipline: a file
// that does not parse is preserved under a single
// .corrupt name, the last-known-good copy is restored
// and the main file repaired with its bytes, and only
// when both fail does the inbox load empty — poisoned,
// with saves refused, so the next delivery cannot cement
// the loss the load shouted about. The caller holds
// deliveryMu.
func loadInboxLocked() {
	if inboxLoaded {
		return
	}
	inboxLoaded = true
	data, err := os.ReadFile(inboxFile())
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		// An existing inbox that cannot be read is a
		// failed load, not an empty one (the sixth
		// audit): restore from the last known good copy,
		// and poison when none restores, so no save
		// cements the loss and boot replay does not act
		// on an empty read.
		if bak, berr := os.ReadFile(inboxFile() + ".bak"); berr == nil {
			var restored []Inbound
			if uerr := json.Unmarshal(bak, &restored); uerr == nil {
				fmt.Fprintf(os.Stderr, "agent: %s could not be read (%v); restored from the last known good copy\n", inboxFile(), err)
				_ = writeFileAtomic(inboxFile(), bak)
				inboxPending = restored
				inboxPoisoned = false
				return
			}
		}
		fmt.Fprintf(os.Stderr, "agent: %s could not be read (%v) and no good copy could be restored; the inbox reads empty and its saves are refused until the file loads again\n", inboxFile(), err)
		inboxPoisoned = true
		return
	}
	var entries []Inbound
	if err := json.Unmarshal(data, &entries); err == nil {
		inboxPending = entries
		inboxPoisoned = false
		return
	}
	_ = os.WriteFile(inboxFile()+".corrupt", data, 0o600)
	if bak, berr := os.ReadFile(inboxFile() + ".bak"); berr == nil {
		var restored []Inbound
		if uerr := json.Unmarshal(bak, &restored); uerr == nil {
			fmt.Fprintf(os.Stderr, "agent: %s does not parse; restored from the last known good copy\n", inboxFile())
			_ = writeFileAtomic(inboxFile(), bak)
			inboxPending = restored
			inboxPoisoned = false
			return
		}
	}
	fmt.Fprintf(os.Stderr, "agent: %s does not parse and no good copy could be restored; the inbox reads empty and its saves are refused until the file parses again\n", inboxFile())
	inboxPoisoned = true
}

func saveInboxLocked() {
	if inboxPoisoned {
		fmt.Fprintf(os.Stderr, "agent: refusing to save over %s: the last load did not parse and no good copy could be restored\n", inboxFile())
		return
	}
	if data, err := json.Marshal(inboxPending); err == nil {
		if writeFileAtomic(inboxFile(), data) {
			_ = writeFileAtomic(inboxFile()+".bak", data)
		}
	}
}

// alreadyHandled reports whether an earlier delivery of
// this message was handled IN FULL — its turn completed
// and the completion was committed. WhatsApp's own
// duplicates of a completed message stop here.
func alreadyHandled(id string) bool {
	if id == "" {
		return false
	}
	deliveryMu.Lock()
	defer deliveryMu.Unlock()
	loadHandledLocked()
	return handledSet[id]
}

// HandleInbound is the delivery door: it applies the
// ledger, then runs the turn. A delivery whose id was
// handled in full is dropped; a delivery whose twin is in
// flight is dropped; any other delivery is written to the
// durable inbox before its turn, and its completion —
// inbox removal plus the handled mark — is committed
// only when the turn returns normally. A panic or a
// process death mid-turn leaves the inbox entry standing
// for the boot replay, and leaves the id unhandled, so a
// redelivery is a second chance rather than a duplicate.
func HandleInbound(in Inbound) {
	if in.MessageID == "" {
		handleInboundTurn(in)
		return
	}
	if alreadyHandled(in.MessageID) {
		return
	}
	deliveryMu.Lock()
	if inFlightIDs[in.MessageID] {
		deliveryMu.Unlock()
		return
	}
	inFlightIDs[in.MessageID] = true
	// The handled set is rechecked under the claim (the
	// fifth audit): the check above runs before the lock,
	// and a twin whose first copy COMPLETED in the gap
	// between the check and the claim committed its mark
	// there — without this recheck the delivery ran its
	// turn a second time.
	loadHandledLocked()
	if handledSet[in.MessageID] {
		delete(inFlightIDs, in.MessageID)
		deliveryMu.Unlock()
		return
	}
	loadInboxLocked()
	pending := false
	for _, e := range inboxPending {
		if e.MessageID == in.MessageID {
			pending = true
			break
		}
	}
	if !pending {
		inboxPending = append(inboxPending, in)
		saveInboxLocked()
	}
	deliveryMu.Unlock()
	defer func() {
		deliveryMu.Lock()
		delete(inFlightIDs, in.MessageID)
		deliveryMu.Unlock()
	}()

	handleInboundTurn(in)

	deliveryMu.Lock()
	kept := inboxPending[:0]
	for _, e := range inboxPending {
		if e.MessageID != in.MessageID {
			kept = append(kept, e)
		}
	}
	inboxPending = kept
	saveInboxLocked()
	loadHandledLocked()
	if !handledSet[in.MessageID] {
		handledSet[in.MessageID] = true
		handledOrder = append(handledOrder, in.MessageID)
	}
	saveHandledLocked()
	deliveryMu.Unlock()
}

// purgePartyFromLedger strikes one party's standing
// deliveries from the ledger at erasure (the fifth
// audit: delete-my-data never touched the ledger, so her
// pending inbox entries kept her number, her words and
// her form answers after erasure was confirmed — and a
// later replay re-ran a pre-erasure delivery and
// recreated the state she had just had deleted). Her
// pending entries are removed, and their ids are
// committed to the handled set — an id is not personal
// data — so the same delivery arriving again from
// WhatsApp is dropped whole instead of recreating her.
func purgePartyFromLedger(from string) {
	deliveryMu.Lock()
	defer deliveryMu.Unlock()
	loadInboxLocked()
	loadHandledLocked()
	kept := inboxPending[:0]
	changed := false
	for _, e := range inboxPending {
		if e.From == from {
			changed = true
			if e.MessageID != "" && !handledSet[e.MessageID] {
				handledSet[e.MessageID] = true
				handledOrder = append(handledOrder, e.MessageID)
			}
			continue
		}
		kept = append(kept, e)
	}
	inboxPending = kept
	if changed {
		saveInboxLocked()
		saveHandledLocked()
	}
}

var replayOnce sync.Once

// StartInboxReplay runs the boot replay: deliveries
// left standing in the inbox by a crash are run again,
// in the order they arrived. It runs SYNCHRONOUSLY —
// the server calls it before it starts listening — so
// no replayed delivery can land after a newer live
// delivery from the same sender and overwrite its effect
// (the fifth audit found the old goroutine racing live
// traffic from the moment the listener opened). Each
// entry is an ordinary HandleInbound, so a replayed
// delivery that completes drains its own inbox entry
// and commits its handled mark, and one that was in
// fact handled before the crash (its completion raced
// the death) is dropped by the handled set instead of
// running twice.
func StartInboxReplay() {
	replayOnce.Do(replayPendingInbox)
}

func replayPendingInbox() {
	deliveryMu.Lock()
	loadInboxLocked()
	pending := append([]Inbound{}, inboxPending...)
	deliveryMu.Unlock()
	for _, in := range pending {
		replayOne(in)
	}
}

// runReplayedDelivery is the turn runner the replay
// uses — HandleInbound itself, named as a variable so
// the replay's panic quarantine can be exercised in
// tests without a genuinely panicking turn.
var runReplayedDelivery = HandleInbound

// replayOne runs one replayed delivery behind a
// recovery: a turn that panics inside the webhook is
// caught by net/http per connection, but the boot
// replay has no such net — the fifth audit's medium was
// exactly this, one panicking delivery crash-looping
// the whole process on every restart, the inbox entry
// standing each time. A panicking entry is quarantined:
// drained from the inbox so it cannot loop, NOT marked
// handled (a future redelivery may still run it, in a
// context that can survive it), and named in the log.
func replayOne(in Inbound) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "agent: replay of delivery %s from %s panicked (%v); the entry is quarantined from the inbox, unhandled\n", in.MessageID, in.From, r)
			deliveryMu.Lock()
			kept := inboxPending[:0]
			for _, e := range inboxPending {
				if e.MessageID != in.MessageID {
					kept = append(kept, e)
				}
			}
			inboxPending = kept
			saveInboxLocked()
			deliveryMu.Unlock()
		}
	}()
	runReplayedDelivery(in)
}
