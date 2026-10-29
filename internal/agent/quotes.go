package agent

// Quoted replies. People answer messages by pointing at
// them: they scroll back, quote an old bubble, and reply
// "Ok", "this one", "change that". The reply's words alone
// do not say what they point at; the quote does. WhatsApp
// attaches the quoted message's id (and its sender) to the
// reply, so the agent keeps a book of every message in each
// chat, by that id: everything it sends (recorded by the
// whatsapp package's send hook, under the id Meta returns,
// or a local id in dry-run) and everything it receives
// (recorded at the door, under the id the webhook carries).
// When a reply quotes a message, the book is looked up
// before any board runs: the resolved quote — what the
// message said, WHO sent it (the shop, or the person
// herself), and what it was about (a product the words name,
// an order its reference or its payment target names) —
// becomes a fact of the turn, in quoteFactLine, and every
// board's thinking judges the reply against the quoted
// message first and the current conversation second.
//
// The book (data/messages.jsonl) is keyed by the other
// party: a quote only ever resolves inside the chat it was
// sent in, so nobody can point the agent at another person's
// messages. It is pruned per party, so an old message hours
// or days back still resolves, and a restart loses nothing.
// A quote the book does not hold (older than the book, or
// sent outside this chat) still yields its author from the
// sender WhatsApp names, but never its words: the thinking
// is told it cannot see them, and must not guess.

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// msgRecord is one line of the message book.
type msgRecord struct {
	ID     string `json:"id"`
	Party  string `json:"party"`
	Author string `json:"author"` // "agent" or "user"
	Body   string `json:"body"`
	Kind   string `json:"kind,omitempty"`
	URL    string `json:"url,omitempty"`
	At     string `json:"at"`
}

var (
	msgMu        sync.Mutex
	msgIndex     map[string]msgRecord
	msgOrder     []string
	msgIndexFile string
)

// keepPerParty is how many of a chat's most recent messages
// the book holds on to: far more than any quote reaches back
// across in practice, bounded so the book cannot grow without
// limit under viral pressure.
const keepPerParty = 300

func msgBookFile() string { return datadir.Path("messages.jsonl") }

// loadMsgIndex returns the in-memory index over the book
// file, (re)loading it when the file in play changes (the
// data directory moves between tests and tools). The caller
// holds msgMu.
func loadMsgIndex() map[string]msgRecord {
	f := msgBookFile()
	if msgIndex != nil && msgIndexFile == f {
		return msgIndex
	}
	msgIndex = map[string]msgRecord{}
	msgOrder = nil
	if data, err := os.ReadFile(f); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var rec msgRecord
			if json.Unmarshal([]byte(line), &rec) == nil && rec.ID != "" {
				if _, dup := msgIndex[rec.ID]; !dup {
					msgOrder = append(msgOrder, rec.ID)
				}
				msgIndex[rec.ID] = rec
			}
		}
	}
	msgIndexFile = f
	return msgIndex
}

