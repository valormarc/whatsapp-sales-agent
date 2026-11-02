package agent

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/datadir"
)

// The status book's tests. Every test uses its own
// fictional numbers, so the shared scratch book the
// package's TestMain provides never couples them.

func statusJournalLines(t *testing.T, messageID string) int {
	t.Helper()
	data, err := os.ReadFile(datadir.Path("statuses.jsonl"))
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"id":"`+messageID+`"`) {
			n++
		}
	}
	return n
}

func TestStatusDeliveredThenReadAdvancesRollup(t *testing.T) {
	person := "233240000901"
	id := "wamid.status-advance"
	RecordMessageStatus(MessageStatus{MessageID: id, Recipient: person, Status: "sent", Timestamp: 1700000100})
	RecordMessageStatus(MessageStatus{MessageID: id, Recipient: person, Status: "delivered", Timestamp: 1700000110})
	RecordMessageStatus(MessageStatus{MessageID: id, Recipient: person, Status: "read", Timestamp: 1700000120})

	status, at, ok := OutboundMessageStatus(id)
	if !ok || status != "read" || at.Unix() != 1700000120 {
		t.Fatalf("message fate = %q at %v (ok %v), want read at 1700000120", status, at, ok)
	}
	read, ok := LastReadAt(person)
	if !ok || read.Unix() != 1700000120 {
		t.Fatalf("last read = %v (ok %v), want 1700000120", read, ok)
	}
	// A read message was on her phone by then: the
	// delivered clock stands at the read's moment even
	// though the delivered report named an earlier one.
	delivered, ok := LastDeliveredAt(person)
	if !ok || delivered.Unix() != 1700000120 {
		t.Fatalf("last delivered = %v (ok %v), want 1700000120", delivered, ok)
	}
	if n := statusJournalLines(t, id); n != 3 {
		t.Fatalf("journal lines for the message = %d, want 3", n)
	}
}

func TestStatusDuplicateIsANoOp(t *testing.T) {
	person := "233240000902"
	id := "wamid.status-dupe"
	RecordMessageStatus(MessageStatus{MessageID: id, Recipient: person, Status: "delivered", Timestamp: 1700000200})
	RecordMessageStatus(MessageStatus{MessageID: id, Recipient: person, Status: "delivered", Timestamp: 1700000200})
	// The same report redelivered with an EARLIER stamp
	// (Meta retries and batches) must move nothing either.
	RecordMessageStatus(MessageStatus{MessageID: id, Recipient: person, Status: "delivered", Timestamp: 1700000150})

	delivered, ok := LastDeliveredAt(person)
	if !ok || delivered.Unix() != 1700000200 {
		t.Fatalf("last delivered = %v (ok %v), want 1700000200 unmoved", delivered, ok)
	}
	if n := statusJournalLines(t, id); n != 1 {
		t.Fatalf("journal lines for the message = %d, want 1 (duplicates are not journaled again)", n)
	}
}

func TestStatusStaleReportNeverPullsBackwards(t *testing.T) {
	person := "233240000903"
	id := "wamid.status-stale"
	RecordMessageStatus(MessageStatus{MessageID: id, Recipient: person, Status: "read", Timestamp: 1700000320})
	// A delivered report for the same message arrives
	// after the read, stamped earlier: the fate stays
	// read, and the read clock stays where it was.
	RecordMessageStatus(MessageStatus{MessageID: id, Recipient: person, Status: "delivered", Timestamp: 1700000310})

	status, at, ok := OutboundMessageStatus(id)
	if !ok || status != "read" || at.Unix() != 1700000320 {
		t.Fatalf("message fate = %q at %v (ok %v), want read at 1700000320", status, at, ok)
	}
	read, _ := LastReadAt(person)
	if read.Unix() != 1700000320 {
		t.Fatalf("last read = %v, want 1700000320", read)
	}
}

func TestStatusFailedRecordsItsError(t *testing.T) {
	person := "233240000904"
	id := "wamid.status-failed"
	RecordMessageStatus(MessageStatus{MessageID: id, Recipient: person, Status: "failed", Timestamp: 1700000400, ErrorCode: 131047, ErrorTitle: "Re-engagement message"})

	status, _, ok := OutboundMessageStatus(id)
	if !ok || status != "failed" {
		t.Fatalf("message fate = %q (ok %v), want failed", status, ok)
	}
	book := loadStatusBook()
	ms := book.Messages[id]
	if ms == nil || ms.ErrorCode != 131047 || ms.ErrorTitle != "Re-engagement message" {
		t.Fatalf("recorded failure = %+v, want code 131047 with its title", ms)
	}
	data, err := os.ReadFile(datadir.Path("statuses.jsonl"))
	if err != nil || !strings.Contains(string(data), `"errorCode":131047`) {
		t.Fatalf("journal does not carry the failure's error: %v", err)
	}
	// A failure is not a delivery: neither clock moves.
	if _, ok := LastDeliveredAt(person); ok {
		t.Fatal("a failed send advanced the delivered clock")
	}
	if _, ok := LastReadAt(person); ok {
		t.Fatal("a failed send advanced the read clock")
	}
}

func TestStatusUnknownMessageStillRecords(t *testing.T) {
	// A report about a message this process never sent
	// (sent before a restart, or by another process of
	// the number) is kept, keyed by its id and recipient.
	person := "233240000905"
	id := "wamid.status-unknown"
	RecordMessageStatus(MessageStatus{MessageID: id, Recipient: person, Status: "read", Timestamp: 1700000500})

	status, _, ok := OutboundMessageStatus(id)
	if !ok || status != "read" {
		t.Fatalf("message fate = %q (ok %v), want read recorded for the unknown id", status, ok)
	}
	read, ok := LastReadAt(person)
	if !ok || read.Unix() != 1700000500 {
		t.Fatalf("last read = %v (ok %v), want 1700000500", read, ok)
	}
}

func TestNoteHeardFromAdvancesOnInbound(t *testing.T) {
	person := "233240000906"
	if _, ok := LastHeardFromAt(person); ok {
		t.Fatal("a person never heard from already has a clock")
	}
	before := time.Now().Unix()
	noteHeardFrom(person)
	heard, ok := LastHeardFromAt(person)
	if !ok || heard.Unix() < before || heard.Unix() > time.Now().Unix() {
		t.Fatalf("last heard from = %v (ok %v), want about now", heard, ok)
	}
}

func TestForgetStatusesErasesThePerson(t *testing.T) {
	keep, drop := "233240000907", "233240000908"
	RecordMessageStatus(MessageStatus{MessageID: "wamid.status-keep", Recipient: keep, Status: "read", Timestamp: 1700000600})
	RecordMessageStatus(MessageStatus{MessageID: "wamid.status-drop", Recipient: drop, Status: "read", Timestamp: 1700000600})
	noteHeardFrom(drop)

	forgetStatuses(drop)

	if _, ok := LastReadAt(drop); ok {
		t.Fatal("the erased person's read clock survived")
	}
	if _, ok := LastHeardFromAt(drop); ok {
		t.Fatal("the erased person's heard-from clock survived")
	}
	if _, _, ok := OutboundMessageStatus("wamid.status-drop"); ok {
		t.Fatal("the erased person's message record survived")
	}
	if n := statusJournalLines(t, "wamid.status-drop"); n != 0 {
		t.Fatalf("journal lines for the erased person = %d, want 0", n)
	}
	if _, ok := LastReadAt(keep); !ok {
		t.Fatal("another person's record went with the erasure")
	}
}
