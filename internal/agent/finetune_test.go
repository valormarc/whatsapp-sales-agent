package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/wants"
)

// Fine tuning from the final read of the two selling groups,
// the last training round: how sellers and buyers
// actually phrase things there, tightened into the agent. Sellers
// answer an availability ask with the plain words "Still
// available", buyers hunt a source with "any plug", prices are
// written with an at sign ("@250ghc"), and returning goods are
// announced as back in stock. The fifth tuning is the lane: the
// agent works for the merchant and does not answer general
// questions with her money.

func TestPlugWantForSomethingNotStocked(t *testing.T) {
	_, c := setupTwoShops(t)

	buyer := "233550005101"
	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "any plug for rice cooker")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "I do not have that one here yet") || !strings.Contains(got, "Smoked Tilapia") {
		t.Fatalf("plug want = %q, want the shelf offered back like any other want", got)
	}
	list := wants.Default.List("ama")
	if len(list) != 1 || !strings.Contains(strings.ToLower(list[0].Text), "rice cooker") {
		t.Fatalf("ama's wants = %+v, want the plug ask recorded with the item in it", list)
	}
}

func TestBarePlugAskOffersTheShelf(t *testing.T) {
	_, c := setupTwoShops(t)

	buyer := "233550005102"
	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "any plug pls")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Smoked Tilapia") {
		t.Fatalf("bare plug ask = %q, want the shelf offered", got)
	}
}

func TestPlugForAStockedItemSellsTheItem(t *testing.T) {
	_, c := setupTwoShops(t)

	buyer := "233550005103"
	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "any plug for beaded bag")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "GH₵850") {
		t.Fatalf("plug for a stocked item = %q, want the bag itself quoted", got)
	}
	if list := wants.Default.List("efua"); len(list) != 0 {
		t.Fatalf("a want was written for goods on the shelf: %+v", list)
	}
}

func TestAvailabilityAskLeadsWithStillAvailable(t *testing.T) {
	_, c := setupTwoShops(t)

	buyer := "233550005104"
	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "is the beaded bag available?")
	pieces := c.replies(t, buyer)
	joined := strings.Join(pieces, "\n")
	if len(pieces) < 2 || pieces[0] != "Still available." || !strings.Contains(joined, "Price: GH₵850") {
		t.Fatalf("availability answer = %q, want 'Still available.' leading, then the bag on its quote record", pieces)
	}

	loose := "233550005105"
	c.say(t, loose, "SHOP-EFUA")
	c.replies(t, loose)
	c.say(t, loose, "do you have bags?")
	pieces = c.replies(t, loose)
	if len(pieces) < 2 || pieces[0] != "Still available." {
		t.Fatalf("loose availability answer = %q, want the same plain lead", pieces)
	}
}

func TestPlainViewingHasNoAvailabilityLead(t *testing.T) {
	_, c := setupTwoShops(t)

	buyer := "233550005106"
	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "show me the beaded bag")
	got := strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "Still available") || !strings.Contains(got, "Price: GH₵850") {
		t.Fatalf("plain viewing = %q, want the ordinary item quote, no availability lead", got)
	}
}

func TestAtFigureIsReadAsAnOffer(t *testing.T) {
	_, c := setupTwoShops(t)

	buyer := "233550005107"
	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "show me the beaded bag")
	c.replies(t, buyer)
	c.say(t, buyer, "@700")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "GH₵") || !strings.Contains(got, "come down") {
		t.Fatalf("at-figure after viewing = %q, want the haggle to answer it as an offer", got)
	}

	oneBreath := "233550005108"
	c.say(t, oneBreath, "SHOP-EFUA")
	c.replies(t, oneBreath)
	c.say(t, oneBreath, "beaded bag @700")
	got = strings.Join(c.replies(t, oneBreath), "\n")
	if !strings.Contains(got, "GH₵") || !strings.Contains(got, "come down") {
		t.Fatalf("at-figure naming the item = %q, want the haggle to open on the bag", got)
	}
}

func TestLaneSteersThenGoesQuiet(t *testing.T) {
	_, c := setupTwoShops(t)

	buyer := "233550005109"
	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)

	// First strike: one short steer naming the work, shelf offered,
	// and the sum itself never answered.
	c.say(t, buyer, "what is 5 + 5?")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "outside my work here") || !strings.Contains(got, "Handmade Beaded Bag") {
		t.Fatalf("first off-topic turn = %q, want the steer with the shelf", got)
	}
	if strings.Contains(got, "10") {
		t.Fatalf("the agent answered the sum: %q", got)
	}

	// Second strike: the briefer repeat.
	c.say(t, buyer, "who is the president of Ghana?")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "I only handle the shop here") {
		t.Fatalf("second off-topic turn = %q, want the brief repeat steer", got)
	}

	// Third strike: the polite close.
	c.say(t, buyer, "what is the meaning of this word?")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "I will be here when you need something from the shop.") {
		t.Fatalf("third off-topic turn = %q, want the polite close", got)
	}

	// After that: silence. Her turns cost the merchant nothing.
	c.say(t, buyer, "tell me a joke")
	if got := c.replies(t, buyer); len(got) != 0 {
		t.Fatalf("closed lane answered again: %q", got)
	}
	c.say(t, buyer, "what is 2 + 2?")
	if got := c.replies(t, buyer); len(got) != 0 {
		t.Fatalf("closed lane answered a sum: %q", got)
	}

	// Talk about the shop reopens everything, and the strikes are
	// cleared: the next off-topic turn is a first strike again.
	c.say(t, buyer, "how much is the beaded bag?")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "The price is GH₵850") {
		t.Fatalf("shop talk after the close = %q, want the haggle quoted", got)
	}
	c.say(t, buyer, "hmm")
	c.replies(t, buyer)
	b2 := "233550005110"
	c.say(t, b2, "SHOP-EFUA")
	c.replies(t, b2)
	c.say(t, b2, "what is 3 + 3?")
	if got := strings.Join(c.replies(t, b2), "\n"); !strings.Contains(got, "outside my work here") {
		t.Fatalf("fresh buyer's first off-topic turn = %q, want a first strike, not silence", got)
	}
}

func TestLaneSparesShopQuestions(t *testing.T) {
	_, c := setupTwoShops(t)

	buyer := "233550005111"
	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)

	// An availability ask for something unstocked is a want, not
	// an off-topic turn: recorded, shelf offered, no steer.
	c.say(t, buyer, "do you have a rice cooker?")
	got := strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "outside my work") || !strings.Contains(got, "I do not have that one here yet") {
		t.Fatalf("availability ask = %q, want the want path, not the lane", got)
	}

	// A price question that happens to start "what is the" is the
	// shop's business.
	c.say(t, buyer, "what is the price of the tilapia?")
	got = strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "outside my work") || !strings.Contains(got, "GH₵180") {
		t.Fatalf("price question = %q, want the tilapia quoted", got)
	}
}
