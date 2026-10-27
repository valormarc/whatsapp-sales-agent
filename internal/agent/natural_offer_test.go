package agent

import (
	"strings"
	"testing"
)

// Natural offers: real buyers do not ask the perfect question. They
// look at an item and jump straight to "can you do it for 150", or
// name the item and the price in one breath. These tests pin the
// behaviour I found missing when my own plain
// sentence offer was met with the generic fallback: an offer in any
// natural shape opens the haggle on the right item, inside the
// right shop, and a price with no item in sight gets the human
// question back, never a sale by guesswork.

const multiShopCatalogue = `{"merchants":{` +
	`"multi":{"id":"multi","name":"Multi Stall","shopCode":"MULTI","ownerWhatsApp":"233550003009","payoutPhone":"233550003009","dailyGiveawayAllowance":200,"products":[` +
	`{"id":"smoked-tilapia","name":"Smoked Tilapia","description":"Smoked tilapia from Lake Volta, well dried.","listPrice":180,"floorPrice":140,"stock":10},` +
	`{"id":"groundnut-paste","name":"Groundnut Paste (1kg)","description":"Stone-ground groundnut paste, nothing added.","listPrice":95,"floorPrice":75,"stock":25}]}` +
	`}}`

func TestPlainSentenceOfferAfterViewingItem(t *testing.T) {
	_, c := setupTwoShops(t)

	buyer := "233550004201"
	c.say(t, buyer, "SHOP-AMA")
	c.say(t, buyer, "Tilapia smoked")
	if got := c.last(t, buyer); !strings.Contains(got, "It goes for GH₵180") {
		t.Fatalf("product view = %q", got)
	}

	// The exact shape I used: no item named, no price question,
	// just a jump to the offer. It must land on the tilapia.
	c.say(t, buyer, "Can you do it 150 please?")
	got := c.last(t, buyer)
	if strings.Contains(got, "I can help with prices and orders") {
		t.Fatalf("plain sentence offer hit the generic fallback: %q", got)
	}
	if !strings.Contains(got, "Smoked Tilapia") || !strings.Contains(got, "GH₵") {
		t.Fatalf("plain sentence offer reply = %q, want a haggle reply about the tilapia", got)
	}

	// And the haggle is truly open: the next bare number is her
	// next move, not a fresh confusion. (The close now arrives in
	// pieces; read the whole reply.)
	c.say(t, buyer, "160")
	got = strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "I can help with prices and orders") || !strings.Contains(got, "GH₵") {
		t.Fatalf("follow-up offer reply = %q, want the haggle to continue", got)
	}
}

func TestOfferNamingItemAndPriceInOneMessage(t *testing.T) {
	_, c := setupTwoShops(t)

	buyer := "233550004202"
	c.say(t, buyer, "SHOP-EFUA")
	c.say(t, buyer, "I will take the beaded bag for 700")
	got := c.last(t, buyer)
	if strings.Contains(got, "I can help with prices and orders") {
		t.Fatalf("item plus price in one message hit the generic fallback: %q", got)
	}
	if !strings.Contains(got, "Beaded Bag") || !strings.Contains(got, "GH₵") {
		t.Fatalf("item plus price reply = %q, want a haggle reply about the bag", got)
	}
}

func TestViewedItemIsTheLatestOneLookedAt(t *testing.T) {
	dir := setupChat(t, "233550009999", multiShopCatalogue)
	c := &chat{dir: dir}

	buyer := "233550004203"
	c.say(t, buyer, "SHOP-MULTI")
	c.say(t, buyer, "tilapia")
	c.say(t, buyer, "groundnut paste")
	c.say(t, buyer, "Can you do it 80 please?")
	got := c.last(t, buyer)
	if !strings.Contains(got, "Groundnut") {
		t.Fatalf("offer after viewing groundnut last = %q, want it on the groundnut paste", got)
	}
	if strings.Contains(got, "Tilapia") && !strings.Contains(got, "yours for") {
		t.Fatalf("offer landed on the wrong item: %q", got)
	}
}

func TestPriceWithNoItemInSightAsksWhichItem(t *testing.T) {
	dir := setupChat(t, "233550009999", multiShopCatalogue)
	c := &chat{dir: dir}

	buyer := "233550004204"
	c.say(t, buyer, "SHOP-MULTI")
	c.say(t, buyer, "150")
	got := c.last(t, buyer)
	if !strings.Contains(got, "Which item is that offer for?") {
		t.Fatalf("bare price with nothing viewed = %q, want the which-item question", got)
	}
}
