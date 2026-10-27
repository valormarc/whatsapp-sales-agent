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
	"os"
	"path/filepath"
	"strings"
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

// Book is a want book stored in one JSON file.
type Book struct {
	file string
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

func (b *Book) loadAll() []Want {
	data, err := os.ReadFile(b.file)
	if err != nil {
		return nil
	}
	var s store
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	return s.Wants
}

func (b *Book) saveAll(all []Want) {
	_ = os.MkdirAll(filepath.Dir(b.file), 0o755)
	_ = os.MkdirAll(datadir.Dir(), 0o755)
	if data, err := marshalJSON(store{Wants: all}); err == nil {
		_ = os.WriteFile(b.file, data, 0o644)
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
func (b *Book) List(merchantID string) []Want {
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
	for _, word := range strings.Fields(p) {
		if len(word) > 3 && strings.Contains(w, word) {
			return true
		}
	}
	for _, word := range strings.Fields(w) {
		if len(word) > 3 && strings.Contains(p, word) {
			return true
		}
	}
	// Short product words ("tv") never survive the substring rules
	// inside a longer message, so a whole-word meeting counts too:
	// "tv" among her words meets the "TV" in "TV Stand".
	wantWords := map[string]bool{}
	for _, word := range strings.Fields(w) {
		wantWords[word] = true
	}
	for _, word := range strings.Fields(p) {
		if len(word) >= 2 && wantWords[word] {
			return true
		}
	}
	return false
}

// MatchAndMark returns one shop's unalerted wants that a product
// answers, and marks them alerted in the book, so each want produces
// at most one alert however many matching products arrive later.
func (b *Book) MatchAndMark(merchantID, productName string) []Want {
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
