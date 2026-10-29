package agent

// Quoted replies, end to end at the chat layer: the message
// book records both authors, a quote resolves inside its own
// chat only, an offer made while quoting an old product
// bubble lands on THAT product however far the conversation
// wandered, an unbound buyer's quoted pick takes her into
// the quoted product's shop, and a merchant quoting one
// claim alert among several settles that claim, not the
// oldest. The model is absent in these tests (setupChat
// clears the key): they prove the book, the resolution and
// the factual anchors; the thinking that reads against the
// quote is proven live in scripts/prove-quotes.js.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const shopBagAndHeels = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":3},{"id":"heels","name":"Ankle Strap Heels","description":"Pointed ankle-strap heels in mesh.","listPrice":120,"floorPrice":100,"stock":3}]}}}`

var quoteSeq uint64

// quoteSay sends one inbound message the way a phone sends a
// quoted reply: its own id, the quoted message's id, and who
// WhatsApp says sent the quoted message.
func quoteSay(t *testing.T, from, text, contextID, contextFrom string) {
	t.Helper()
	n := atomic.AddUint64(&quoteSeq, 1)
	HandleInbound(Inbound{From: from, Text: text, MessageID: fmt.Sprintf("wamid.T%d", n), ContextID: contextID, ContextFrom: contextFrom})
}

func msgBookOnDisk(t *testing.T, dir string) []msgRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "data", "messages.jsonl"))
	if err != nil {
		t.Fatalf("message book: %v", err)
	}
	var out []msgRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec msgRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("book line not JSON: %v", err)
		}
		out = append(out, rec)
	}
	return out
}

// agentMsgID finds the id of the most recent message the
// agent sent to one party whose body carries the text.
func agentMsgID(t *testing.T, dir, party, substr string) string {
	t.Helper()
	id := ""
	for _, rec := range msgBookOnDisk(t, dir) {
		if rec.Author == "agent" && rec.Party == party && strings.Contains(rec.Body, substr) {
			id = rec.ID
		}
	}
	if id == "" {
		t.Fatalf("no agent message to %s carrying %q in the book", party, substr)
	}
	return id
}

func TestQuoteBookKnowsBothAuthors(t *testing.T) {
	dir := setupChat(t, "233240000001", shopBagAndHeels)
	c := &chat{dir: dir}
	buyer := "233550007101"

	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Ankle Strap Heels")
	c.replies(t, buyer)

	// The agent's price quote is in the book, under the id it
	// was sent with, authored by the agent, in her chat.
	quoteID := agentMsgID(t, dir, buyer, "Price: GH₵120")
	rq := resolveQuote(Inbound{From: buyer, ContextID: quoteID, ContextFrom: "233209450786"})
	if rq == nil || !rq.Known || rq.Author != "agent" || rq.Product == nil || rq.Product.Name != "Ankle Strap Heels" {
		t.Fatalf("agent quote resolution = %+v, want known, agent-authored, about the heels", rq)
	}

	// Her own words are in the book too, under their own id,
	// and a quote of them resolves as hers.
	HandleInbound(Inbound{From: buyer, Text: "I will collect it myself", MessageID: "wamid.OWN1"})
	rq = resolveQuote(Inbound{From: buyer, ContextID: "wamid.OWN1", ContextFrom: buyer})
	if rq == nil || !rq.Known || rq.Author != "user" || !strings.Contains(rq.Body, "collect it myself") {
		t.Fatalf("own quote resolution = %+v, want known, user-authored, her own words", rq)
	}
}

func TestQuoteResolvesOnlyInsideItsOwnChat(t *testing.T) {
	dir := setupChat(t, "233240000001", shopBagAndHeels)
	c := &chat{dir: dir}
	buyer, other := "233550007102", "233550007103"

	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Ankle Strap Heels")
	c.replies(t, buyer)
	quoteID := agentMsgID(t, dir, buyer, "Price: GH₵120")

	// The same id quoted by a different person resolves to
	// nothing: her book is not his to point at.
	rq := resolveQuote(Inbound{From: other, ContextID: quoteID, ContextFrom: "233209450786"})
	if rq == nil || rq.Known {
		t.Fatalf("cross-chat resolution = %+v, want unknown", rq)
	}
}

