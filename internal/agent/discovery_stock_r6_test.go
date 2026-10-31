package agent

import (
	"strings"
	"testing"
)

// Run-6 record: discovery offered stock-zero products as
// available and bound the buyer to their shop. The
// availability rule now lives at the discovery sources.

const shopSoldOutBag = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag.","listPrice":850,"floorPrice":680,"stock":0},{"id":"leather-purse","name":"Leather Purse","description":"A purse.","listPrice":200,"floorPrice":150,"stock":2}]}}}`

func TestDiscoveryNeverOffersSoldOutGoods(t *testing.T) {
	setupChat(t, stockOwner, shopSoldOutBag)
	dir := discoveryDirectory()
	if strings.Contains(dir, "Handmade Beaded Bag") {
		t.Fatalf("the directory must not list a sold-out product: %q", dir)
	}
	if !strings.Contains(dir, "Leather Purse") {
		t.Fatalf("the directory must still list what can be bought: %q", dir)
	}
	taste, shown := discoveryTaste()
	if strings.Contains(taste, "Handmade Beaded Bag") || len(shown) != 1 {
		t.Fatalf("the taste must name only stocked goods: %q shown=%v", taste, shown)
	}
	u := &discoveryUnderstanding{Matches: []discoveryMatch{
		{Shop: "TEST", Product: "Handmade Beaded Bag"}, {Shop: "TEST", Product: "Leather Purse"},
	}}
	got := verifiedMatches(u)
	if len(got) != 1 || got[0].Product.Name != "Leather Purse" {
		t.Fatalf("verified matches must exclude the sold-out product: %+v", got)
	}
}
