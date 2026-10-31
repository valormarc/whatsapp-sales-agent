package agent

import (
	"fmt"
	"os"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/datadir"
)

// Regression for the audit finding that the agent-side
// books loaded silently as empty on a parse failure: a
// torn trust write unflagged a three-strike seller because
// nothing preserved the evidence or said a word. A corrupt
// book now keeps its bytes under a .corrupt- name.
func TestCorruptTrustBookIsPreservedNotSwallowed(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	path := datadir.Path("trust.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadTrust(); len(got) != 0 {
		t.Fatalf("a corrupt book reads as empty, got %v", got)
	}
	// One fixed aside name since the fifth audit (a
	// timestamped copy per load accumulated without
	// bound): the preserved file is path + ".corrupt".
	raw, err := os.ReadFile(path + ".corrupt")
	if err != nil {
		t.Fatalf("the corrupt bytes must be preserved, got %v", err)
	}
	if string(raw) != "{not json" {
		t.Fatalf("the preserved file must hold the original bytes, got %q", raw)
	}
}

// Saves are atomic: the temp file is renamed over the book,
// so no .tmp is left behind and the book parses.
func TestTrustSaveIsAtomic(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	saveTrust(map[string]*trustRecord{"233500000001": {Strikes: 2}})
	if _, err := os.Stat(datadir.Path("trust.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("no temp file may be left behind by a save")
	}
	if got := loadTrust(); got["233500000001"] == nil || got["233500000001"].Strikes != 2 {
		t.Fatalf("the saved book must read back, got %v", got)
	}
}

// Regression for the audit findings that the message book
// grew without an aggregate bound and its compaction never
// settled: past the trigger, every new record re-ran a full
// rewrite removing almost nothing. The book now has a total
// ceiling, drops the globally oldest records to reach it,
// and then leaves the book alone until it grows past the
// ceiling plus a slack.
func TestMessageBookCompactionSettlesUnderTheCeiling(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	parties := 100
	per := (keepTotalMax + compactionSlack + 500) / parties
	for p := 0; p < parties; p++ {
		party := fmt.Sprintf("233557%06d", p)
		for i := 0; i < per; i++ {
			recordMsg(msgRecord{ID: fmt.Sprintf("bulk-%d-%d", p, i), Party: party, Author: "user", Body: "hello"})
		}
	}
	msgMu.Lock()
	total := len(msgOrder)
	msgMu.Unlock()
	if total > keepTotalMax+compactionSlack {
		t.Fatalf("the book must never grow past the ceiling plus its slack: %d records, ceiling %d + slack %d", total, keepTotalMax, compactionSlack)
	}
	if total >= keepTotalMax+compactionSlack+500-100 {
		t.Fatalf("compaction must have cut the book back during seeding, got %d", total)
	}
	if _, ok := lookupMsg("bulk-0-0", "233557000000"); ok {
		t.Fatal("the globally oldest records are the ones dropped")
	}
	lastParty := fmt.Sprintf("233557%06d", parties-1)
	if _, ok := lookupMsg(fmt.Sprintf("bulk-%d-%d", parties-1, per-1), lastParty); !ok {
		t.Fatal("the newest records survive compaction")
	}
	// And it settles: a few more records do not trigger
	// another cut.
	for i := 0; i < 10; i++ {
		recordMsg(msgRecord{ID: fmt.Sprintf("after-%d", i), Party: lastParty, Author: "user", Body: "again"})
	}
	msgMu.Lock()
	after := len(msgOrder)
	msgMu.Unlock()
	if after != total+10 {
		t.Fatalf("compaction must settle, not re-run per message: before %d, after %d", total, after)
	}
}
