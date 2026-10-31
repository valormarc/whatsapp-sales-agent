package agent

// The sales layer's floors: the budget is carried from her
// words, a balk on plain words brings the cheaper suitable
// thing forward, and the price-only picks obey the budget.
// The judgment itself (which product fits her occasion) is
// the model's and is proven live; what is proven here is
// that the platform's arithmetic never lies.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

const salesOwner = "233550004311"
const salesBuyer = "233550004312"

const salesSeed = `{"merchants":{"efua":{"id":"efua","name":"Efua's Store","shopCode":"EFUA","ownerWhatsApp":"` + salesOwner + `","payoutPhone":"` + salesOwner + `","dailyGiveawayAllowance":300,"products":[{"id":"bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":5},{"id":"shea","name":"Raw Shea Butter","description":"Unrefined shea butter, 500g jar.","listPrice":120,"floorPrice":100,"stock":5},{"id":"scarf","name":"Silk Scarf","description":"Bright silk scarf.","listPrice":400,"floorPrice":320,"stock":5}]}}}`

func TestBudgetIsCarriedFromHerWords(t *testing.T) {
	setupChat(t, salesOwner, salesSeed)
	c := &chat{seen: map[string]int{}}

	c.say(t, salesBuyer, "SHOP-EFUA")
	c.replies(t, salesBuyer)
	c.say(t, salesBuyer, "My budget is 1500")
	c.replies(t, salesBuyer)
	s := sessions.get(sessionKey("efua", salesBuyer))
	if s == nil || s.Budget != 1500 {
		t.Fatalf("budget not carried on the session: %+v", s)
	}
	if got := netBudget("i can spend around 2,000 cedis"); got != 2000 {
		t.Fatalf("net budget = %v", got)
	}
	if got := netBudget("the bag is 850"); got != 0 {
		t.Fatalf("a plain price must not become a budget: %v", got)
	}
}

func TestBalkBringsTheCheaperThingForward(t *testing.T) {
	setupChat(t, salesOwner, salesSeed)
	c := &chat{seen: map[string]int{}}

	c.say(t, salesBuyer, "SHOP-EFUA")
	c.replies(t, salesBuyer)
	c.say(t, salesBuyer, "Handmade Beaded Bag")
	c.replies(t, salesBuyer)
	c.say(t, salesBuyer, "That is too expensive")
	got := c.joined(t, salesBuyer)
	if !strings.Contains(got, "Silk Scarf") || !strings.Contains(got, "GH₵400") {
		t.Fatalf("the dearest cheaper alternative should lead without a model: %q", got)
	}
	if !strings.Contains(got, "make me an offer on the Handmade Beaded Bag") {
		t.Fatalf("the haggle door must stay open behind the alternative: %q", got)
	}
	s := sessions.get(sessionKey("efua", salesBuyer))
	if s == nil || s.LastViewed == nil || s.LastViewed.Name != "Silk Scarf" {
		t.Fatalf("she should now be looking at the alternative: %+v", s)
	}
}

func TestCodeSalesPickObeysTheBudget(t *testing.T) {
	shop := &catalogue.Merchant{ID: "x", ShopCode: "X"}
	mk := func(name string, price float64) salesCandidate {
		return salesCandidate{Shop: shop, Product: &catalogue.Product{Name: name, ListPrice: price}}
	}
	cands := []salesCandidate{mk("Dear", 1800), mk("Middle", 1200), mk("Cheap", 400)}

	pick, alt := codeSalesPick(cands, 1500)
	if pick == nil || pick.Product.Name != "Middle" || alt == nil || alt.Product.Name == "Middle" {
		t.Fatalf("under a 1500 budget the pick must be the dearest fit: %+v / %+v", pick, alt)
	}
	pick, _ = codeSalesPick(cands, 300)
	if pick == nil || pick.Product.Name != "Cheap" {
		t.Fatalf("with nothing under the budget the cheapest is the closest: %+v", pick)
	}
	if got := dearestFit(cands, 1500); got == nil || got.Product.Name != "Middle" {
		t.Fatalf("objection floor under budget = %+v", got)
	}
	if got := dearestFit(cands, 0); got == nil || got.Product.Name != "Dear" {
		t.Fatalf("objection floor without a budget = %+v", got)
	}
}
