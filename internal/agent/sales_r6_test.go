package agent

import (
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// Run-6 records, sales family: the factual floor
// presented an over-budget alternative when the pick was
// the sole fit, and the drafted reply's prose was only
// presence-checked.

func TestCodePickSoleFitHasNoOverBudgetAlternative(t *testing.T) {
	shop := &catalogue.Merchant{ID: "demo", Name: "Test Shop", ShopCode: "TEST"}
	cheap := &catalogue.Product{ID: "a", Name: "Cloth Bag", ListPrice: 500}
	dear := &catalogue.Product{ID: "b", Name: "Leather Bag", ListPrice: 900}
	cands := []salesCandidate{
		{Shop: shop, Product: cheap},
		{Shop: shop, Product: dear},
	}
	pick, alt := codeSalesPick(cands, 600)
	if pick == nil || pick.Product.ID != "a" {
		t.Fatalf("the pick must be the fit, got %+v", pick)
	}
	if alt != nil {
		t.Fatalf("a sole-fit pick must carry no over-budget alternative, got %+v", alt.Product)
	}
}

func stubSalesModelR6(t *testing.T, reply string) {
	t.Helper()
	salesDraftStubContent = reply
	stubSalesDraftModel(t)
}

const shopThreeGoods = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag.","listPrice":850,"floorPrice":680,"stock":3},{"id":"purse","name":"Leather Purse","description":"A purse.","listPrice":300,"floorPrice":250,"stock":2},{"id":"scarf","name":"Silk Scarf","description":"A scarf.","listPrice":120,"floorPrice":100,"stock":2}]}}}`

func TestDraftedReplyIsBoundExclusively(t *testing.T) {
	setupChat(t, stockOwner, shopThreeGoods)
	shop := catalogue.GetMerchant("demo")
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	other := catalogue.FindByName("demo", "Leather Purse")
	cands := []salesCandidate{
		{Shop: shop, Product: bag},
		{Shop: shop, Product: other},
	}
	// A draft naming a third catalogue product outside
	// the resolved set must not pass, however clean its
	// figures are.
	stubSalesModelR6(t, `{"pick":"Handmade Beaded Bag","alternative":"Leather Purse","reply":"The Handmade Beaded Bag at GH₵850 is my pick for you. The Leather Purse at GH₵300 is the alternative. The Silk Scarf at GH₵120 is also lovely."}`)
	if _, _, _, ok := draftSalesPick("233550000911", "sys", "a bag", 0, cands); ok {
		t.Fatal("a reply naming an outside product must not pass")
	}
	// A clean draft passes.
	stubSalesModelR6(t, `{"pick":"Handmade Beaded Bag","alternative":"Leather Purse","reply":"The Handmade Beaded Bag at GH₵850 is my pick for you. The Leather Purse at GH₵300 is the alternative."}`)
	pick, _, _, ok := draftSalesPick("233550000911", "sys", "a bag", 0, cands)
	if !ok || pick == nil || pick.Product.ID != "beaded-bag" {
		t.Fatal("a clean draft bound to the pick must pass")
	}
}

func TestContainsNameBounded(t *testing.T) {
	if !containsNameBounded("the beaded bag is here", "beaded bag") {
		t.Fatal("a bounded name must match")
	}
	if containsNameBounded("the beaded bags are here", "beaded bag") {
		t.Fatal("a substring of a longer word must not match")
	}
	if containsNameBounded("handmade beaded bag", "beaded") {
		// "beaded" IS bounded here (spaces both sides) —
		// this is a true positive, not a substring case.
	} else {
		t.Fatal("a whole word must match")
	}
}
