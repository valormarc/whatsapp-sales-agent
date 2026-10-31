package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/wants"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// The privacy page promises: send the words "delete my data"
// and the personal information held about you is deleted.
// This lane is that promise kept. The ask is a control
// phrase, read deterministically like the other control
// shapes in this codebase: a person asking for erasure is
// never handed to a model's guess.
//
// The second audit found the first matcher wanting in both
// directions at once: it was anchored to the whole message,
// so a person who asked inside an ordinary sentence ("I
// want you to delete my data, please") got no erasure and
// no signal at all. The phrase is now recognised where a
// person actually puts it: opening the message or a clause,
// or following a plain request lead-in. A narration does
// not trigger it ("she said delete my data" is not an ask),
// and neither does a negation ("do not delete my data").
var deletePhraseRe = regexp.MustCompile(`(?i)(delete|remove|erase)\s+(all\s+)?(of\s+)?my\s+(data|details|information|info)\b`)

var deleteLeadIns = []string{
	"please", "i want", "i want you to", "i would like", "i'd like",
	"i need", "i need you to", "can you", "could you",
	"i am asking", "i'm asking", "go ahead and",
}

func isDeleteMyDataAsk(text string) bool {
	low := strings.ToLower(text)
	loc := deletePhraseRe.FindStringIndex(low)
	if loc == nil {
		return false
	}
	// Narration is not an ask (the sixth audit): when the
	// text before the phrase, back to the last sentence
	// break, frames the words as someone's speech —
	// "she said, delete my data" — the phrase is being
	// reported, not spoken by the sender. Commas do not
	// end that frame, so this check reads the whole
	// sentence, not the comma clause.
	sentence := low[:loc[0]]
	if i := strings.LastIndexAny(sentence, ".!?\n"); i >= 0 {
		sentence = sentence[i+1:]
	}
	// Her own self-report ("I said delete my data") is
	// still her ask; another person's speech is not.
	if trimmed := strings.TrimSpace(sentence); !strings.HasPrefix(trimmed, "i ") && !strings.HasPrefix(trimmed, "i'") {
		for _, marker := range []string{" said", " says", " told", " asked", " wrote", " texted", " messaged", "\"", "“"} {
			if strings.Contains(sentence, marker) {
				return false
			}
		}
	}
	// The clause the phrase sits in: everything since the
	// last sentence or clause break before it.
	before := low[:loc[0]]
	if i := strings.LastIndexAny(before, ".!?;\n,"); i >= 0 {
		before = before[i+1:]
	}
	clause := strings.TrimSpace(before)
	if clause == "" {
		return true
	}
	for _, neg := range []string{"do not", "don't", "never", "not"} {
		if clause == neg || strings.HasSuffix(clause, " "+neg) {
			return false
		}
	}
	for _, lead := range deleteLeadIns {
		if clause == lead || strings.HasSuffix(clause, " "+lead) {
			return true
		}
	}
	return false
}

