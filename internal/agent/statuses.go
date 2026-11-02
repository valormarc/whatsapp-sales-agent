package agent

// The status book: what Meta reports back about the
// messages this number sends — when each one was
// delivered to her phone, when she read it, and when a
// send failed and why — kept per message, and rolled up
// per person as three clocks: last delivered, last
// read, last heard from.
//
// Meta posts these reports to the same webhook that
// carries inbound messages, in the value's statuses
// array, and until this book existed the webhook's
// payload did not even declare the field, so every
// report was dropped unseen and Stall knew nothing
// about the fate of anything it sent: its reminders ran
// on time alone, blind to whether the note had ever
// been opened. The capture is deliberately only a
// capture: no reply, reminder or dispatch reads this
// book yet. A later build may act on it; this one makes
// sure the facts exist to act on, because Meta sends
// each report once and an unrecorded history cannot be
// recovered later.
//
// Two honesty rules are built in. A read stands in this
// book only when Meta reports one: nothing here infers
// reading from a reply, a delivery or anything else,
// and a person who has switched read receipts off in
// her own WhatsApp settings will simply never show a
// read — the book's silence says "not reported", never
// "not read". And no presence is recorded or claimed
// anywhere: the platform gives businesses no online or
// last-seen signal, so the nearest truthful facts are
// the three clocks kept here.

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/valormarc/hackathon-agent/internal/datadir"
)

// MessageStatus is one status report from Meta about a
// message this number sent, as the webhook carries it:
// the message's own id, who it went to, the status word
// (sent, delivered, read, failed), Meta's timestamp,
// and for a failure the provider's error code and title.
type MessageStatus struct {
	MessageID  string
	Recipient  string
	Status     string
	Timestamp  int64 // Meta's unix seconds; 0 when the webhook carried none readable
	ErrorCode  int
	ErrorTitle string
}

// personStatus is one person's three clocks, as unix
// seconds. A zero clock was never observed.
type personStatus struct {
	LastDeliveredAt int64 `json:"lastDeliveredAt,omitempty"`
	LastReadAt      int64 `json:"lastReadAt,omitempty"`
	LastHeardFromAt int64 `json:"lastHeardFromAt,omitempty"`
}

// sentStatus is the recorded fate of one outbound
// message: who it went to, the furthest status Meta has
// reported for it and when, every status word reported
// (the idempotency record: Meta delivers the same
// report more than once, and a status already in Seen
// changes nothing when it arrives again), and the
// failure's error when the furthest report is one.
type sentStatus struct {
	To         string   `json:"to"`
	Status     string   `json:"status"`
	At         int64    `json:"at"`
	Seen       []string `json:"seen,omitempty"`
	ErrorCode  int      `json:"errorCode,omitempty"`
	ErrorTitle string   `json:"errorTitle,omitempty"`
}

type statusBook struct {
	People   map[string]*personStatus `json:"people"`
	Messages map[string]*sentStatus   `json:"messages"`
}

var statusMu sync.Mutex

func statusBookFile() string    { return datadir.Path("statuses.json") }
func statusJournalFile() string { return datadir.Path("statuses.jsonl") }

// loadStatusBook reads the book; the caller holds
// statusMu. A missing file is a fresh book, never an
// error (loadBookJSON's discipline covers the rest:
// corrupt bytes are preserved, the last known good copy
// restored, and a double failure poisons the path so
// later saves refuse rather than cementing the loss).
func loadStatusBook() *statusBook {
	book := &statusBook{}
	loadBookJSON(statusBookFile(), book)
	if book.People == nil {
		book.People = map[string]*personStatus{}
	}
	if book.Messages == nil {
		book.Messages = map[string]*sentStatus{}
	}
	return book
}

func saveStatusBook(book *statusBook) {
	// The book owns its directory, as the want book's
	// save does: a status report can be the first thing
	// a fresh process ever writes.
	_ = os.MkdirAll(datadir.Dir(), 0o700)
	saveBookJSON(statusBookFile(), book)
}

func (b *statusBook) person(phone string) *personStatus {
	p := b.People[phone]
	if p == nil {
		p = &personStatus{}
		b.People[phone] = p
	}
	return p
}

