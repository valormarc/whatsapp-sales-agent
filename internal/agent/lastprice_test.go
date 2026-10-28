package agent

// Her last price, in her own words. The seller is asked for the
// lowest she will go as her "last price", never as a "floor":
// that word lives inside the code and nowhere in her market.
// And a seller whose price does not move is a real case, not an
// error: her last price can be her price, in figures or in the
// words "non-negotiable", and the product is then fixed price,
// quoted as fixed and never haggled.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

const shopWithFixedCooker = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"products":[{"id":"rice-cooker","name":"Rice Cooker","description":"A two-litre rice cooker.","listPrice":250,"floorPrice":250,"fixed":true,"stock":2}]}}}`

func TestOnboardingNonNegotiableWordMeansFixed(t *testing.T) {
	merchant := "233550001201"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.last(t, merchant)
	c.say(t, merchant, "Rice Cooker")
	c.last(t, merchant)
	c.say(t, merchant, "A two-litre rice cooker.")
	c.last(t, merchant)
	c.say(t, merchant, "250")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") || !strings.Contains(got, "non-negotiable") {
		t.Fatalf("last price question = %q", got)
	}
	c.say(t, merchant, "non-negotiable")
	replies := strings.Join(c.replies(t, merchant), "\n")
	if !strings.Contains(replies, "fixed") {
		t.Fatalf("non-negotiable should be read back as fixed, got %q", replies)
	}
	c.say(t, merchant, "whole")
	summary := c.last(t, merchant)
	if !strings.Contains(summary, "fixed") || strings.Contains(summary, "give away") {
		t.Fatalf("fixed summary = %q", summary)
	}
	c.say(t, merchant, "yes")
	if got := c.last(t, merchant); !strings.Contains(got, "fixed price") {
		t.Fatalf("save confirmation = %q", got)
	}
	p := catalogue.FindByName(MerchantID, "Rice Cooker")
	if p == nil || !p.Fixed || p.ListPrice != 250 || p.FloorPrice != 250 {
		t.Fatalf("saved product = %+v, want fixed at 250", p)
	}
}

func TestBuyerCannotHaggleAFixedPrice(t *testing.T) {
	merchant := "233550001202"
	customer := "233550009202"
	dir := setupChat(t, merchant, shopWithFixedCooker)
	c := &chat{dir: dir}

	c.say(t, customer, "SHOP-TEST")
	c.replies(t, customer)
	c.say(t, customer, "How much is the rice cooker?")
	if got := c.last(t, customer); !strings.Contains(got, "GH₵250") || !strings.Contains(got, "fixed") || strings.Contains(got, "Make me an offer") {
		t.Fatalf("fixed quote = %q", got)
	}
	c.say(t, customer, "I can offer 200")
	if got := c.last(t, customer); !strings.Contains(got, "fixed") || !strings.Contains(got, "GH₵250") {
		t.Fatalf("offer below a fixed price = %q", got)
	}
	// The talk is still open at the price: meeting it buys the
	// cooker through the ordinary close, at 250 exactly.
	c.say(t, customer, "250")
	deal := strings.Join(c.replies(t, customer), "\n")
	if !strings.Contains(deal, "yours for GH₵250") {
		t.Fatalf("meeting the fixed price should agree the deal, got %q", deal)
	}
	c.say(t, customer, "Accra")
	c.replies(t, customer)
	c.finishDetails(t, customer)
	if got := strings.Join(c.replies(t, customer), "\n"); !strings.Contains(got, "Pay here") {
		t.Fatalf("the fixed-price sale should complete with the link, got %q", got)
	}
}

func TestChangeLastPriceFlipsFixedBothWays(t *testing.T) {
	merchant := "233550001203"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "change last price of beaded bag to 850")
	if got := c.last(t, merchant); !strings.Contains(got, "fixed") {
		t.Fatalf("last price at the price should read back as fixed, got %q", got)
	}
	if p := catalogue.FindByName(MerchantID, "beaded bag"); p == nil || !p.Fixed {
		t.Fatalf("product after the command = %+v, want fixed", p)
	}
	c.say(t, merchant, "my catalogue")
	if got := c.last(t, merchant); !strings.Contains(got, "fixed price") {
		t.Fatalf("catalogue should show the bag as fixed price, got %q", got)
	}
	c.say(t, merchant, "change last price of beaded bag to 700")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") || !strings.Contains(got, "GH₵700") {
		t.Fatalf("last price below the price reply = %q", got)
	}
	if p := catalogue.FindByName(MerchantID, "beaded bag"); p == nil || p.Fixed || p.FloorPrice != 700 {
		t.Fatalf("product after the command = %+v, want negotiable with last price 700", p)
	}
}
