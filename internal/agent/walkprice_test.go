package agent

// The walk's price steps read price-shaped answers only.
// The burst batch learned the lesson first (a figure inside
// a sentence is about something else); the ordinary listing
// walk learned it the hard way, when a seller's sentence
// about her stock landed at the last price step and a
// product was saved at GH₵2. These tests hold the line: a
// sentence at a price step is refused and the question is
// asked again, while every shape a seller actually answers
// in — a bare figure, a currency figure, "it is 450", "take
// 400", a plain figure at the intake gate — still settles.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// finishWalkToShelf answers whatever the walk asks after
// the last price (the measure question, a quantity
// question, or the summary itself) and confirms the save.
func finishWalkToShelf(t *testing.T, c *chat, merchant string) {
	t.Helper()
	got := c.last(t, merchant)
	if strings.Contains(got, "by measure") {
		c.say(t, merchant, "only as a whole")
	} else if strings.Contains(got, "How much of it") {
		c.say(t, merchant, "6 yards")
	}
	if got := c.last(t, merchant); !strings.Contains(got, "Shall I add it?") {
		t.Fatalf("confirm = %q", got)
	}
	c.say(t, merchant, "yes")
}

func TestWalkRefusesAStockSentenceAtTheLastPrice(t *testing.T) {
	merchant := "233550001701"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.last(t, merchant)
	c.say(t, merchant, "Shell Sandals")
	c.last(t, merchant)
	c.say(t, merchant, "Light sandals with a shell strap.")
	if got := c.last(t, merchant); !strings.Contains(got, "list price") {
		t.Fatalf("want the list price question, got %q", got)
	}
	c.say(t, merchant, "80")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") {
		t.Fatalf("want the last price question, got %q", got)
	}
	c.say(t, merchant, "I have 2 pairs in stock")
	if got := c.last(t, merchant); !strings.Contains(got, "did not take a last price") {
		t.Fatalf("a stock sentence at the last price step must be refused, got %q", got)
	}
	c.say(t, merchant, "70")
	finishWalkToShelf(t, c, merchant)
	p := catalogue.FindByName(MerchantID, "Shell Sandals")
	if p == nil || p.ListPrice != 80 || p.FloorPrice != 70 {
		t.Fatalf("saved product = %+v, want list 80 and last 70, never the sentence's 2", p)
	}
}

func TestWalkRefusesASentenceAtTheListPrice(t *testing.T) {
	merchant := "233550001702"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.last(t, merchant)
	c.say(t, merchant, "Woven Basket")
	c.last(t, merchant)
	c.say(t, merchant, "Hand woven market basket.")
	c.last(t, merchant)
	c.say(t, merchant, "I run 3 shops in Accra")
	if got := c.last(t, merchant); !strings.Contains(got, "did not take a price") {
		t.Fatalf("a sentence at the list price step must be refused, got %q", got)
	}
	// A plainly shaped answer still settles the step.
	c.say(t, merchant, "It is 450 cedis")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") {
		t.Fatalf("'it is 450 cedis' is a price answer and must settle, got %q", got)
	}
	c.say(t, merchant, "take 400")
	finishWalkToShelf(t, c, merchant)
	p := catalogue.FindByName(MerchantID, "Woven Basket")
	if p == nil || p.ListPrice != 450 || p.FloorPrice != 400 {
		t.Fatalf("saved product = %+v, want list 450 and last 400", p)
	}
}

func TestIntakeGateRefusesASentenceFigure(t *testing.T) {
	merchant := "233550001703"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.last(t, merchant)
	c.say(t, merchant, "Dell Laptop 5550 4GB")
	c.last(t, merchant)
	c.say(t, merchant, "A fast laptop for work.")
	if got := c.last(t, merchant); !strings.Contains(got, "I read the price for Dell Laptop 4GB as GH₵5,550") {
		t.Fatalf("want the intake gate, got %q", got)
	}
	c.say(t, merchant, "I have 5 of them in the shop")
	if got := c.last(t, merchant); !strings.Contains(got, "I read the price") {
		t.Fatalf("a sentence must not replace the gated price; the gate asks again, got %q", got)
	}
	c.say(t, merchant, "4500")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") {
		t.Fatalf("her own figure settles the gate, got %q", got)
	}
	c.say(t, merchant, "3900")
	finishWalkToShelf(t, c, merchant)
	p := catalogue.FindByName(MerchantID, "Dell Laptop 4GB")
	if p == nil || p.ListPrice != 4500 || p.FloorPrice != 3900 {
		t.Fatalf("saved product = %+v, want her confirmed 4500 and last 3900", p)
	}
}
