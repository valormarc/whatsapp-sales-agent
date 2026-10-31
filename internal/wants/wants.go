// Package wants is the book of buyer wants: the things buyers asked
// a shop for that the shop does not stock.
//
// It exists because of how real buying ends (my own chats):
// a buyer asks for something the seller does not have,
// says "anytime you get them, alert me, I'll buy", and the sale dies
// quietly because no alert ever comes. So the agent writes the want
// down, per shop, and when the merchant stocks a matching product the
// waiting buyers are told. JSON file, same pattern as the order book
// and the day book.
package wants

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/valormarc/hackathon-agent/internal/datadir"
)

// Want is one recorded request: which shop was asked, who asked, what
// they asked for (their own words), and the day they asked. Alerted
// is set once the buyer has been told a matching product is in, so
// nobody is alerted twice about the same want.
type Want struct {
	MerchantID string `json:"merchantId"`
	Buyer      string `json:"buyer"`
	Text       string `json:"text"`
	Date       string `json:"date"`
	Alerted    bool   `json:"alerted,omitempty"`
}

func marshalJSON(v any) ([]byte, error) {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(buf.String(), "\n")), nil
}

// Book is a want book stored in one JSON file. Every
// public method holds the mutex across its whole load,
// modify, save cycle: the book is written from buyer turns
// and from merchant stocking turns alike.
type Book struct {
	file string
	mu   sync.Mutex
	// poisoned is set when a load met a book that would not
	// parse and no last-known-good copy could be restored:
	// saves are then refused rather than cementing the empty
	// read over the real book. Guarded by mu, through loadAll
	// and saveAll.
	poisoned bool
}

// New returns a want book backed by the given file.
func New(file string) *Book {
	return &Book{file: file}
}

// Default is the shared want book, backed by data/wants.json.
var Default = New(datadir.Path("wants.json"))

// today returns the trading date, UTC, formatted YYYY-MM-DD, the same
// convention as the day book (Accra is UTC+0).
func today() string {
	return time.Now().UTC().Format("2006-01-02")
}

type store struct {
	Wants []Want `json:"wants"`
}

// loadAll reads the book; the caller holds the lock. The
// corrupt-file discipline is the platform's one shape (the
// fifth audit found this book still on the preserve-only
// version the fourth audit condemned in orders): a file
// that exists but does not parse is preserved under a
// single .corrupt name and shouted about; the load restores
// from the last-known-good copy every save leaves beside
// the book and repairs the main file with those bytes; and
// only when both copies fail does the book read empty —
// poisoned, with saves refused until a load parses again.
func (b *Book) loadAll() []Want {
	// Both failure roads restore or poison (the sixth
	// audit); only an absent file is a fresh book.
	restore := func() ([]Want, bool) {
		bak, berr := os.ReadFile(b.file + ".bak")
		if berr != nil {
			return nil, false
		}
		var restored store
		if uerr := json.Unmarshal(bak, &restored); uerr != nil {
			return nil, false
		}
		tmp := b.file + ".tmp"
		if werr := os.WriteFile(tmp, bak, 0o600); werr == nil {
			_ = os.Rename(tmp, b.file)
		}
		b.poisoned = false
		return restored.Wants, true
	}
	data, err := os.ReadFile(b.file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		if restored, ok := restore(); ok {
			fmt.Fprintf(os.Stderr, "wants: %s could not be read (%v); restored from the last known good copy\n", b.file, err)
			return restored
		}
		fmt.Fprintf(os.Stderr, "wants: %s could not be read (%v) and no good copy could be restored; read as empty; saves are refused until the book loads again\n", b.file, err)
		b.poisoned = true
		return nil
	}
	var s store
	if err := json.Unmarshal(data, &s); err != nil {
		bad := b.file + ".corrupt"
		_ = os.WriteFile(bad, data, 0o600)
		if restored, ok := restore(); ok {
			fmt.Fprintf(os.Stderr, "wants: %s does not parse (%v); preserved as %s and restored from the last known good copy\n", b.file, err, bad)
			return restored
		}
		fmt.Fprintf(os.Stderr, "wants: %s does not parse (%v); preserved as %s and read as empty; saves are refused until the book parses again\n", b.file, err, bad)
		b.poisoned = true
		return nil
	}
	b.poisoned = false
	return s.Wants
}

// saveAll writes the book atomically (temporary file,
// renamed over), owner-only, and refreshes the
// last-known-good copy with the same bytes. A poisoned
// book refuses: writing the empty read over the real file
// would cement the loss the load shouted about. The caller
// holds the lock.
func (b *Book) saveAll(all []Want) {
	if b.poisoned {
		fmt.Fprintf(os.Stderr, "wants: refusing to save over %s: the last load did not parse and no good copy could be restored\n", b.file)
		return
	}
	_ = os.MkdirAll(filepath.Dir(b.file), 0o700)
	_ = os.MkdirAll(datadir.Dir(), 0o700)
	if data, err := marshalJSON(store{Wants: all}); err == nil {
		tmp := b.file + ".tmp"
		if err := os.WriteFile(tmp, data, 0o600); err == nil {
			if err := os.Rename(tmp, b.file); err == nil {
				bakTmp := b.file + ".bak.tmp"
				if err := os.WriteFile(bakTmp, data, 0o600); err == nil {
					_ = os.Rename(bakTmp, b.file+".bak")
				}
			}
		}
	}
}

