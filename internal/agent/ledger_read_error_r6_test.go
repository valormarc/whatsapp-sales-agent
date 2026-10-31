package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/datadir"
)

// makeUnreadable replaces a data file with a symlink to
// a directory: reads of the path fail (not IsNotExist)
// while writes to the path still land, the shape a
// read-error record needs.
func makeUnreadable(t *testing.T, path string) {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join(filepath.Dir(path), "elsewhere-"+filepath.Base(path)))
	if err != nil {
		t.Fatal(err)
	}
	target := abs
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// Run-6 record: the delivery ledger's loaders read an
// existing-but-unreadable file as empty with no poison —
// the inbox's next save cemented the loss, and the
// handled loader skipped its backup and cemented its
// seeded partial set over both copies.
func TestLedgerReadErrorRestoresFromBackup(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	resetLedgerForTest()
	deliveryMu.Lock()
	loadInboxLocked()
	inboxPending = []Inbound{{From: "233550000777", Text: "hi", MessageID: "wamid.READ1"}}
	saveInboxLocked()
	loadHandledLocked()
	handledSet["wamid.READ0"] = true
	handledOrder = append(handledOrder, "wamid.READ0")
	saveHandledLocked()
	deliveryMu.Unlock()

	makeUnreadable(t, inboxFile())
	makeUnreadable(t, handledFile())

	resetLedgerForTest()
	deliveryMu.Lock()
	loadInboxLocked()
	loadHandledLocked()
	inboxOK := len(inboxPending) == 1 && inboxPending[0].MessageID == "wamid.READ1"
	handledOK := handledSet["wamid.READ0"]
	poisoned := inboxPoisoned
	deliveryMu.Unlock()
	if !inboxOK {
		t.Fatal("an unreadable inbox must restore its pending deliveries from the backup")
	}
	if poisoned {
		t.Fatal("a restored inbox is not poisoned")
	}
	if !handledOK {
		t.Fatal("an unreadable handled file must restore its set from the backup")
	}
	resetLedgerForTest()
}

func TestLedgerReadErrorWithoutBackupPoisonsInbox(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	resetLedgerForTest()
	makeUnreadable(t, inboxFile())
	deliveryMu.Lock()
	loadInboxLocked()
	poisoned := inboxPoisoned
	saveInboxLocked()
	deliveryMu.Unlock()
	if !poisoned {
		t.Fatal("an unreadable inbox with no good copy must be poisoned")
	}
	fi, err := os.Lstat(inboxFile())
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("a poisoned inbox save must be refused (the path stands untouched), lstat err=%v", err)
	}
	resetLedgerForTest()
}

// Run-6 record: the erasure tombstone loader latched on
// a failed (poisoning) load, so post-repair markErased
// writes were refused for the life of the process and
// the tombstones stayed non-durable. The loader now
// retries while its book is poisoned and merges the
// in-memory appends a poisoned save refused.
func TestErasedTombstonesSurviveARepairedBook(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	erasedMu.Lock()
	erasedParties = nil
	erasedLoaded = false
	erasedMu.Unlock()

	path := erasedFileForTest()
	makeUnreadable(t, path)
	markErased("233511110001") // its save is refused while the book is poisoned
	if _, found := erasedAt("233511110001"); !found {
		t.Fatal("the tombstone must stand in memory even while its book is poisoned")
	}
	// Repair: a real file holding an older tombstone.
	_ = os.Remove(path)
	older := []erasedParty{{Number: "233511110099", At: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}}
	raw, _ := json.Marshal(older)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, found := erasedAt("233511110099"); !found {
		t.Fatal("a repaired tombstone book must be reloaded in-process")
	}
	if _, found := erasedAt("233511110001"); !found {
		t.Fatal("the pending in-memory tombstone must survive the reload (merge, not replace)")
	}
	markErased("233511110002")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, number := range []string{"233511110099", "233511110001", "233511110002"} {
		if !strings.Contains(string(raw), number) {
			t.Fatalf("the saved tombstone book must carry %s, holds %s", number, raw)
		}
	}
}

func erasedFileForTest() string {
	return datadir.Path("erased.json")
}
