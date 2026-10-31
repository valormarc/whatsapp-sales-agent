package agent

// Regression tests for the three fixes Monday's work
// (pulled forward) surfaced: the shelf facts now carry
// stock, a stated quantity declines the plain item
// reading so the measure road owns the turn, and a judged
// add outranks the fulfilment walk's free-text steps.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

func TestMondayShelfFactsCarryStock(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	facts, figures := shelfFacts("demo")
	if !strings.Contains(facts, "Handmade Beaded Bag at GH₵850, 3 in stock") {
		t.Fatalf("the facts carry the shelf's stock, got %q", facts)
	}
	found := false
	for _, f := range figures {
		if f == 3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("the stock figure is verifiable, figures = %v", figures)
	}

	setupChat(t, stockOwner, measureShop)
	facts, _ = shelfFacts("demo")
	if !strings.Contains(facts, "72 yards in stock") {
		t.Fatalf("by-measure stock is stated in its own unit, got %q", facts)
	}
	if !strings.Contains(facts, "sold out") && !strings.Contains(facts, "in stock") {
		t.Fatalf("every line carries its stock state, got %q", facts)
	}
}

func TestMondayModelFigureReadsNumberAndWords(t *testing.T) {
	// The live gift probe's failure, pinned: the model
	// sometimes returns a budget as "600 cedis" rather
	// than 600, and the strict field used to reject the
	// whole discovery reading — intent, matches and all —
	// so a gift ask inside a stated budget fell through
	// to the steering line. The figure now reads either
	// way, on both boards that carry a budget.
	for _, tc := range []struct {
		raw  string
		want float64
	}{
		{`600`, 600},
		{`"600 cedis"`, 600},
		{`"GH₵1,200"`, 1200},
		{`"5k"`, 5000},
		{`"not stated"`, 0},
		{`null`, 0},
	} {
		var du discoveryUnderstanding
		if err := json.Unmarshal([]byte(`{"intent":"search","budget":`+tc.raw+`}`), &du); err != nil {
			t.Fatalf("discovery budget %s should parse: %v", tc.raw, err)
		}
		if float64(du.Budget) != tc.want {
			t.Fatalf("discovery budget %s = %v, want %v", tc.raw, float64(du.Budget), tc.want)
		}
		var bu understood
		if err := json.Unmarshal([]byte(`{"intent":"recommend","budget":`+tc.raw+`}`), &bu); err != nil {
			t.Fatalf("buyer budget %s should parse: %v", tc.raw, err)
		}
		if float64(bu.Budget) != tc.want {
			t.Fatalf("buyer budget %s = %v, want %v", tc.raw, float64(bu.Budget), tc.want)
		}
	}
}

func TestMondayQuantityAskDeclinesTheItemReading(t *testing.T) {
	setupChat(t, stockOwner, measureShop)
	buyer := "233550009001"
	shop := catalogue.GetMerchant("demo")
	s := &Session{Stage: "browsing", MerchantID: "demo"}

	// Her words carry a quantity in the product's own
	// unit: the item reading must decline, so the measure
	// road below quotes the cut.
	u := &understood{Intent: intentItem, Item: "Ankara Fabric (6 yards)", Words: "I need 3 yards of the ankara fabric"}
	if actOnBuyerUnderstanding(buyer, s, shop, "demo", u) {
		t.Fatalf("a stated quantity declines the item reading")
	}
	// Without a quantity, the same reading answers as
	// it always did.
	u2 := &understood{Intent: intentItem, Item: "Ankara Fabric (6 yards)", Words: "Tell me about the ankara fabric"}
	if !actOnBuyerUnderstanding(buyer, s, shop, "demo", u2) {
		t.Fatalf("a plain item ask is still answered by the item reading")
	}
}

func TestMondayJudgedAddOutranksThePackageNameStep(t *testing.T) {
	threeGoods := `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"deliveryFee":25,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":3},{"id":"dress","name":"Woodin Print Midi Dress","description":"Woodin print midi dress with short sleeves.","listPrice":520,"floorPrice":430,"stock":4},{"id":"headwrap","name":"African Print Headwrap","description":"Full-length African print headwrap.","listPrice":85,"floorPrice":60,"stock":10}]}}}`
	setupChat(t, stockOwner, threeGoods)
	// The stage thinking reads her words as an add but
	// substitutes the dress for the headwrap she named —
	// the substitution the live real-shop run produced.
	// Her own words carry the decisive name word, so the
	// verified candidate is the headwrap, not the model's
	// item.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": `{"intent":"add","item":"Woodin Print Midi Dress"}`}}},
		})
	}))
	defer stub.Close()
	t.Setenv("META_MODEL_API_KEY", "stub-key")
	t.Setenv("META_MODEL_API_BASE", stub.URL+"/v1")

	buyer := "233550009002"
	merchant := catalogue.GetMerchant("demo")
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	s := &Session{
		Stage: "fulfilment", MerchantID: "demo", Product: bag,
		AgreedPrice: 800, FulfilStep: "pkgname",
		PendingFulfil: fulfilment{Mode: "delivery", Location: "East Legon"},
	}
	sessions.put(sessionKey("demo", buyer), s)
	inTurn(buyer, func() bool {
		handleFulfilment(buyer, s, merchant, "Please add the headwrap too", "please add the headwrap too")
		return true
	})
	if s.PendingAdd == nil || s.PendingAdd.Name != "African Print Headwrap" {
		t.Fatalf("the judged add is offered at the package-name step, PendingAdd = %+v", s.PendingAdd)
	}
	if s.PendingFulfil.PackageName != "" {
		t.Fatalf("her instruction must not land on the package, got %q", s.PendingFulfil.PackageName)
	}
}
