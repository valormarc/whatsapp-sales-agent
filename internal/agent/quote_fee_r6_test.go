package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// Run-6 records: a quote-style shop's retained set fee
// was quoted to a buyer as a definite total, and a
// multi-shop seller's bare fee answer bound to an
// arbitrarily chosen waiting shop.

const shopQuoteRetainedFee = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"deliveryStyle":"quote","deliveryFee":40,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag.","listPrice":850,"floorPrice":680,"stock":3}]}}}`

func TestQuoteStyleShopNeverQuotesItsRetainedFee(t *testing.T) {
	setupChat(t, "233550000901", shopQuoteRetainedFee)
	c := &chat{seen: map[string]int{}}
	buyer := "233550000901"
	merchant := catalogue.GetMerchant("demo")
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	s := &Session{MerchantID: "demo", LastViewed: bag}
	handled := inTurn(buyer, func() bool {
		return answerDeliveryOrTotal(buyer, s, merchant, "how much in total with delivery?")
	})
	if !handled {
		t.Fatal("the total question must be answered")
	}
	got := strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "890") || strings.Contains(got, "GH₵40") {
		t.Fatalf("the retained set fee must never reach the buyer as a figure: %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "quote") {
		t.Fatalf("the answer must say delivery is quoted per order: %q", got)
	}
}

const twoShopsOneOwner = `{"merchants":{
	"shopa":{"id":"shopa","name":"Ama's Shop","shopCode":"AMA","ownerWhatsApp":"233550000902","products":[{"id":"bag-a","name":"Bag A","listPrice":100,"floorPrice":90,"stock":2}]},
	"shopb":{"id":"shopb","name":"Efua's Shop","shopCode":"EFUA","ownerWhatsApp":"233550000902","products":[{"id":"bag-b","name":"Bag B","listPrice":100,"floorPrice":90,"stock":2}]}
}}`

func TestBareFeeAnswerAcrossTwoShopsAsksForThePlace(t *testing.T) {
	owner := "233550000902"
	setupChat(t, owner, twoShopsOneOwner)
	c := &chat{seen: map[string]int{}}
	openFeeQuote(feeQuote{ShopID: "shopa", Buyer: "233550000903", Item: "Bag A", Price: 100, Place: "Kasoa", Status: "open"})
	openFeeQuote(feeQuote{ShopID: "shopb", Buyer: "233550000904", Item: "Bag B", Price: 100, Place: "Osu", Status: "open"})
	merchant := catalogue.GetMerchant("shopa")

	handled := inTurn(owner, func() bool {
		return handleMerchantFeeQuote(owner, merchant, "30", "30")
	})
	if !handled {
		t.Fatal("the bare answer must be claimed by the fee lane")
	}
	got := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "Kasoa") || !strings.Contains(got, "Osu") {
		t.Fatalf("she must be asked with both waiting places named: %q", got)
	}
	if len(openFeeQuotes("shopa")) != 1 || len(openFeeQuotes("shopb")) != 1 {
		t.Fatal("a bare answer must bind nothing while two shops wait")
	}

	handled = inTurn(owner, func() bool {
		return handleMerchantFeeQuote(owner, merchant, "Kasoa 30", "kasoa 30")
	})
	if !handled {
		t.Fatal("the named answer must be claimed")
	}
	if len(openFeeQuotes("shopa")) != 0 {
		t.Fatal("the named place's quote must settle")
	}
	if len(openFeeQuotes("shopb")) != 1 {
		t.Fatal("the other shop's quote must stand open")
	}
}
