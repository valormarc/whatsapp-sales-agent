package agent

import (
	"os"
	"strings"
	"testing"
)

// Run-4 record: the dedupe mark used to be the message
// book record, written when a turn STARTS — durable
// before the turn ran, behind a 200 already sent — so a
// crash mid-turn lost the message permanently and
// silently. The delivery ledger now commits "handled"
// only when a turn completes, keeps the delivery itself
// in a durable inbox until then, and replays what a
// crash left standing.

func TestRedeliveryOfACompletedMessageIsDropped(t *testing.T) {
	dir := setupChat(t, stockOwner, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550005101"
	in := Inbound{From: buyer, Text: "SHOP-TEST", MessageID: "dup-delivery-1"}
	HandleInbound(in)
	first := c.replies(t, buyer)
	if len(first) == 0 {
		t.Fatal("the first delivery must be handled")
	}
	HandleInbound(in)
	HandleInbound(in)
	// replies() returns only what is new since the last
	// read: redeliveries must have added nothing at all.
	if got := c.replies(t, buyer); len(got) != 0 {
		t.Fatalf("redeliveries of a completed message must be dropped whole, they added %q", got)
	}
	if !alreadyHandled("dup-delivery-1") {
		t.Fatal("a completed delivery must carry the handled mark")
	}
}

func TestACrashedDeliveryIsReplayedFromTheInbox(t *testing.T) {
	dir := setupChat(t, stockOwner, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550005102"
	in := Inbound{From: buyer, Text: "SHOP-TEST", MessageID: "crash-delivery-1"}

	// Leave the delivery standing exactly as a crash
	// mid-turn would: in the durable inbox, no handled
	// mark, nothing in flight.
	deliveryMu.Lock()
	loadInboxLocked()
	inboxPending = append(inboxPending, in)
	saveInboxLocked()
	deliveryMu.Unlock()

	// Simulate the restart: in-memory ledger state is
	// rebuilt from the files.
	deliveryMu.Lock()
	inFlightIDs = map[string]bool{}
	handledLoaded = false
	handledOrder = nil
	handledSet = map[string]bool{}
	inboxLoaded = false
	inboxPending = nil
	deliveryMu.Unlock()

	if len(c.replies(t, buyer)) != 0 {
		t.Fatal("nothing may be handled before the replay")
	}
	replayPendingInbox()
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Test Shop") {
		t.Fatalf("the replay must run the crashed delivery's turn, got %q", got)
	}
	deliveryMu.Lock()
	loadInboxLocked()
	remaining := len(inboxPending)
	deliveryMu.Unlock()
	if remaining != 0 {
		t.Fatalf("a completed replay must drain its inbox entry, %d remain", remaining)
	}
	// And a second boot replays nothing: the handled
	// mark now stands, so no new reply is produced.
	replayPendingInbox()
	if got := c.replies(t, buyer); len(got) != 0 {
		t.Fatalf("a second replay must not re-run a handled delivery, it added %q", got)
	}
}

// resetLedgerForTest puts the delivery ledger's in-memory
// state back to its boot shape over the current files:
// the package state is process-wide, so tests that drive
// the loaders directly reset it by hand, as the crash
// replay test above does inline.
func resetLedgerForTest() {
	deliveryMu.Lock()
	defer deliveryMu.Unlock()
	inFlightIDs = map[string]bool{}
	handledLoaded = false
	handledOrder = nil
	handledSet = map[string]bool{}
	inboxLoaded = false
	inboxPending = nil
	inboxPoisoned = false
}

// Run-5 record: a corrupt inbox.json was silently read as
// empty and then overwritten by the next delivery,
// destroying the uncommitted deliveries the ledger exists
// to replay. The inbox now restores from its
// last-known-good copy (repairing the file), and a double
// failure poisons its saves.
func TestCorruptInboxRestoresAndPoisons(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	resetLedgerForTest()
	deliveryMu.Lock()
	loadInboxLocked()
	inboxPending = []Inbound{{From: "233550000777", Text: "hi", MessageID: "wamid.CORRUPT1"}}
	saveInboxLocked()
	deliveryMu.Unlock()
	if err := os.WriteFile(inboxFile(), []byte("{torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	resetLedgerForTest()
	deliveryMu.Lock()
	loadInboxLocked()
	restored := len(inboxPending) == 1 && inboxPending[0].MessageID == "wamid.CORRUPT1"
	deliveryMu.Unlock()
	if !restored {
		t.Fatal("the inbox must restore from its good copy")
	}
	raw, _ := os.ReadFile(inboxFile())
	if string(raw) == "{torn" {
		t.Fatal("the restore must repair the inbox file")
	}
	// Double failure: poisoned, and the next save refuses.
	if err := os.WriteFile(inboxFile(), []byte("{torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inboxFile()+".bak", []byte("{also torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	resetLedgerForTest()
	deliveryMu.Lock()
	loadInboxLocked()
	saveInboxLocked()
	deliveryMu.Unlock()
	raw, _ = os.ReadFile(inboxFile())
	if string(raw) != "{torn" {
		t.Fatalf("a poisoned inbox save must be refused, file holds %q", raw)
	}
	resetLedgerForTest()
}

// Run-5 record (medium): the boot replay ran pending
// turns in a bare goroutine with no recovery, so one
// panicking delivery crash-looped the whole process on
// every restart. A panicking entry is now quarantined —
// drained from the inbox, not marked handled — and the
// replay carries on with the next entry.
func TestReplayQuarantinesAPanickingDelivery(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	resetLedgerForTest()
	bad := Inbound{From: "233550000779", Text: "boom", MessageID: "wamid.PANIC1"}
	good := Inbound{From: "233550000780", Text: "later", MessageID: "wamid.PANIC2"}
	deliveryMu.Lock()
	inboxLoaded = true
	inboxPending = []Inbound{bad, good}
	deliveryMu.Unlock()
	orig := runReplayedDelivery
	defer func() { runReplayedDelivery = orig }()
	var ran []string
	runReplayedDelivery = func(in Inbound) {
		ran = append(ran, in.MessageID)
		if in.MessageID == bad.MessageID {
			panic("test panic")
		}
	}
	replayPendingInbox()
	if len(ran) != 2 {
		t.Fatalf("the replay must survive the panic and run the next entry, ran %v", ran)
	}
	deliveryMu.Lock()
	remaining := append([]Inbound{}, inboxPending...)
	deliveryMu.Unlock()
	if len(remaining) != 1 || remaining[0].MessageID != good.MessageID {
		t.Fatalf("the panicking entry must be quarantined from the inbox, remaining %v", remaining)
	}
	if alreadyHandled(bad.MessageID) {
		t.Fatal("a quarantined entry must not be marked handled")
	}
	resetLedgerForTest()
}

// Run-5 records: erasure never touched the delivery
// ledger or the sale journal. Her pending inbox entries
// now leave at delete-my-data (their ids committed as
// handled marks, so the same delivery cannot recreate
// her afterwards), and her journal entries go with them.
func TestErasureStrikesTheLedgerAndTheJournal(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	resetLedgerForTest()
	her := "233550000781"
	other := "233550000782"
	deliveryMu.Lock()
	inboxLoaded = true
	inboxPending = []Inbound{
		{From: her, Text: "her words", MessageID: "wamid.ERASE1"},
		{From: other, Text: "not hers", MessageID: "wamid.ERASE2"},
	}
	deliveryMu.Unlock()
	purgePartyFromLedger(her)
	deliveryMu.Lock()
	remaining := append([]Inbound{}, inboxPending...)
	deliveryMu.Unlock()
	if len(remaining) != 1 || remaining[0].From != other {
		t.Fatalf("only her entries may leave the inbox, remaining %v", remaining)
	}
	if !alreadyHandled("wamid.ERASE1") {
		t.Fatal("her purged delivery's id must stand as a handled mark")
	}
	recordSaleCompletion(sessionKey("demo", her), "sale-erase-1", "TEST-ERASE-1", "beaded-bag", "demo", nil)
	recordSaleCompletion(sessionKey("demo", other), "sale-erase-2", "TEST-ERASE-2", "beaded-bag", "demo", nil)
	forgetSaleJournalFor(her)
	for _, e := range loadSaleJournal() {
		if strings.HasSuffix(e.Key, "|"+her) {
			t.Fatalf("her journal entry must be purged, still holds %+v", e)
		}
	}
	resetLedgerForTest()
}

// Run-5 record: the handled-set seed (message book ids)
// must exclude deliveries still standing in the inbox —
// under a partial restore the seed marked a crashed,
// never-completed delivery as handled and the boot
// replay silently dropped it.
func TestHandledSeedExcludesPendingInboxDeliveries(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	resetLedgerForTest()
	from := "233550000778"
	recordMsg(msgRecord{ID: "wamid.SEED1", Party: from, Author: "user", Body: "a bag please"})
	deliveryMu.Lock()
	inboxLoaded = true
	inboxPending = []Inbound{{From: from, Text: "a bag please", MessageID: "wamid.SEED1"}}
	deliveryMu.Unlock()
	if alreadyHandled("wamid.SEED1") {
		t.Fatal("a delivery still standing in the inbox must not seed as handled")
	}
	resetLedgerForTest()
}
