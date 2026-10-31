package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// The counter fixes from the owner's live run, pinned.
// The model-read roads (the basket ask in her own words,
// acceptance at a standing counter) are proven live in
// scripts/prove-counterlife.js; what the stub suite pins
// is the deterministic half each road stands on: the net's
// exact boundary, the place judgement's gate, the frames,
// the carried note, the in-hand figure, and the draft
// guard.

// The basket net keeps its exact shape: the canonical
// phrasings are served by the view, "check basket" is NOT
// widened into the net (the thinking reads it), and an
// adding ask is never the view.
func TestBasketNetKeepsItsShape(t *testing.T) {
	for _, m := range []string{"show my basket", "my basket", "check my basket", "what is in my basket?", "is the dress in my basket?"} {
		if !basketViewRe.MatchString(m) {
			t.Fatalf("the net should read %q as the basket ask", m)
		}
	}
	for _, m := range []string{"check basket", "add to my basket", "basketball shoes"} {
		if basketViewRe.MatchString(m) {
			t.Fatalf("the net must not claim %q: that reading belongs to the thinking", m)
		}
	}
}

// A place is judged by the reading, not by its shape: a
// greeting the thinking read as small talk is not a town,
// a stated place stands on her own words, and with no
// reading the shaped word stands as it always did.
func TestReadPlaceIsJudgedNotShaped(t *testing.T) {
	if got := readPlace("", "Hello", stageReading{Intent: stageIntentOther}); got != "" {
		t.Fatalf("a greeting read as talk settled as a place: %q", got)
	}
	if got := readPlace("", "Accra", stageReading{Intent: stageIntentAnswer}); got != "Accra" {
		t.Fatalf("an answered place should stand, got %q", got)
	}
	if got := readPlace("East Legon", "hello", stageReading{Intent: stageIntentOther}); got != "East Legon" {
		t.Fatalf("a stated place stands on her words, got %q", got)
	}
	if got := readPlace("", "Hello", stageReading{}); got != "Hello" {
		t.Fatalf("with no reading the shaped word stands, got %q", got)
	}
}

// The draft guard: two products strung in prose is a list
// pretending to be a sentence; one named product is talk.
func TestProductsStrungGuard(t *testing.T) {
	setupChat(t, "233265993567", discoveryCatalogue)
	strung := "We have a few options, such as the Handmade Beaded Bag at GH₵850 or the Beaded Sandals."
	if !productsStrung(strung) {
		t.Fatalf("a reply stringing two products should read as strung: %q", strung)
	}
	if productsStrung("The Handmade Beaded Bag is lovely, and it is the only one left.") {
		t.Fatal("naming one product in talk must stay allowed")
	}
	if productsStrung("Your order is on its way, thank you for buying from us.") {
		t.Fatal("plain talk names no products and must pass")
	}
}

