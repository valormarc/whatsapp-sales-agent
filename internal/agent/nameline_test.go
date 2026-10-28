package agent

// The smarter product step: a seller who names her product the
// way sellers write, name, price and size in one line, is
// understood in one pass. The price in her line is her list price
// and is not asked for again, the size leaves the name and rides
// with the description, and what was read is told back to her.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

func TestTypedNameLineReadsPriceAndSize(t *testing.T) {
	merchant := "233550001001"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.say(t, merchant, "BLACK NYLON SHIRT @250GHS     SIZE XXL")
	replies := c.replies(t, merchant)
	joined := strings.Join(replies, "\n")
	if !strings.Contains(joined, "I took the price from your line: GH₵250.") {
		t.Fatalf("price readback missing in %q", joined)
	}
	if !strings.Contains(joined, "I have noted the size, XXL, with the description.") {
		t.Fatalf("size readback missing in %q", joined)
	}
	if got := replies[len(replies)-1]; !strings.Contains(got, "Tell me about BLACK NYLON SHIRT") || strings.Contains(got, "@250") {
		t.Fatalf("description step should name the clean product, got %q", got)
	}

	c.say(t, merchant, "ITS LOCAL MADE")
	// The list step never comes: her line already gave the price.
	// The walk lands on the last price.
	if got := c.last(t, merchant); !strings.Contains(got, "last price") {
		t.Fatalf("after description the walk should ask the last price, got %q", got)
	}
	c.say(t, merchant, "200")
	if got := c.last(t, merchant); !strings.Contains(got, "by measure") {
		t.Fatalf("measure step = %q", got)
	}
	c.say(t, merchant, "only as a whole")
	summary := c.last(t, merchant)
	for _, want := range []string{"BLACK NYLON SHIRT", "GH₵250", "Size XXL", "ITS LOCAL MADE"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q missing %q", summary, want)
		}
	}
	if strings.Contains(summary, "@250") || strings.Contains(summary, "SIZE XXL is live") {
		t.Fatalf("summary still carries the raw caption: %q", summary)
	}
	c.say(t, merchant, "yes")
	p := catalogue.FindByName(MerchantID, "BLACK NYLON SHIRT")
	if p == nil {
		t.Fatalf("product not saved under its clean name; catalogue: %+v", catalogue.ListProducts(MerchantID))
	}
	if p.ListPrice != 250 || p.FloorPrice != 200 {
		t.Fatalf("saved prices = %v / %v, want 250 / 200", p.ListPrice, p.FloorPrice)
	}
	if !strings.Contains(p.Description, "Size XXL") || !strings.Contains(p.Description, "ITS LOCAL MADE") {
		t.Fatalf("saved description = %q", p.Description)
	}
}

func TestTypedNameLinePlainNameUnchanged(t *testing.T) {
	merchant := "233550001001"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.say(t, merchant, "Kente Cloth")
	gotReplies := c.replies(t, merchant)
	replies := strings.Join(gotReplies, "\n")
	if strings.Contains(replies, "I took the price") || strings.Contains(replies, "I have noted the size") {
		t.Fatalf("a plain name should draw no readback, got %q", replies)
	}
	if got := gotReplies[len(gotReplies)-1]; !strings.Contains(got, "Tell me about Kente Cloth") {
		t.Fatalf("description step = %q", got)
	}
	c.say(t, merchant, "Handwoven, bright patterns.")
	// No price in her line, so the list price is asked as always.
	if got := c.last(t, merchant); !strings.Contains(got, "list price") {
		t.Fatalf("list step = %q", got)
	}
}

func TestTypedNameLineCountIsNotAPrice(t *testing.T) {
	merchant := "233550001001"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.say(t, merchant, "Wardrobe 3 doors")
	replies := strings.Join(c.replies(t, merchant), "\n")
	if strings.Contains(replies, "I took the price") {
		t.Fatalf("a door count must never read as a price, got %q", replies)
	}
	c.say(t, merchant, "White, with a mirror.")
	if got := c.last(t, merchant); !strings.Contains(got, "list price") {
		t.Fatalf("the price should still be asked, got %q", got)
	}
	if p := catalogue.FindByName(MerchantID, "Wardrobe 3 doors"); p != nil {
		t.Fatalf("nothing should be saved mid-flow, got %+v", p)
	}
}