func normalise(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(text)), " ")
}

// Record writes a buyer's want into one shop's book. The same buyer
// asking the same thing twice in a day is written once: her words are
// compared trimmed and case-insensitively. It reports whether a new
// entry was added.
func (b *Book) Record(merchantID, buyer, text string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	text = strings.TrimSpace(text)
	if merchantID == "" || buyer == "" || text == "" {
		return false
	}
	all := b.loadAll()
	date := today()
	for _, w := range all {
		if w.MerchantID == merchantID && w.Buyer == buyer &&
			w.Date == date && normalise(w.Text) == normalise(text) {
			return false
		}
	}
	all = append(all, Want{MerchantID: merchantID, Buyer: buyer, Text: text, Date: date})
	b.saveAll(all)
	return true
}

// List returns one shop's wants, in the order they were recorded, and
// no other shop's: a merchant only ever sees what her own buyers
// asked her for.
// Forget removes every want one buyer has recorded, in
// every shop, for a "delete my data" request: a deleted
// buyer must never be alerted (that is, messaged) later
// because a want she left behind matched a stocking.
// Returns how many wants were removed.
func (b *Book) Forget(buyer string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	all := b.loadAll()
	kept := all[:0]
	n := 0
	for _, w := range all {
		if w.Buyer == buyer {
			n++
			continue
		}
		kept = append(kept, w)
	}
	if n > 0 {
		b.saveAll(kept)
	}
	return n
}

func (b *Book) List(merchantID string) []Want {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []Want
	for _, w := range b.loadAll() {
		if w.MerchantID == merchantID {
			out = append(out, w)
		}
	}
	return out
}

// MatchesWant reports whether a newly stocked product answers a
// recorded want: a case-insensitive substring match either way, so a
// want written as "I need a tv" matches a product called "TV", and a
// want written as "tilapia" matches "Smoked Tilapia". A want is the
// buyer's whole message, so the match also runs word by word: any
// significant word of the product's name appearing in her words (or
// the other way round) counts, which is how "tilapia needed" meets
// "Smoked Tilapia".
func MatchesWant(wantText, productName string) bool {
	w := normalise(wantText)
	p := normalise(productName)
	if w == "" || p == "" {
		return false
	}
	if strings.Contains(w, p) || strings.Contains(p, w) {
		return true
	}
	// Word agreement, on substantially ALL of the product
	// name's significant words (the sixth audit): the old
	// rule matched on any ONE shared word, so a want for a
	// beaded bag was consumed by a leather bag's restock —
	// the shared word "bag" spent her only alert. Now the
	// want's words must cover the product's name: every
	// significant word of it standing whole in her text.
	// A one-word product ("TV") still matches on that word
	// standing whole in her ask.
	wantWords := map[string]bool{}
	for _, word := range strings.Fields(w) {
		wantWords[word] = true
	}
	productWords := map[string]bool{}
	significant := 0
	covered := 0
	for _, word := range strings.Fields(p) {
		if len(word) < 2 || word == "the" || word == "and" || word == "for" {
			continue
		}
		productWords[word] = true
		significant++
		if wantWords[word] {
			covered++
		}
	}
	if significant > 0 && covered == significant {
		return true
	}
	// The reverse partial: her ask names PART of the
	// product's name ("tilapia" for the Smoked Tilapia) —
	// a match when the shared word is real and her ask
	// carries no OTHER product-describing word the
	// product does not cover. That uncovered-word test is
	// what keeps the audit's case out: "beaded" stands
	// uncovered in "the beaded bag", so the leather bag
	// does not spend her alert; "needed" in "tilapia
	// needed" is request framing, not description.
	if covered == 0 {
		return false
	}
	for _, word := range strings.Fields(w) {
		if len(word) < 2 || productWords[word] || wantFiller[word] {
			continue
		}
		return false
	}
	return true
}

// wantFiller words frame a request without describing a
// product; they never make an ask more specific.
var wantFiller = map[string]bool{
	"the": true, "and": true, "for": true, "you": true,
	"have": true, "has": true, "need": true, "needed": true,
	"needs": true, "want": true, "wants": true, "looking": true,
	"please": true, "any": true, "some": true, "get": true,
	"got": true, "with": true, "that": true, "this": true,
}

// MatchAndMark returns one shop's unalerted wants that a product
// answers, and marks them alerted in the book, so each want produces
// at most one alert however many matching products arrive later.
func (b *Book) MatchAndMark(merchantID, productName string) []Want {
	b.mu.Lock()
	defer b.mu.Unlock()
	all := b.loadAll()
	var matched []Want
	changed := false
	for i, w := range all {
		if w.MerchantID != merchantID || w.Alerted {
			continue
		}
		if MatchesWant(w.Text, productName) {
			all[i].Alerted = true
			matched = append(matched, all[i])
			changed = true
		}
	}
	if changed {
		b.saveAll(all)
	}
	return matched
}