func TestUnknownQuoteKeepsItsAuthorAndAdmitsBlindness(t *testing.T) {
	setupChat(t, "233240000001", shopBagAndHeels)
	t.Setenv("AGENT_WHATSAPP_NUMBER", "233209450786")
	buyer := "233550007104"

	rq := resolveQuote(Inbound{From: buyer, ContextID: "wamid.GONE", ContextFrom: "233209450786"})
	if rq == nil || rq.Known || rq.Author != "agent" {
		t.Fatalf("unknown agent quote = %+v, want unknown but agent-authored", rq)
	}
	rq = resolveQuote(Inbound{From: buyer, ContextID: "wamid.GONE2", ContextFrom: buyer})
	if rq == nil || rq.Known || rq.Author != "user" {
		t.Fatalf("unknown own quote = %+v, want unknown but user-authored", rq)
	}

	// And the fact the thinking is given says plainly that
	// the words cannot be seen and must not be guessed.
	turnQuote = &resolvedQuote{Author: "agent"}
	defer func() { turnQuote = nil }()
	line := quoteFactLine()
	if !strings.Contains(line, "does not hold") || !strings.Contains(line, "Do not guess") {
		t.Fatalf("unknown quote fact line = %q, want the blindness stated", line)
	}
}

func TestQuotedOfferLandsOnTheQuotedProduct(t *testing.T) {
	dir := setupChat(t, "233240000001", shopBagAndHeels)
	c := &chat{dir: dir}
	buyer := "233550007105"

	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Ankle Strap Heels")
	c.replies(t, buyer)
	heelsQuote := agentMsgID(t, dir, buyer, "Price: GH₵120")

	// The conversation wanders: she looks at the bag, and the
	// bag is what she was last viewing.
	c.say(t, buyer, "Handmade Beaded Bag")
	c.replies(t, buyer)

	// She quotes the OLD heels bubble and offers on it: the
	// haggle that answers is the heels' haggle, not the
	// bag's, though the bag was the last thing she viewed.
	quoteSay(t, buyer, "100", heelsQuote, "233209450786")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Ankle Strap Heels") || strings.Contains(got, "Beaded Bag") {
		t.Fatalf("quoted offer replies = %q, want the haggle on the quoted heels", got)
	}
	s := sessions[sessionKey("demo", buyer)]
	if s == nil || s.Stage != "haggling" || s.Product == nil || s.Product.Name != "Ankle Strap Heels" {
		t.Fatalf("session after quoted offer = %+v, want a haggle on the heels", s)
	}
}

func TestQuotedOfferRetargetsAnOpenHaggle(t *testing.T) {
	dir := setupChat(t, "233240000001", shopBagAndHeels)
	c := &chat{dir: dir}
	buyer := "233550007106"

	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Ankle Strap Heels")
	c.replies(t, buyer)
	heelsQuote := agentMsgID(t, dir, buyer, "Price: GH₵120")

	// A haggle opens on the bag.
	c.say(t, buyer, "How much is the Handmade Beaded Bag?")
	c.replies(t, buyer)

	// Quoting the heels with an offer moves the haggle
	// itself: the session's product is the heels now.
	quoteSay(t, buyer, "110", heelsQuote, "233209450786")
	got := strings.Join(c.replies(t, buyer), "\n")
	s := sessions[sessionKey("demo", buyer)]
	if s == nil || s.Product == nil || s.Product.Name != "Ankle Strap Heels" {
		t.Fatalf("session after quoted offer = %+v, want the haggle on the heels", s)
	}
	if !strings.Contains(got, "Heels") {
		t.Fatalf("quoted haggle replies = %q, want them about the heels", got)
	}
}

