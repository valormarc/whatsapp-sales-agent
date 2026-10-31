package agent

import (
	"strings"
	"testing"
)

// Who takes the delivery money: it
// is the merchant's own setting, DeliveryPaidTo, and the default
// is "carrier" — most buyers pay whoever physically brings the
// item, on arrival, and the fee never enters the money that moves
// through here. These tests pin both purses for both seed shops:
// the charged totals, the order record, the buyer's two-purse
// wording, the merchant's notice, and the chat control that
// flips the setting.

// carrierShopCatalogue is the Twitter shops with the setting
// stripped out entirely, so what is tested is the unset default
// itself, not a value the fixture happened to carry.
var carrierShopCatalogue = strings.ReplaceAll(twitterShopCatalogue, `"deliveryPaidTo":"merchant",`, "")

func setupCarrierShops(t *testing.T) (string, *chat) {
	t.Helper()
	dir := setupChat(t, "233550009999", carrierShopCatalogue)
	return dir, &chat{dir: dir}
}

func TestCarrierDeliveryCloseNamesBothPurses(t *testing.T) {
	dir, c := setupCarrierShops(t)
	buyer := "233550009001"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Handmade Beaded Bag")
	c.last(t, buyer)
	c.say(t, buyer, "850")
	// The fulfilment ask itself names the purse: the fee is paid
	// to the station, on arrival, not folded into her payment.
	ask := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(ask, "Delivery is GH₵25, paid to the station when it reaches you.") {
		t.Fatalf("fulfilment ask = %q, want the carrier purse named", ask)
	}
	c.say(t, buyer, "Send it to Koforidua")
	c.finishDetails(t, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Handmade Beaded Bag - GH₵850") ||
		!strings.Contains(got, "Delivery to Koforidua: GH₵25 (paid to the station on arrival)") ||
		!strings.Contains(got, "Total: GH₵850") ||
		!strings.Contains(got, "That is GH₵875 in all, with the delivery.") ||
		!strings.Contains(got, "Pay here") {
		t.Fatalf("carrier close = %q, want both purses and the link", got)
	}
	// The order: charged total is the item alone, but the books
	// still know the quoted fee and whose it is.
	o := ordersOnDisk(t, dir)[0]
	if o["total"] != 850.0 || o["price"] != 850.0 || o["deliveryFee"] != 25.0 || o["deliveryPaidTo"] != "carrier" {
		t.Fatalf("order = %v, want total 850, quoted fee 25, paid to the carrier", o)
	}
	// The merchant's notice never counts the fee as hers.
	notes := strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(notes, "The GH₵25 delivery is hers to pay the station on arrival; your money is the GH₵850.") {
		t.Fatalf("merchant notice = %q", notes)
	}
	if strings.Contains(notes, "included in the total") {
		t.Fatalf("carrier notice speaks of the fee as hers: %q", notes)
	}
	// The day book books the item price alone, as it always has.
	if day := dayState(t, dir, "efua"); day["revenue"] != 850.0 {
		t.Fatalf("day book = %v, want revenue 850 (the item, never the delivery money)", day)
	}
}

func TestCarrierDeliveryCloseAmaPaysTheRider(t *testing.T) {
	dir, c := setupCarrierShops(t)
	buyer := "233550009002"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "Smoked Tilapia")
	c.last(t, buyer)
	c.say(t, buyer, "180")
	ask := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(ask, "paid to the rider when it reaches you") {
		t.Fatalf("fulfilment ask = %q, want the rider named as the purse", ask)
	}
	c.say(t, buyer, "Deliver it to Madina")
	c.finishDetails(t, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Smoked Tilapia - GH₵180") || !strings.Contains(got, "Delivery to Madina: GH₵15 (paid to the rider on arrival)") || !strings.Contains(got, "Total: GH₵180") || !strings.Contains(got, "That is GH₵195 in all, with the delivery.") {
		t.Fatalf("carrier close = %q", got)
	}
	o := ordersOnDisk(t, dir)[0]
	if o["total"] != 180.0 || o["deliveryFee"] != 15.0 || o["deliveryPaidTo"] != "carrier" {
		t.Fatalf("order = %v, want total 180, quoted fee 15, paid to the carrier", o)
	}
	notes := strings.Join(c.replies(t, ownerAma), "\n")
	if !strings.Contains(notes, "The GH₵15 delivery is hers to pay the rider on arrival; your money is the GH₵180.") {
		t.Fatalf("merchant notice = %q", notes)
	}
}

func TestMerchantModeDeliveryStillChargesOneTotal(t *testing.T) {
	dir, c := setupTwitterShops(t) // both shops pinned "merchant"
	buyer := "233550009003"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Shea butter")
	c.last(t, buyer)
	c.say(t, buyer, "120")
	c.replies(t, buyer)
	c.say(t, buyer, "Send it to Koforidua")
	c.finishDetails(t, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Raw Shea Butter (500g) - GH₵120") || !strings.Contains(got, "Delivery to Koforidua: GH₵25") || !strings.Contains(got, "Total: GH₵145") {
		t.Fatalf("merchant-mode close = %q, want the fee inside one total", got)
	}
	o := ordersOnDisk(t, dir)[0]
	if o["total"] != 145.0 || o["deliveryFee"] != 25.0 || o["deliveryPaidTo"] != "merchant" {
		t.Fatalf("order = %v, want total 145 with the fee hers", o)
	}
	notes := strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(notes, "fee GH₵25 included in the total") {
		t.Fatalf("merchant notice = %q", notes)
	}
}

