package agent

// A pasted advert, read in one go. Sellers do not feed an agent
// one answer at a time when they already hold the whole advert:
// they copy the text they post everywhere and send it once. The
// name step reads that the way a person does: first line the
// name, the rest the description, the marked price taken from
// the whole message, and only what the advert did not carry is
// ever asked for. Figures carry their thousands commas with
// them: "Ghc 1,500" is fifteen hundred cedis, never one.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

func TestThrowPriceReadsCommaFiguresWhole(t *testing.T) {
	for _, tc := range []struct {
		caption string
		want    float64
	}{
		{"Ghc 1,500", 1500},
		{"Ghc 12,500", 12500},
		{"Ghc 3,499", 3499},
		{"@1,500gh", 1500},
		{"1,500", 1500},
		{"GHS 4500", 4500},
		{"Laptop in good shape. Ghc 1,500 negotiable", 1500},
	} {
		price, span, ok := throwPrice(tc.caption)
		if !ok || price != tc.want {
			t.Fatalf("throwPrice(%q) = %v, %v; want %v", tc.caption, price, ok, tc.want)
		}
		if tc.caption == "Ghc 1,500" && tc.caption[span[0]:span[1]] != "Ghc 1,500" {
			t.Fatalf("span for %q covers %q, want the whole price expression", tc.caption, tc.caption[span[0]:span[1]])
		}
	}
}

const laptopAdvert = "HP EliteBook 1030 G8 x360\n" +
	"Intel Core i5 Processor, 11th generation\n" +
	"16GB RAM, 512GB SSD\n" +
	"Few months used, like new, with box\n" +
	"👇🏾👇🏾👇🏾\n" +
	"Ghc 1,500"

func TestPastedAdvertIsReadInOneGo(t *testing.T) {
	merchant := "233550001301"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.last(t, merchant)
	c.say(t, merchant, laptopAdvert)
	batch := c.replies(t, merchant)
	replies := strings.Join(batch, "\n")
	if !strings.Contains(replies, "I took the price from your message: GH₵1,500") {
		t.Fatalf("the advert's price should be read whole and told back, got %q", replies)
	}
	if !strings.Contains(replies, "I have read it as") {
		t.Fatalf("the split should be told back, got %q", replies)
	}
	// Everything the advert carried is settled, so the only
	// question left is the one no advert carries: her last price.
	if got := batch[len(batch)-1]; !strings.Contains(got, "last price") || !strings.Contains(got, "HP EliteBook 1030 G8 x360") {
		t.Fatalf("after the advert, want the last price question, got %q", got)
	}
	c.say(t, merchant, "1,200")
	if got := c.last(t, merchant); !strings.Contains(got, "by measure") {
		t.Fatalf("a comma figure answers the last price too, got %q", got)
	}
	c.say(t, merchant, "whole")
	summary := c.last(t, merchant)
	for _, want := range []string{"HP EliteBook 1030 G8 x360", "GH₵1,500", "GH₵1,200"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q missing %q", summary, want)
		}
	}
	c.say(t, merchant, "yes")
	c.last(t, merchant)
	p := catalogue.FindByName(MerchantID, "HP EliteBook 1030 G8 x360")
	if p == nil || p.Name != "HP EliteBook 1030 G8 x360" || p.ListPrice != 1500 || p.FloorPrice != 1200 {
		t.Fatalf("saved product = %+v, want the laptop at 1500 with last price 1200", p)
	}
	if !strings.Contains(p.Description, "16GB RAM") || strings.Contains(p.Description, "Ghc") || strings.Contains(p.Description, "👇") {
		t.Fatalf("saved description = %q, want her spec lines with no price line and no decoration", p.Description)
	}
}

func TestPastedWriteUpSingleLineAsksOnlyTheName(t *testing.T) {
	merchant := "233550001302"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.last(t, merchant)
	c.say(t, merchant, "HP EliteBook 840 laptop, Intel Core i7, 16GB RAM, 512GB SSD, brand new sealed with warranty, GHS 4500")
	batch := c.replies(t, merchant)
	replies := strings.Join(batch, "\n")
	if !strings.Contains(replies, "GH₵4,500") || !strings.Contains(replies, "description") {
		t.Fatalf("the write-up should be read as description and price, got %q", replies)
	}
	if got := batch[len(batch)-1]; !strings.Contains(got, "What is it called?") {
		t.Fatalf("the one missing piece is the name, got %q", got)
	}
	// The name she gives carries its model number, and the price
	// is already settled, so the number stays in the name.
	c.say(t, merchant, "HP EliteBook 840")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") {
		t.Fatalf("after the name, want the last price question, got %q", got)
	}
	c.say(t, merchant, "3900")
	finishWhole(t, c, merchant)
	p := catalogue.FindByName(MerchantID, "HP EliteBook 840")
	if p == nil || p.Name != "HP EliteBook 840" || p.ListPrice != 4500 || p.FloorPrice != 3900 {
		t.Fatalf("saved product = %+v, want the laptop named whole at 4500 with last price 3900", p)
	}
}

func TestSloganLineIsNotTheProductName(t *testing.T) {
	// The shape that reached the live shelf: a decorated
	// shout over a laptop advert. The shout is not the name,
	// the laptop line is, and its spec segments read as
	// description, not as part of the name.
	draft, _, ok := parsePastedAdvert("*🔥OTUMFOU😂*\nHP EliteBook 840 G5, Core i7, 16GB RAM, 512GB SSD\nPrice: Ghc 3,499")
	if !ok {
		t.Fatal("the advert must be read")
	}
	if draft.Name != "HP EliteBook 840 G5" {
		t.Fatalf("name = %q, want the laptop line without its spec segments", draft.Name)
	}
	if !strings.Contains(draft.Description, "Core i7") || strings.Contains(draft.Description, "OTUMFOU") {
		t.Fatalf("description = %q", draft.Description)
	}
	if draft.ListPrice != 3499 {
		t.Fatalf("price = %v, want 3499", draft.ListPrice)
	}
}

func TestBareOneWordNameSurvives(t *testing.T) {
	draft, _, ok := parsePastedAdvert("Tilapia\nFresh from the lake, smoked today\nGH₵120")
	if !ok || draft.Name != "Tilapia" {
		t.Fatalf("a bare one-word name is a name: %+v ok=%v", draft, ok)
	}
}

func TestDecoratedNameIsCleanedForTheShelf(t *testing.T) {
	name, _, _ := parseTypedNameLine("🔥BLACK NYLON SHIRT🔥", 0)
	if name != "BLACK NYLON SHIRT" {
		t.Fatalf("name = %q", name)
	}
}