// statusRank orders the progress statuses: a later
// report about a message must never pull its recorded
// fate backwards, and a stale duplicate arriving out of
// order (Meta batches and retries) meets this order,
// not the wall clock. A failure stands outside the
// order: it ends an attempt, and is handled on its own
// branch in RecordMessageStatus.
func statusRank(status string) int {
	switch status {
	case "sent":
		return 1
	case "delivered":
		return 2
	case "read":
		return 3
	}
	return 0
}

// statusJournalRecord is one line of the status
// journal: the append-only trail of every report
// applied, in the order they were applied.
type statusJournalRecord struct {
	At         string `json:"at"` // the report's own moment, ISO
	MessageID  string `json:"id"`
	To         string `json:"to"`
	Status     string `json:"status"`
	Timestamp  int64  `json:"ts"`
	ErrorCode  int    `json:"errorCode,omitempty"`
	ErrorTitle string `json:"errorTitle,omitempty"`
}

// The journal rotates at the outbox's ceiling, with the
// same sibling count: the trail stays inspectable and
// the total stays bounded.
const statusJournalCeiling = 8 << 20 // 8 MiB
const statusJournalSiblings = 3

// appendStatusJournal writes one applied report to the
// journal. The caller holds statusMu. Like the outbox,
// a journal failure is silent: the book is the working
// record, the journal its trail.
func appendStatusJournal(rec statusJournalRecord) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
		return
	}
	path := statusJournalFile()
	_ = os.MkdirAll(datadir.Dir(), 0o700)
	if st, err := os.Stat(path); err == nil && st.Size()+int64(buf.Len()) > statusJournalCeiling {
		for i := statusJournalSiblings - 1; i >= 1; i-- {
			_ = os.Rename(path+"."+string(rune('0'+i)), path+"."+string(rune('0'+i+1)))
		}
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(buf.Bytes())
}

// RecordMessageStatus is the recorder the webhook
// routes every status report to. It joins the report
// to the outbound message it names — the same id the
// send path wrote into the outbox and the message book
// — and records the event whether or not that message
// is otherwise known here, so a report about a message
// sent before a restart, or by another process of this
// number, is kept rather than dropped. Reports are
// idempotent: a status already recorded for the
// message changes nothing and is not journaled again,
// and no clock ever moves backwards.
func RecordMessageStatus(ev MessageStatus) {
	if ev.MessageID == "" || ev.Recipient == "" || ev.Status == "" {
		return
	}
	ts := ev.Timestamp
	if ts <= 0 {
		ts = time.Now().Unix()
	}
	statusMu.Lock()
	defer statusMu.Unlock()
	book := loadStatusBook()
	ms := book.Messages[ev.MessageID]
	if ms == nil {
		ms = &sentStatus{To: ev.Recipient}
		book.Messages[ev.MessageID] = ms
	}
	if ms.To == "" {
		ms.To = ev.Recipient
	}
	for _, s := range ms.Seen {
		if s == ev.Status {
			return // a duplicate report: nothing moves, nothing is written again
		}
	}
	ms.Seen = append(ms.Seen, ev.Status)
	switch {
	case ev.Status == "failed":
		// A failure ends the attempt unless the message
		// is already known to have arrived: a read or
		// delivered message does not unhappen because a
		// stray failure report follows it.
		if statusRank(ms.Status) < statusRank("delivered") {
			ms.Status = "failed"
			ms.At = ts
		}
		ms.ErrorCode = ev.ErrorCode
		ms.ErrorTitle = ev.ErrorTitle
	case statusRank(ev.Status) > statusRank(ms.Status):
		ms.Status = ev.Status
		ms.At = ts
	}
	p := book.person(ev.Recipient)
	switch ev.Status {
	case "delivered":
		if ts > p.LastDeliveredAt {
			p.LastDeliveredAt = ts
		}
	case "read":
		if ts > p.LastReadAt {
			p.LastReadAt = ts
		}
		// A message she read was on her phone by then,
		// whether or not its delivered report was ever
		// seen here.
		if ts > p.LastDeliveredAt {
			p.LastDeliveredAt = ts
		}
	}
	saveStatusBook(book)
	appendStatusJournal(statusJournalRecord{
		At:         time.Unix(ts, 0).UTC().Format("2006-01-02T15:04:05.000Z"),
		MessageID:  ev.MessageID,
		To:         ev.Recipient,
		Status:     ev.Status,
		Timestamp:  ts,
		ErrorCode:  ev.ErrorCode,
		ErrorTitle: ev.ErrorTitle,
	})
}

