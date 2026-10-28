package agent

// The intake gate. A price the agent reads out of a seller's own
// words is a proposal, not a fact: when the harvest itself doubts
// the figure (a fragment of a bigger one, a number lifted out of
// a spec line), the seller confirms it in her own words before
// the listing can move, and an unconfirmed draft never reaches
// the shelf. A name already on the shelf offers replace or
// rename, never a dead end. The last price question names its
// own escape, the price correction. And a shop with no daily
// allowance set is told so, with the command spelled out.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

func TestSpecLinePriceIsConfirmedBeforeItIsTrusted(t *testing.T) {
	merchant := "233550001501"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.last(t, merchant)
	c.say(t, merchant, "Dell Laptop 5550 4GB")
	batch := c.replies(t, merchant)
	joined := strings.Join(batch, "\n")
	if !strings.Contains(joined, "I took the price from your line: GH₵5550") {
		t.Fatalf("the spec figure should be read and told back, got %q", joined)
	}
	if got := batch[len(batch)-1]; !strings.Contains(got, "Tell me about") {
		t.Fatalf("after the name line, want the description question, got %q", got)
	}
	c.say(t, merchant, "A fast laptop for work.")
	if got := c.last(t, merchant); !strings.Contains(got, "I read the price for Dell Laptop 4GB as GH₵5550") || !strings.Contains(got, "Is that the right price?") {
		t.Fatalf("a figure harvested from a spec line must be confirmed, got %q", got)
	}
	c.say(t, merchant, "4500")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") {
		t.Fatalf("her own figure settles the gate and the walk goes on, got %q", got)
	}
	c.say(t, merchant, "3900")
	finishWhole(t, c, merchant)
	p := catalogue.FindByName(MerchantID, "Dell Laptop 4GB")
	if p == nil || p.ListPrice != 4500 || p.FloorPrice != 3900 {
		t.Fatalf("saved product = %+v, want her confirmed 4500, not the harvested 5550", p)
	}
}

func TestTinyPriceIsConfirmedAndKeptOnYes(t *testing.T) {
	merchant := "233550001502"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.last(t, merchant)
	c.say(t, merchant, "Groundnut Paste @4")
	c.last(t, merchant)
	c.say(t, merchant, "Smooth groundnut paste.")
	if got := c.last(t, merchant); !strings.Contains(got, "GH₵4") || !strings.Contains(got, "Is that the right price?") {
		t.Fatalf("a GH₵4 read must be confirmed even when she marked it herself, got %q", got)
	}
	c.say(t, merchant, "yes")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") {
		t.Fatalf("a yes at the gate lets the walk go on, got %q", got)
	}
	c.say(t, merchant, "3")
	finishWhole(t, c, merchant)
	p := catalogue.FindByName(MerchantID, "Groundnut Paste")
	if p == nil || p.ListPrice != 4 || p.FloorPrice != 3 {
		t.Fatalf("saved product = %+v, want the confirmed GH₵4 kept", p)
	}
}

func TestDoubtedPriceRefusedAtTheGateIsAskedPlainly(t *testing.T) {
	merchant := "233550001503"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.last(t, merchant)
	c.say(t, merchant, "Rice @2")
	c.last(t, merchant)
	c.say(t, merchant, "Long grain rice.")
	c.last(t, merchant)
	c.say(t, merchant, "no")
	if got := c.last(t, merchant); !strings.Contains(got, "What is the list price for Rice") {
		t.Fatalf("a no at the gate should ask the price plainly, got %q", got)
	}
	c.say(t, merchant, "300")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") {
		t.Fatalf("her typed price is her statement and is not gated, got %q", got)
	}
	c.say(t, merchant, "250")
	finishWhole(t, c, merchant)
	p := catalogue.FindByName(MerchantID, "Rice")
	if p == nil || p.ListPrice != 300 {
		t.Fatalf("saved product = %+v, want her typed 300", p)
	}
}

func TestDuplicateNameOffersReplace(t *testing.T) {
	merchant := "233550001504"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.last(t, merchant)
	c.say(t, merchant, "Handmade Beaded Bag")
	if got := c.last(t, merchant); !strings.Contains(got, "replace") || !strings.Contains(got, "different name") {
		t.Fatalf("a name clash should offer replace or rename, got %q", got)
	}
	c.say(t, merchant, "replace")
	batch := c.replies(t, merchant)
	joined := strings.Join(batch, "\n")
	if !strings.Contains(joined, "the old one is off the shelf") {
		t.Fatalf("replace should take the old listing out, got %q", joined)
	}
	if got := batch[len(batch)-1]; !strings.Contains(got, "Tell me about") {
		t.Fatalf("after replace, the walk goes on, got %q", got)
	}
	c.say(t, merchant, "A newer make of the bag.")
	c.last(t, merchant)
	c.say(t, merchant, "900")
	c.last(t, merchant)
	c.say(t, merchant, "800")
	finishWhole(t, c, merchant)
	products := catalogue.ListProducts(MerchantID)
	if len(products) != 1 {
		t.Fatalf("products = %v, want the one replacement", products)
	}
	if products[0].ListPrice != 900 {
		t.Fatalf("the shelf carries %+v, want the new listing at 900", products[0])
	}
}