// The basket's items sit in the code box, the shape the
// owner set with his screenshot: the heading and the
// total stay ordinary text, the list itself is fenced.
func TestBasketViewArrivesBoxed(t *testing.T) {
	dir := setupChat(t, stockOwner, totalHandCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009558"
	pickRackAndHalter(t, c, buyer, "jf2")
	c.say(t, buyer, "show me my basket")
	got := c.last(t, buyer)
	if !strings.Contains(got, "```\nDouble Clothes Rack - GH₵270") || !strings.Contains(got, "Halter Pleated Maxi Dress - GH₵150\n```") {
		t.Fatalf("the basket should arrive with its items in the code box, got %q", got)
	}
}

// The haggle's holding frames speak of a hand as all of
// them, never as its joined label with a singular verb.
func TestHandHoldsSpeakOfAllOfThem(t *testing.T) {
	dir := setupChat(t, stockOwner, totalHandCatalogue)
	_ = dir
	c := &chat{dir: dir}
	buyer := "233550009557"
	pickRackAndHalter(t, c, buyer, "jf1")
	c.say(t, buyer, "300 for all")
	c.last(t, buyer) // the counter stands
	c.say(t, buyer, "let me think")
	got := c.last(t, buyer)
	if !strings.Contains(got, "All of them stand at GH₵") {
		t.Fatalf("the holding frame for a hand = %q, want it to speak of all of them", got)
	}
	if strings.Contains(got, " and ") && strings.Contains(got, "stands at") {
		t.Fatalf("the joined label spoke with a singular verb: %q", got)
	}
}

// "Same" at the message question carries the note her
// last order held, and the recap prints the note itself.
func TestNoteSameCarriesTheLastNote(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550009777"
	orders.Default.RecordOrder("EFUA-0001", orders.Order{
		Reference: "EFUA-0001", Status: "paid", From: buyer, MerchantID: "efua",
		ProductName: "Handmade Beaded Bag", Price: 850,
		Note: "Pink and white", CreatedAt: "2026-10-30T10:00:00Z",
	})
	agreeBagAtList(t, c, buyer)
	tap(t, buyer, "ff_delivery")
	c.say(t, buyer, "Osu")
	c.say(t, buyer, "Akosua Mensah")
	if got := c.last(t, buyer); !strings.Contains(got, "Any message for the seller") {
		t.Fatalf("after the name = %q, want the message question", got)
	}
	c.say(t, buyer, "same")
	got := c.last(t, buyer)
	if !strings.Contains(got, "Your message for the seller: Pink and white.") {
		t.Fatalf("the recap should print the carried note, got %q", got)
	}
	s, ok := sessions.lookup(sessionKey("efua", buyer))
	if !ok || s.PendingFulfil.Note != "Pink and white" {
		t.Fatalf("the carried note should stand on the pending fulfilment, got %+v", s)
	}
}

// The in-hand ask names the gated item at its own share:
// the heels at GH₵116, not the order's GH₵195.
const inHandCatalogue = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","ownerWhatsApp":"233550000001","dailyGiveawayAllowance":300,"products":[{"id":"slippers","name":"Studded Thong Slippers","description":"Studded thong slippers.","listPrice":80,"floorPrice":70,"stock":1},{"id":"heels","name":"Ankle Strap Heels","description":"Ankle strap heels.","listPrice":130,"floorPrice":110,"stock":1}]}}}`

func TestInHandAskNamesTheItemAtItsShare(t *testing.T) {
	dir := setupChat(t, stockOwner, inHandCatalogue)
	c := &chat{dir: dir}
	merchant := catalogue.GetMerchant("demo")
	slippers := catalogue.FindProduct("demo", "studded thong slippers")
	heels := catalogue.FindProduct("demo", "ankle strap heels")
	if slippers == nil || heels == nil {
		t.Fatal("fixture products missing")
	}
	buyer := "233550009556"
	s := &Session{
		MerchantID:  "demo",
		Stage:       "fulfilment",
		Product:     heels,
		AgreedPrice: 116,
		Basket:      []BasketItem{{Product: slippers, AgreedPrice: 79}},
	}
	grounds := inHandGrounds(merchant, s, s.AgreedPrice)
	if !strings.Contains(grounds, "The Ankle Strap Heels is the only one the shop has") {
		t.Fatalf("the buyer's grounds should name the gated item, got %q", grounds)
	}
	inTurn(buyer, func() bool {
		beginInHandWait(buyer, s, merchant, fulfilment{Mode: "arranged", Location: "Accra"}, grounds)
		return true
	})
	got := c.joined(t, stockOwner)
	if !strings.Contains(got, "Ankle Strap Heels (GH₵116)") {
		t.Fatalf("the seller should be asked about the heels at their share, got %q", got)
	}
	if strings.Contains(got, "(GH₵195)") {
		t.Fatalf("the seller was asked at the whole order's total: %q", got)
	}
}

// The cross-shop search arrives as one boxed list: each
// shop's code alone as its heading, its goods under it,
// the blocks parted by a blank line.
func TestDiscoverySearchArrivesBoxed(t *testing.T) {
	dir := setupChat(t, "233265993567", discoveryCatalogue)
	stubDiscoveryModel(t)
	c := &chat{dir: dir}
	buyer := "233550009555"
	c.say(t, buyer, "anything beaded")
	got := c.joined(t, buyer)
	want := "```\nEFUA\nHandmade Beaded Bag - GH₵850\n\nLUXE\nBeaded Sandals - GH₵80\n```"
	if !strings.Contains(got, want) {
		t.Fatalf("the search results should arrive in the code box, got %q", got)
	}
}