// noteHeardFrom advances one person's last-heard-from
// clock to now. It is called on every inbound turn (see
// handleInboundTurn): her speaking is itself one of the
// three facts this book keeps.
func noteHeardFrom(from string) {
	if from == "" {
		return
	}
	now := time.Now().Unix()
	statusMu.Lock()
	defer statusMu.Unlock()
	book := loadStatusBook()
	p := book.person(from)
	if now > p.LastHeardFromAt {
		p.LastHeardFromAt = now
		saveStatusBook(book)
	}
}

// The accessors below are the book's read face, kept
// for the behaviours a later build may add: each
// returns the recorded moment and whether one stands
// recorded at all, so a caller can always tell a fact
// from an absence. Nothing in the conversation reads
// them yet.

// LastDeliveredAt returns when a message last landed
// on the person's phone, as Meta reported it.
func LastDeliveredAt(person string) (time.Time, bool) {
	statusMu.Lock()
	defer statusMu.Unlock()
	p := loadStatusBook().People[person]
	if p == nil || p.LastDeliveredAt <= 0 {
		return time.Time{}, false
	}
	return time.Unix(p.LastDeliveredAt, 0).UTC(), true
}

// LastReadAt returns when the person last read a
// message from this number, as Meta reported it. An
// absent read is "not reported", never "not read".
func LastReadAt(person string) (time.Time, bool) {
	statusMu.Lock()
	defer statusMu.Unlock()
	p := loadStatusBook().People[person]
	if p == nil || p.LastReadAt <= 0 {
		return time.Time{}, false
	}
	return time.Unix(p.LastReadAt, 0).UTC(), true
}

// LastHeardFromAt returns when the person last sent
// this number a message that reached a turn.
func LastHeardFromAt(person string) (time.Time, bool) {
	statusMu.Lock()
	defer statusMu.Unlock()
	p := loadStatusBook().People[person]
	if p == nil || p.LastHeardFromAt <= 0 {
		return time.Time{}, false
	}
	return time.Unix(p.LastHeardFromAt, 0).UTC(), true
}

// OutboundMessageStatus returns the furthest status
// Meta has reported for one outbound message, and when.
func OutboundMessageStatus(messageID string) (string, time.Time, bool) {
	statusMu.Lock()
	defer statusMu.Unlock()
	ms := loadStatusBook().Messages[messageID]
	if ms == nil || ms.Status == "" {
		return "", time.Time{}, false
	}
	return ms.Status, time.Unix(ms.At, 0).UTC(), true
}

// forgetStatuses drops one person's standing record
// for a "delete my data" request: her rollup entry,
// every per-message record addressed to her, and her
// lines of the status journal and its rotated siblings.
// The reports were about her phone's handling of her
// messages; they are hers to have deleted like the
// rest.
func forgetStatuses(person string) {
	if person == "" {
		return
	}
	statusMu.Lock()
	defer statusMu.Unlock()
	book := loadStatusBook()
	changed := false
	if _, ok := book.People[person]; ok {
		delete(book.People, person)
		changed = true
	}
	for id, ms := range book.Messages {
		if ms.To == person {
			delete(book.Messages, id)
			changed = true
		}
	}
	if changed {
		saveStatusBook(book)
	}
	base := statusJournalFile()
	purgeStatusJournalFile(base, person)
	for i := 1; i <= statusJournalSiblings; i++ {
		purgeStatusJournalFile(base+"."+string(rune('0'+i)), person)
	}
}

func purgeStatusJournalFile(path, person string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var kept []string
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec statusJournalRecord
		if json.Unmarshal([]byte(line), &rec) == nil && rec.To == person {
			n++
			continue
		}
		kept = append(kept, line)
	}
	if n > 0 {
		_ = os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o600)
	}
}