func TestDuplicateNameRenameKeepsBoth(t *testing.T) {
	merchant := "233550001505"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.last(t, merchant)
	c.say(t, merchant, "Handmade Beaded Bag")
	c.last(t, merchant)
	c.say(t, merchant, "Beaded Tote Bag")
	if got := c.last(t, merchant); !strings.Contains(got, "Tell me about Beaded Tote Bag") {
		t.Fatalf("a new name at the choice should carry the walk on, got %q", got)
	}
	c.say(t, merchant, "A tote version of the bag.")
	c.last(t, merchant)
	c.say(t, merchant, "700")
	c.last(t, merchant)
	c.say(t, merchant, "600")
	finishWhole(t, c, merchant)
	if got := len(catalogue.ListProducts(MerchantID)); got != 2 {
		t.Fatalf("products = %d, want the old bag and the renamed tote", got)
	}
	old := catalogue.FindByName(MerchantID, "Handmade Beaded Bag")
	if old == nil || old.ListPrice != 850 {
		t.Fatalf("the old listing should stand untouched, got %+v", old)
	}
}

func TestLastPriceQuestionStatesItsOwnEscape(t *testing.T) {
	merchant := "233550001506"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.last(t, merchant)
	c.say(t, merchant, "Kente Cloth")
	c.last(t, merchant)
	c.say(t, merchant, "Handwoven kente.")
	c.last(t, merchant)
	c.say(t, merchant, "500")
	if got := c.last(t, merchant); !strings.Contains(got, "correct it like this: price is 500") {
		t.Fatalf("the last price question should name the correction, got %q", got)
	}
	c.say(t, merchant, "plenty")
	if got := c.last(t, merchant); !strings.Contains(got, "correct it like this: price is 500") {
		t.Fatalf("the re-ask should name the correction too, got %q", got)
	}
	c.say(t, merchant, "price is 600")
	batch := c.replies(t, merchant)
	joined := strings.Join(batch, "\n")
	if !strings.Contains(joined, "Corrected. The list price for Kente Cloth is now GH₵600.") {
		t.Fatalf("the stated correction should work from inside the step, got %q", joined)
	}
	if got := batch[len(batch)-1]; !strings.Contains(got, "last price") {
		t.Fatalf("after the correction the step is asked again, got %q", got)
	}
	c.say(t, merchant, "450")
	finishWhole(t, c, merchant)
	p := catalogue.FindByName(MerchantID, "Kente Cloth")
	if p == nil || p.ListPrice != 600 || p.FloorPrice != 450 {
		t.Fatalf("saved product = %+v, want the corrected 600 with last price 450", p)
	}
}

const shopNoAllowance = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":0,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":3}]}}}`

func TestZeroAllowanceIsPromptedOnTheDayAnswer(t *testing.T) {
	merchant := "233550001507"
	dir := setupChat(t, merchant, shopNoAllowance)
	c := &chat{dir: dir}

	c.say(t, merchant, "How is today going?")
	joined := strings.Join(c.replies(t, merchant), "\n")
	if !strings.Contains(joined, "sale(s) today") {
		t.Fatalf("the day answer itself should still come first, got %q", joined)
	}
	if !strings.Contains(joined, "you have not set a daily giveaway allowance") || !strings.Contains(joined, "set allowance 300") {
		t.Fatalf("a zero allowance should be prompted with the command spelled out, got %q", joined)
	}

	merchant2 := "233550001508"
	dir2 := setupChat(t, merchant2, shopWithBag)
	c2 := &chat{dir: dir2}
	c2.say(t, merchant2, "How is today going?")
	joined2 := strings.Join(c2.replies(t, merchant2), "\n")
	if strings.Contains(joined2, "have not set a daily giveaway allowance") {
		t.Fatalf("a shop with an allowance set should not be prompted, got %q", joined2)
	}
}

func TestPhotoPriceAmongOtherFiguresIsConfirmed(t *testing.T) {
	merchant := "233550001509"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, merchant, "Heels size 44 250")
	if got := c.last(t, merchant); !strings.Contains(got, "What is it called?") {
		t.Fatalf("the caption carries no name, so the name is asked, got %q", got)
	}
	c.say(t, merchant, "Red Heels")
	if got := c.last(t, merchant); !strings.Contains(got, "GH₵250") || !strings.Contains(got, "Is that the right price?") {
		t.Fatalf("a lone figure among sizes must be confirmed, got %q", got)
	}
	c.say(t, merchant, "yes")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") {
		t.Fatalf("a yes at the gate lets the walk go on, got %q", got)
	}
	c.say(t, merchant, "200")
	finishWhole(t, c, merchant)
	p := catalogue.FindByName(MerchantID, "Red Heels")
	if p == nil || p.ListPrice != 250 || p.FloorPrice != 200 {
		t.Fatalf("saved product = %+v, want the confirmed 250", p)
	}
}