// handleDeleteMyData erases one speaker's personal data
// across every book that holds it, and tells her plainly
// what went and what, by its nature, stays:
//
//   - her sessions and shop binding on this number: gone,
//     and any question another speaker's session has armed
//     about her (an in-hand ask naming her as its subject)
//     is disarmed with them.
//   - her recorded messages: gone, under her own party and
//     in relay copies recorded under anyone else.
//   - her wants: gone, so no stocking can ever message her
//     from a want she left behind.
//   - her in-hand waits and fee quotes: gone, under the
//     same locks their other writers hold.
//   - the package names remembered for her orders: gone.
//   - the local outbox copies that name her: gone, from
//     the live file and its rotated siblings alike.
//   - preserved copies (corrupt-book evidence, last known
//     good backups) that carry her number: gone.
//   - her orders: the personal details are struck (number,
//     delivery place, collector, package name, her note,
//     the rider's number for her, and her name as the
//     buyer on any dispute), leaving the shop an
//     anonymised sales record of goods and amounts only.
//
// What stays, by its nature: a shop she owns (its
// catalogue is hers to edit product by product, as the
// privacy page says), the anonymised sales record (the
// shop's book, not hers), and any trust record judged
// against her number as a seller — Stall's own record of
// disputes judged, kept because it protects other buyers.
// The second audit showed the first version of this lane
// erasing a flagged seller's strikes with everything
// else; that door is closed and is not reopened here.
func handleDeleteMyData(from string) {
	strikes := strikesOf(from)
	// Her order references are gathered before the orders
	// are anonymised: the ask-buyer relay copies of her
	// words sit in the message book under the MERCHANT's
	// party, carrying her order's reference but not her
	// number, and the purge below finds them by it (the
	// third audit's narrowed survivor of the other-speaker
	// copies record).
	var herRefs []string
	for _, o := range orders.Default.All() {
		if o.From == from {
			herRefs = append(herRefs, o.Reference)
		}
	}
	ordersTombstoned := orders.Default.AnonymiseBuyer(from)
	_ = wants.Default.Forget(from)
	forgetInHand(from)
	forgetFeeQuotes(from)
	// The fifth audit's erasure gaps, closed here with
	// the rest: her standing deliveries leave the ledger
	// (their ids stay as handled marks, so the same
	// delivery cannot recreate her after erasure), her
	// sale-journal entries go (the journal keys by her
	// session key, number included), and a tombstone is
	// written synchronously below so a crash before the
	// next session snapshot cannot restore her sessions
	// from the stale one.
	purgePartyFromLedger(from)
	forgetSaleJournalFor(from)
	purgeMsgParty(from, herRefs...)
	whatsapp.PurgeOutbox(from, herRefs...)
	// packageNames is keyed by shop-and-speaker session
	// key, never by the bare number: deleting by the bare
	// number matched nothing and her package name survived
	// erasure to be offered back on her next order (the
	// third audit). Sweep every key that is hers.
	for key := range packageNames.snapshot() {
		if strings.HasSuffix(key, "|"+from) {
			packageNames.del(key)
		}
	}
	purgePreservedCopies(from)

	// The turn already holds the session lock (see
	// HandleInbound), so the maps are edited directly, and
	// the snapshot is taken once the turn lets go of it —
	// the same pattern completeSale uses. Her OWN sessions
	// go outright, under her own stripe, already held. A
	// question armed on ANOTHER speaker's session with her
	// as its subject is that speaker's state: the third
	// audit found this lane clearing it bare through the
	// shared pointer, racing the owner's live turn and the
	// snapshot marshal — the one write in the codebase
	// outside the stripe discipline. Those clears collect
	// first and run under each owner's stripe, the same
	// road every other cross-speaker write takes.
	var foreign []string
	sessions.rangeAll(func(key string, s *Session) bool {
		if strings.HasSuffix(key, "|"+from) {
			sessions.del(key)
			return true
		}
		if s != nil && s.YesNoQ != nil && s.YesNoQ.Ref == from {
			foreign = append(foreign, key)
		}
		return true
	})
	for _, key := range foreign {
		speaker := key
		if i := strings.LastIndex(key, "|"); i >= 0 {
			speaker = key[i+1:]
		}
		withCrossLock(from, speaker, func() {
			if live := sessions.get(key); live != nil && live.YesNoQ != nil && live.YesNoQ.Ref == from {
				live.YesNoQ = nil
				sessions.put(key, live)
			}
		})
	}
	bindings.del(from)
	doorShown.del(from)
	// The tombstone lands synchronously, before the
	// confirmation below: the snapshot itself stays
	// asynchronous (it cannot be taken under her own
	// stripe), but restoreSessions now drops her
	// pre-erasure sessions by the tombstone even when
	// the snapshot on disk still holds them.
	markErased(from)
	go SnapshotSessionsNow()

	lines := []string{"Done. I have deleted the personal information I held about you:"}
	lines = append(lines, "• Your messages in this chat, and mine to you")
	lines = append(lines, "• Anything you asked me to watch for")
	if ordersTombstoned > 0 {
		lines = append(lines, "• Your name, number and delivery details from your orders")
	}
	if strikes > 0 {
		lines = append(lines, "• One thing stays: the record of disputes judged on this number, because it protects other buyers, the way the shops keep their anonymised books.")
	}
	lines = append(lines, "", "The shops keep an anonymised record of what was sold and for how much, with nothing on it that identifies you, because a business must keep its books. Messages already in your own WhatsApp are yours to delete there.")
	whatsapp.SendText(from, strings.Join(lines, "\n"))
}

// purgePreservedCopies removes the safety copies that can
// outlive the books themselves — a corrupt book preserved
// verbatim as evidence, a last-known-good backup — when
// they carry the deleted speaker's number. The live books
// are the lane's business; these copies were nobody's,
// and the second audit found her records surviving in
// them indefinitely.
func purgePreservedCopies(number string) {
	entries, err := os.ReadDir(datadir.Dir())
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		// The aside shapes the stores actually write: the
		// single fixed ".corrupt" name every store
		// standardised on in the fifth audit's build (the
		// retired timestamped ".corrupt-" asides may still
		// sit on older data directories), and the ".bak"
		// last-known-good copies. The filter previously
		// matched only the retired shape, so every current
		// aside survived the purge (the sixth audit).
		if !strings.HasSuffix(name, ".corrupt") && !strings.Contains(name, ".corrupt-") && !strings.HasSuffix(name, ".bak") {
			continue
		}
		path := filepath.Join(datadir.Dir(), name)
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), number) {
			_ = os.Remove(path)
		}
	}
}