// recordMsg writes one message into the book. A message
// already recorded under its id is not written twice.
func recordMsg(rec msgRecord) {
	if rec.ID == "" || rec.Party == "" {
		return
	}
	msgMu.Lock()
	defer msgMu.Unlock()
	idx := loadMsgIndex()
	if _, seen := idx[rec.ID]; seen {
		return
	}
	if rec.At == "" {
		rec.At = time.Now().UTC().Format(time.RFC3339)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	f, err := os.OpenFile(msgBookFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()
	idx[rec.ID] = rec
	msgOrder = append(msgOrder, rec.ID)
	if len(msgOrder) > keepPerParty*20 {
		compactMsgBook()
	}
}

// compactMsgBook rewrites the book keeping only each party's
// most recent messages. The caller holds msgMu.
func compactMsgBook() {
	counts := map[string]int{}
	keep := map[string]bool{}
	for i := len(msgOrder) - 1; i >= 0; i-- {
		rec := msgIndex[msgOrder[i]]
		if counts[rec.Party] < keepPerParty {
			counts[rec.Party]++
			keep[rec.ID] = true
		}
	}
	var b strings.Builder
	var order []string
	for _, id := range msgOrder {
		if !keep[id] {
			delete(msgIndex, id)
			continue
		}
		if line, err := json.Marshal(msgIndex[id]); err == nil {
			b.Write(line)
			b.WriteByte('\n')
			order = append(order, id)
		}
	}
	msgOrder = order
	_ = os.WriteFile(msgBookFile(), []byte(b.String()), 0o644)
}

// lookupMsg finds one recorded message by id, inside one
// party's chat only: a quote resolves in the conversation it
// was sent in, never across people.
func lookupMsg(id, party string) (msgRecord, bool) {
	if id == "" {
		return msgRecord{}, false
	}
	msgMu.Lock()
	defer msgMu.Unlock()
	rec, ok := loadMsgIndex()[id]
	if !ok || rec.Party != party {
		return msgRecord{}, false
	}
	return rec, true
}

// init wires the sent-message recorder: every message the
// number sends, on every path (chat, settlement webhook,
// the unpaid watch), enters the book under its WhatsApp id.
func init() {
	whatsapp.OnSend = func(s whatsapp.SentRecord) {
		body := s.Body
		if s.Kind == "cta_url" && len(s.Buttons) > 0 {
			// The phone shows the body and the button's
			// words together; the book keeps what she saw.
			body = s.Body + "\n" + s.Buttons[0]
		}
		recordMsg(msgRecord{ID: s.ID, Party: s.To, Author: "agent", Body: body, Kind: s.Kind, URL: s.URL, At: s.At})
	}
}

// alreadyHandled reports whether an inbound message id is
// already in the book, meaning an earlier delivery of the
// same message was handled in full. WhatsApp delivers a
// webhook more than once when the answer is slow, and a
// photo turn is slow; the book refuses to record the same
// id twice, but until this check the handling itself ran
// again on every redelivery — the same photo opening a
// second listing, the same "last price" answer saving the
// next product too. A message with no id cannot be judged
// and is never dropped on this ground.
func alreadyHandled(id string) bool {
	if id == "" {
		return false
	}
	msgMu.Lock()
	defer msgMu.Unlock()
	_, seen := loadMsgIndex()[id]
	return seen
}

// recordInboundMsg writes one received message into the
// book, under the id the webhook carried, so the person's
// own earlier words can be quoted back and resolved too.
func recordInboundMsg(in Inbound) {
	body, kind := in.Text, "text"
	switch {
	case in.ImageID != "":
		body, kind = in.Caption, "image"
	case in.ButtonID != "":
		kind = "interactive"
		if body == "" {
			body = in.ButtonID
		}
	}
	recordMsg(msgRecord{ID: in.MessageID, Party: in.From, Author: "user", Body: body, Kind: kind})
}

// resolvedQuote is a turn's quoted message, resolved: what
// it said, who sent it, and what it was about. Known is
// false when the book does not hold the message: then only
// Author may be known (from the sender WhatsApp named), and
// its words must never be guessed.
type resolvedQuote struct {
	Known       bool
	Author      string // "agent" or "user"
	Body        string
	Kind        string
	At          string
	Product     *catalogue.Product
	ProductShop *catalogue.Merchant
	Order       *orders.Order
}

// turnQuote is the quote in play on the turn being handled.
// Turns are serialised by the session lock (the same
// discipline as currentDiscoveryFrom), so a plain package
// variable is safe.
var turnQuote *resolvedQuote

// resolveQuote turns an inbound message's quote pointer
// into the turn's resolved quote, or nil when the message
// quotes nothing.
func resolveQuote(in Inbound) *resolvedQuote {
	if in.ContextID == "" {
		return nil
	}
	q := &resolvedQuote{}
	// The author WhatsApp itself names: the business number
	// means the shop sent it; the sender's own number means
	// she is quoting her own earlier words.
	if in.ContextFrom != "" {
		switch {
		case agentNumber() != "" && digitsOnly(in.ContextFrom) == agentNumber():
			q.Author = "agent"
		case digitsOnly(in.ContextFrom) == digitsOnly(in.From):
			q.Author = "user"
		}
	}
	rec, ok := lookupMsg(in.ContextID, in.From)
	if !ok {
		return q
	}
	q.Known = true
	q.Author = rec.Author // the book outranks the pointer
	q.Body = rec.Body
	q.Kind = rec.Kind
	q.At = rec.At
	q.Product, q.ProductShop = productNamedIn(rec.Body)
	if ref := orderRefRe.FindString(rec.Body); ref != "" {
		q.Order = orders.Default.GetOrder(strings.ToUpper(ref))
	}
	if q.Order == nil && rec.URL != "" {
		if ref := strings.TrimPrefix(rec.URL, "https://pay.seevplus.com/"); ref != rec.URL && ref != "" {
			for _, o := range orders.Default.All() {
				oo := o
				if oo.ProviderRef != nil && *oo.ProviderRef == ref {
					q.Order = &oo
					break
				}
			}
		}
	}
	return q
}

func agentNumber() string {
	return digitsOnly(os.Getenv("AGENT_WHATSAPP_NUMBER"))
}

// productNamedIn finds the product a message is about, by
// its full name standing in the message's words, across
// every shop (a quote can point at a discovery line for a
// shop the person is not standing in). The longest name
// wins, so a longer product name is never shadowed by a
// shorter one it contains.
func productNamedIn(body string) (*catalogue.Product, *catalogue.Merchant) {
	lower := strings.ToLower(body)
	var best *catalogue.Product
	var bestShop *catalogue.Merchant
	for _, m := range catalogue.ListMerchants() {
		shop := m
		for _, p := range catalogue.ListProducts(m.ID) {
			pp := p
			if strings.Contains(lower, strings.ToLower(pp.Name)) {
				if best == nil || len(pp.Name) > len(best.Name) {
					best, bestShop = &pp, &shop
				}
			}
		}
	}
	return best, bestShop
}

// quoteClock renders a book timestamp as the time of day a
// person would name ("09:02"), or "" when it cannot be read.
func quoteClock(at string) string {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05.000Z"} {
		if t, err := time.Parse(layout, at); err == nil {
			return t.Format("15:04")
		}
	}
	return ""
}

// quoteFactLine states the turn's quote as a fact for a
// thinking step: the quoted words, who sent them, when, and
// what they were about, with the reasoning order spelled
// out — the quoted message first, the current conversation
// second. It returns "" when the turn quotes nothing.
func quoteFactLine() string {
	q := turnQuote
	if q == nil {
		return ""
	}
	if !q.Known {
		who := "a message whose sender is not recorded"
		switch q.Author {
		case "agent":
			who = "a message the shop sent"
		case "user":
			who = "a message she sent herself"
		}
		return "She is replying to " + who + ", but this chat's record does not hold that message's words (it is older than the record, or was sent outside this chat), so you cannot see what it said. Do not guess at its content. If her reply does not stand on its own without it, ask her one short question about what she is pointing at.\n"
	}
	var b strings.Builder
	who := "the shop"
	if q.Author == "user" {
		who = "herself"
	}
	b.WriteString("She is replying to a message sent by " + who + " earlier in this chat")
	if clock := quoteClock(q.At); clock != "" {
		b.WriteString(" (at " + clock + ")")
	}
	b.WriteString(": \"" + q.Body + "\"")
	if q.Product != nil && q.ProductShop != nil {
		b.WriteString(" That message is about the " + q.Product.Name + " at " + q.ProductShop.Name + " (shop code " + q.ProductShop.ShopCode + "), priced " + money.GHS(q.Product.ListPrice) + ".")
	}
	if q.Order != nil {
		b.WriteString(" That message is about order " + q.Order.Reference + " (the " + q.Order.ProductName + ", " + money.GHS(q.Order.Charged()) + ", status " + q.Order.Status + ").")
	}
	b.WriteString(" Judge her reply against that quoted message FIRST, and against the current conversation second.\n")
	return b.String()
}

// quotedProductIn returns the product of this shop the
// turn's quote is about, or nil.
func quotedProductIn(shopID string) *catalogue.Product {
	q := turnQuote
	if q == nil || !q.Known || q.Product == nil || q.ProductShop == nil || q.ProductShop.ID != shopID {
		return nil
	}
	return catalogue.FindByID(shopID, q.Product.ID)
}

// applyQuotedFocus points a session's last-viewed item at
// the product its quote is about (same shop only): a bare
// offer, a bare "Ok" or a picture ask after quoting a
// product's bubble is about that product, however far the
// conversation has wandered since it was sent.
func applyQuotedFocus(s *Session, shopID string) {
	p := quotedProductIn(shopID)
	if p == nil || s == nil {
		return
	}
	if s.LastViewed == nil || s.LastViewed.ID != p.ID {
		s.LastViewed = p
	}
}

// quotedOrderFor returns the order the turn's quote is
// about when it is this buyer's order in this shop, read
// fresh from the book, or nil.
func quotedOrderFor(shopID, from string) *orders.Order {
	q := turnQuote
	if q == nil || !q.Known || q.Order == nil {
		return nil
	}
	o := orders.Default.GetOrder(q.Order.Reference)
	if o == nil || o.MerchantID != shopID || o.From != from {
		return nil
	}
	return o
}

// quotedUnpaidOrder is quotedOrderFor for an order still
// waiting on its money.
func quotedUnpaidOrder(shopID, from string) *orders.Order {
	if o := quotedOrderFor(shopID, from); o != nil && o.Unpaid() {
		return o
	}
	return nil
}