func TestQuotedOwnWordsCorrectThePlace(t *testing.T) {
	dir := setupChat(t, "233240000001", shopBagAndHeels)
	c := &chat{dir: dir}
	buyer := "233550007108"

	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	HandleInbound(Inbound{From: buyer, Text: "I am in Accra", MessageID: "wamid.LOC1"})
	c.replies(t, buyer)
	s := sessions[sessionKey("demo", buyer)]
	if s == nil || s.Location != "Accra" {
		t.Fatalf("session after stating a place = %+v, want Accra kept", s)
	}

	// A new place named WITHOUT the quote does not rewrite
	// the kept one (the standing rule), but the same words
	// said against a quote of her own statement do: she is
	// correcting herself, and the quote is what says so.
	quoteSay(t, buyer, "Change that, I am in Kumasi", "wamid.LOC1", buyer)
	c.replies(t, buyer)
	if s.Location != "Kumasi" {
		t.Fatalf("session after quoted correction = %+v, want Kumasi", s)
	}
}

func TestUnboundQuotedPickBindsTheQuotedShop(t *testing.T) {
	dir := setupChat(t, "233240000001", shopBagAndHeels)
	c := &chat{dir: dir}
	buyer := "233550007107"

	// Discovery shows her the heels, one match, unbound.
	c.say(t, buyer, "Do you have heels?")
	c.replies(t, buyer)
	heelsQuote := agentMsgID(t, dir, buyer, "Price: GH₵120")
	if bindings[buyer] != "" {
		t.Fatalf("a shown result bound her before any pick: %q", bindings[buyer])
	}

	// Her "Ok" names nothing and picks nothing the shown
	// list can read; the quote is what says which product
	// she means, and it takes her into the shop.
	quoteSay(t, buyer, "Ok", heelsQuote, "233209450786")
	c.replies(t, buyer)
	if bindings[buyer] != "demo" {
		t.Fatalf("bindings after quoted Ok = %q, want demo", bindings[buyer])
	}
	s := sessions[sessionKey("demo", buyer)]
	if s == nil || s.LastViewed == nil || s.LastViewed.Name != "Ankle Strap Heels" {
		t.Fatalf("session after quoted pick = %+v, want the heels in view", s)
	}
}

func TestMerchantQuotingOneClaimSettlesThatClaim(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer1, buyer2 := "233550007111", "233550007112"

	closePickupSale(t, c, buyer1, "SHOP-EFUA", "Shea butter", "120")
	c.replies(t, ownerEfua)
	c.say(t, buyer1, "I will pay to the number")
	c.replies(t, buyer1)
	sayImage(t, buyer1, "MEDIA-Q1", "")
	c.replies(t, buyer1)
	c.replies(t, ownerEfua)

	closePickupSale(t, c, buyer2, "SHOP-EFUA", "Shea butter", "120")
	c.replies(t, ownerEfua)
	c.say(t, buyer2, "I will pay to the number")
	c.replies(t, buyer2)
	sayImage(t, buyer2, "MEDIA-Q2", "")
	c.replies(t, buyer2)
	c.replies(t, ownerEfua)

	orders := ordersOnDisk(t, dir)
	if len(orders) != 2 {
		t.Fatalf("orders = %v, want two", orders)
	}
	ref1, ref2 := orders[0]["reference"].(string), orders[1]["reference"].(string)
	alert2 := agentMsgID(t, dir, ownerEfua, "(order "+ref2+")")

	// She quotes the SECOND claim's alert and says yes: the
	// second claim settles, the first (the oldest, the one a
	// plain yes would have taken) stays open.
	quoteSay(t, ownerEfua, "yes", alert2, "233209450786")
	c.replies(t, ownerEfua)
	byRef := map[string]map[string]any{}
	for _, o := range ordersOnDisk(t, dir) {
		byRef[o["reference"].(string)] = o
	}
	if byRef[ref2]["status"] != "paid" || byRef[ref2]["claimStatus"] != "confirmed" {
		t.Fatalf("quoted claim order = %v, want paid and confirmed", byRef[ref2])
	}
	if byRef[ref1]["status"] != "pending" || byRef[ref1]["claimStatus"] != "open" {
		t.Fatalf("older claim order = %v, want still pending and open", byRef[ref1])
	}

	// A plain yes still takes the remaining (oldest) claim.
	c.say(t, ownerEfua, "yes")
	c.replies(t, ownerEfua)
	for _, o := range ordersOnDisk(t, dir) {
		if o["reference"].(string) == ref1 && o["status"] != "paid" {
			t.Fatalf("plain yes did not settle the remaining claim: %v", o)
		}
	}
}