func TestMerchantFlipsWhoTakesTheDeliveryMoney(t *testing.T) {
	dir, c := setupCarrierShops(t)

	// Her catalogue states the default in plain words.
	c.say(t, ownerEfua, "my catalogue")
	if got := strings.Join(c.replies(t, ownerEfua), "\n"); !strings.Contains(got, "The delivery money is not yours: your buyers pay whoever brings the item, when it arrives.") {
		t.Fatalf("my catalogue = %q, want the carrier setting in plain words", got)
	}

	// She takes the delivery money herself: the confirmation says
	// so, the catalogue follows, and the next close charges it.
	c.say(t, ownerEfua, "delivery money comes to me")
	if got := c.last(t, ownerEfua); !strings.Contains(got, "The delivery money now comes to you") {
		t.Fatalf("flip confirmation = %q", got)
	}
	c.say(t, ownerEfua, "my catalogue")
	if got := strings.Join(c.replies(t, ownerEfua), "\n"); !strings.Contains(got, "The delivery money comes to you, inside the total your buyers pay here.") {
		t.Fatalf("my catalogue after the flip = %q", got)
	}
	buyer := "233550009004"
	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Shea butter")
	c.last(t, buyer)
	c.say(t, buyer, "120")
	c.replies(t, buyer)
	c.say(t, buyer, "Send it to Koforidua")
	c.finishDetails(t, buyer)
	c.replies(t, buyer)
	if o := ordersOnDisk(t, dir)[0]; o["total"] != 145.0 || o["deliveryPaidTo"] != "merchant" {
		t.Fatalf("close after the flip = %v, want the fee inside the total", o)
	}

	// And back again, in the other plain shape.
	c.say(t, ownerEfua, "buyer pays the delivery")
	if got := c.last(t, ownerEfua); !strings.Contains(got, "What comes to you here is the item money only.") {
		t.Fatalf("flip-back confirmation = %q", got)
	}
	buyer2 := "233550009005"
	c.say(t, buyer2, "SHOP-EFUA")
	c.replies(t, buyer2)
	c.say(t, buyer2, "Shea butter")
	c.last(t, buyer2)
	c.say(t, buyer2, "120")
	c.replies(t, buyer2)
	c.say(t, buyer2, "Send it to Koforidua")
	c.finishDetails(t, buyer2)
	got := strings.Join(c.replies(t, buyer2), "\n")
	if !strings.Contains(got, "Total: GH₵120") || !strings.Contains(got, "That is GH₵145 in all, with the delivery.") {
		t.Fatalf("close after the flip back = %q", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 2 {
		t.Fatalf("orders = %v, want two closes", orders)
	}
	// The order book is a map, so its slice order is not the
	// closing order: find each close by its buyer.
	byBuyer := map[string]map[string]any{}
	for _, o := range orders {
		byBuyer[o["from"].(string)] = o
	}
	if o := byBuyer[buyer2]; o == nil || o["total"] != 120.0 || o["deliveryPaidTo"] != "carrier" {
		t.Fatalf("second close = %v, want it back on the carrier purse", byBuyer[buyer2])
	}
}

func TestTotalQuestionMidHaggleNamesBothPurses(t *testing.T) {
	_, c := setupCarrierShops(t)
	buyer := "233550009006"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.last(t, buyer)
	c.say(t, buyer, "100")
	c.replies(t, buyer)
	c.say(t, buyer, "How much in total?")
	got := c.last(t, buyer)
	// Both purses, and the two together: she never has to add
	// anything up herself, and never mistakes the fee for part
	// of what she pays here.
	if !strings.Contains(got, "Smoked Tilapia - GH₵") ||
		!strings.Contains(got, "Delivery: GH₵15 (paid to the rider on arrival)") ||
		!strings.Contains(got, " in all, with the delivery.") {
		t.Fatalf("total answer = %q, want both purses and the sum of the two", got)
	}
	figs := figuresIn(got)
	if len(figs) != 4 || figs[0] != figs[2] || figs[0]+figs[1] != figs[3] {
		t.Fatalf("total answer figures = %v in %q, want standing price, fee, the total here, and the sum of the two purses", figs, got)
	}
}

func TestCarrierConfirmationQuoteNamesTheCarrierPurse(t *testing.T) {
	_, c := setupCarrierShops(t)
	buyer := "233550009007"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Shea butter")
	c.last(t, buyer)
	c.say(t, buyer, "I'll buy it")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "The Raw Shea Butter (500g) is GH₵120. If you want it sent, delivery is GH₵25, paid to the station when it reaches you.") ||
		!strings.Contains(got, "Shall I send the payment link?") {
		t.Fatalf("confirmation quote = %q, want the price and the carrier purse named", got)
	}
	if strings.Contains(got, "plus GH₵25 delivery if you want it sent to you") {
		t.Fatalf("carrier quote used the merchant-purse wording: %q", got)
	}
}
