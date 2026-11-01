package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// The merchant build's pins: the commands she was taught
// outrank an open walk, her link is always hers to have
// back, a description and a shop name can be changed in
// the command shape, a price command in her own mixed
// words executes once verified, a new want reaches her in
// the moment, and the buyer's delivery answer is composed
// from her own settings.

func TestTaughtCommandOutranksAnOpenWalk(t *testing.T) {
	owner := "233550008101"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, owner, "add a product")
	c.replies(t, owner)
	c.say(t, owner, "my catalogue")
	got := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "Handmade Beaded Bag") {
		t.Fatalf("my catalogue mid-walk = %q, want the catalogue itself", got)
	}
	if p := catalogue.FindByName(MerchantID, "my catalogue"); p != nil {
		t.Fatal("the command was eaten as a product name")
	}
	if s := sessions.get(sessionKey(MerchantID, owner)); s != nil && s.Onboarding != nil {
		t.Fatal("the walk should have been dropped by the command")
	}
}

func TestShopLinkCommand(t *testing.T) {
	owner := "233550008102"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, owner, "send me my shop link")
	got := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "SHOP-TEST") {
		t.Fatalf("shop link reply = %q, want the code in it", got)
	}
}

func TestDescriptionAndRenameCommands(t *testing.T) {
	owner := "233550008103"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, owner, "change description of Handmade Beaded Bag to Soft suede finish")
	got := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "Soft suede finish") {
		t.Fatalf("description reply = %q", got)
	}
	if p := catalogue.FindByName(MerchantID, "Handmade Beaded Bag"); p == nil || p.Description != "Soft suede finish" {
		t.Fatalf("description on the product = %+v", p)
	}

	c.say(t, owner, "rename my shop to Test Shop and More")
	got = strings.Join(c.replies(t, owner), "\n")
	m := catalogue.GetMerchant(MerchantID)
	if m == nil || m.Name != "Test Shop and More" {
		t.Fatalf("shop after rename = %+v", m)
	}
	if m.ShopCode != "TEST" {
		t.Fatalf("the code must not move with the name, got %q", m.ShopCode)
	}
	if !strings.Contains(got, "SHOP-TEST") {
		t.Fatalf("rename reply = %q, want the standing code named", got)
	}
}

func TestModelReadPriceChangeExecutesVerified(t *testing.T) {
	owner := "233550008104"
	setupChat(t, owner, shopWithBag)

	u := &understood{Intent: intentChangePrice, Item: "Handmade Beaded Bag", Figure: 900}
	acted := inTurn(owner, func() bool {
		return actOnMerchantUnderstanding(owner, catalogue.GetMerchant(MerchantID), MerchantID, "abeg change di price of Handmade Beaded Bag to 900", u)
	})
	if !acted {
		t.Fatal("a verified price change in her own words should be acted on")
	}
	if p := catalogue.FindByName(MerchantID, "Handmade Beaded Bag"); p == nil || p.ListPrice != 900 {
		t.Fatalf("price after the change = %+v", p)
	}

	// A figure she never wrote is not carried, however the
	// reading names it.
	u2 := &understood{Intent: intentChangePrice, Item: "Handmade Beaded Bag", Figure: 300}
	acted = inTurn(owner, func() bool {
		return actOnMerchantUnderstanding(owner, catalogue.GetMerchant(MerchantID), MerchantID, "change the bag price please", u2)
	})
	if acted {
		t.Fatal("an unverified figure must not be acted on")
	}
	if p := catalogue.FindByName(MerchantID, "Handmade Beaded Bag"); p == nil || p.ListPrice != 900 {
		t.Fatalf("price moved on an unverified reading: %+v", p)
	}
}

func TestNewWantAlertsTheMerchantInTheMoment(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550008105"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.replies(t, ownerAma) // drain anything standing
	c.say(t, buyer, "Do you have a rice cooker?")
	c.replies(t, buyer)
	got := strings.Join(c.replies(t, ownerAma), "\n")
	if !strings.Contains(got, "rice cooker") {
		t.Fatalf("merchant alert = %q, want the buyer's ask quoted to her", got)
	}
}

func TestDraftedBareNoNeverPreemptsTheWantRoad(t *testing.T) {
	owner := "233550008106"
	setupChat(t, owner, shopWithBag)

	// A reading that names an availability ask "ask" and
	// drafts a bare no must be declined by the act, so the
	// dispatch can record the want instead of ending the
	// conversation with nothing written down.
	u := &understood{Intent: intentAsk, Reply: "No, we do not have guinea brocade. Thank you for checking with us.", Words: "Do you have guinea brocade?"}
	acted := actOnBuyerUnderstanding("233550008107", &Session{Stage: "browsing", MerchantID: MerchantID}, catalogue.GetMerchant(MerchantID), MerchantID, u)
	if acted {
		t.Fatal("a drafted bare no to an availability ask must not be sent from the ask act")
	}
}

func TestDeliveryAnswerComesFromHerSettings(t *testing.T) {
	shop := &catalogue.Merchant{
		Name:         "Efua Fabrics",
		DeliveryFee:  25,
		DeliveryNote: "Bikes leave at 4pm daily",
	}
	got := deliveryAnswerFor(shop)
	if !strings.Contains(got, "GH₵25") || !strings.Contains(got, "Bikes leave at 4pm daily") {
		t.Fatalf("delivery answer = %q, want her fee and her note", got)
	}
	quote := &catalogue.Merchant{Name: "Efua Fabrics", DeliveryStyle: "quote", DeliveryNote: "Bikes leave at 4pm daily"}
	if got := deliveryAnswerFor(quote); !strings.Contains(got, "per order") || !strings.Contains(got, "4pm") {
		t.Fatalf("quote delivery answer = %q", got)
	}
}
