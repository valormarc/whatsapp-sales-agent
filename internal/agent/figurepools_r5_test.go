package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-5 cluster F: the day gates verify in two pools, the
// drafted sales pick obeys the budget and names what it
// resolved, and the seller's notice states a measured
// quantity once.

// A count is not money: a day with 850 sales and no
// GH₵850 anywhere must not let a drafted reply state
// GH₵850 as a fact of the day, while the bare count
// itself stays sayable.
func TestDayPoolsKeepCountsOutOfTheMoney(t *testing.T) {
	money := []float64{1200, 300, 500, 200}
	counts := []float64{850, 2}
	if recapFiguresClean("Revenue today is GH₵850.", money, counts) {
		t.Fatal("a sales count of 850 licensed a currency figure of GH₵850")
	}
	if !recapFiguresClean("You had 850 sales today and revenue of GH₵1,200.", money, counts) {
		t.Fatal("a bare count and a true money figure must both verify in their own pools")
	}
}

var salesDraftStubContent string

func stubSalesDraftModel(t *testing.T) {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": salesDraftStubContent}}},
		})
	}))
	t.Cleanup(stub.Close)
	t.Setenv("META_MODEL_API_KEY", "stub-key")
	t.Setenv("META_MODEL_API_BASE", stub.URL+"/v1")
}

func salesDraftCandidates() []salesCandidate {
	shop := &catalogue.Merchant{ID: "efua", Name: "Efua's Store", ShopCode: "EFUA"}
	return []salesCandidate{
		{Shop: shop, Product: &catalogue.Product{ID: "bag", Name: "Handmade Beaded Bag", ListPrice: 850}},
		{Shop: shop, Product: &catalogue.Product{ID: "shea", Name: "Raw Shea Butter", ListPrice: 120}},
		{Shop: shop, Product: &catalogue.Product{ID: "scarf", Name: "Silk Scarf", ListPrice: 400}},
	}
}

// The budget rule is a floor under the draft, not a hope
// in its prompt: when a candidate fits her budget, a
// pick over it is rejected even with clean figures.
func TestDraftSalesPickObeysTheBudget(t *testing.T) {
	setupChat(t, salesOwner, salesSeed)
	stubSalesDraftModel(t)
	salesDraftStubContent = `{"pick":"Handmade Beaded Bag","pickShop":"EFUA","alternative":"","altShop":"","reply":"The Handmade Beaded Bag is perfect for her evening out. It is GH₵850."}`
	if _, _, _, ok := draftSalesPick(salesBuyer, recommendSystem, "a gift for my sister", 500, salesDraftCandidates()); ok {
		t.Fatal("a pick over her budget must be rejected when cheaper candidates fit it")
	}
}

// The prose is bound to the resolution: a reply that
// names a different product than the resolved pick —
// even at the pick's own price, so the figure gate
// passes — is rejected.
func TestDraftSalesPickReplyNamesThePick(t *testing.T) {
	setupChat(t, salesOwner, salesSeed)
	stubSalesDraftModel(t)
	salesDraftStubContent = `{"pick":"Raw Shea Butter","pickShop":"EFUA","alternative":"","altShop":"","reply":"The Silk Scarf would suit her beautifully. It is GH₵120."}`
	if _, _, _, ok := draftSalesPick(salesBuyer, recommendSystem, "something for her skin", 500, salesDraftCandidates()); ok {
		t.Fatal("a reply naming a product other than the resolved pick must be rejected")
	}
	salesDraftStubContent = `{"pick":"Raw Shea Butter","pickShop":"EFUA","alternative":"","altShop":"","reply":"The Raw Shea Butter is pure and unrefined, kind to her skin. It is GH₵120."}`
	pick, _, _, ok := draftSalesPick(salesBuyer, recommendSystem, "something for her skin", 500, salesDraftCandidates())
	if !ok || pick == nil || pick.Product.ID != "shea" {
		t.Fatal("a draft that names its pick, at its price, inside her budget must pass")
	}
}

// A measured line's stored name already carries its
// measure; the seller's notice must state the quantity
// once, the way the buyer's own records do.
func TestOrderNoticeStatesAMeasuredQuantityOnce(t *testing.T) {
	o := &orders.Order{
		Reference: "TEST-1001",
		From:      "233550000009",
		Price:     340,
		Items: []orders.OrderItem{
			{Name: "3 yards of Ankara fabric", Price: 240, Quantity: 3, Unit: "yard"},
			{Name: "Plain Tote Bag", Price: 100, Quantity: 1},
		},
	}
	joined := strings.Join(orderNoticeLines(o, nil), "\n")
	if strings.Contains(joined, "3 yards of 3 yards") {
		t.Fatalf("the quantity is stated twice in the seller's notice:\n%s", joined)
	}
	if !strings.Contains(joined, "3 yards of Ankara fabric") {
		t.Fatalf("the measured line must still be named with its measure once:\n%s", joined)
	}
}
