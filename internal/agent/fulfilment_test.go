package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/money"
)

// The fulfilment step, from playing buyer myself.
// A price agreement used to complete the sale on the
// spot, delivery fee and all, without the agent ever asking where
// the goods were going or how the buyer would receive them. In my
// own chats, delivery method, location and cost are what actually
// close a sale, so now every agreement waits here until the buyer
// says how the goods reach her, and a delivery fee is only ever
// charged on a delivery she chose, to a place she named.

// haggleToAgreement plays the shape I played: an offer goes
// in; if the agent counters, the buyer meets the counter. Either
// way an agreement is reached and the fulfilment step is open. It
// returns the agreed price and the agreement replies (the deal
// line and the fulfilment question pieces).
func haggleToAgreement(t *testing.T, c *chat, buyer string, firstOffer float64) (float64, string) {
	t.Helper()
	c.say(t, buyer, moneyFigure(firstOffer))
	got := strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "yours for") {
		return firstOffer, got
	}
	figs := figuresIn(got)
	if len(figs) == 0 {
		t.Fatalf("first offer reply = %q, want a counter or an agreement", got)
	}
	standing := figs[len(figs)-1]
	c.say(t, buyer, moneyFigure(standing))
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "yours for") {
		t.Fatalf("meeting the counter = %q, want the agreement", got)
	}
	return standing, got
}

func TestFulfilmentSheaShapeDeliveryWithPlaceInOneMessage(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550007001"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	// The exact shape I just played live: view the shea
	// butter, offer, take the counter, meet it.
	c.say(t, buyer, "Shea butter")
	c.last(t, buyer)
	price, ask := haggleToAgreement(t, c, buyer, 100)

	// The agreement does not complete anything: the fulfilment
	// question opens in pieces, on Efua's own bus terms, with the
	// pickup door held open and the place asked for.
	for _, want := range []string{"How do you want to get it?", "Sent by bus to your station", "Delivery is GH₵25.", "pick it up yourself, no delivery fee", "Which one, and where are you?"} {
		if !strings.Contains(ask, want) {
			t.Fatalf("fulfilment ask = %q, missing %q", ask, want)
		}
	}
	if ordersCount(t, dir) != 0 {
		t.Fatal("an order was recorded at the agreement, before fulfilment")
	}

	// Delivery chosen, with the place in the same message: the
	// completion totals it up, charges the fee, and says where
	// the goods go.
	c.say(t, buyer, "Send it to Koforidua")
	c.finishDetails(t, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	wantBreakdown := "Raw Shea Butter (500g): " + money.GHS(price) + "\nDelivery to Koforidua: GH₵25\nTotal: " + money.GHS(price+25)
	if !strings.Contains(got, wantBreakdown) || !strings.Contains(got, "Pay here") {
		t.Fatalf("delivery completion = %q, want %q and the link", got, wantBreakdown)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 {
		t.Fatalf("orders = %v, want 1", orders)
	}
	o := orders[0]
	if o["fulfilment"] != "delivery" || o["location"] != "Koforidua" || o["price"] != price || o["deliveryFee"] != 25.0 || o["total"] != price+25 {
		t.Fatalf("order = %v, want delivery to Koforidua at item %v, fee 25, total %v", o, price, price+25)
	}
	// The merchant hears how the goods move: the note, the place,
	// and the fee inside the total.
	notes := strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(notes, "Delivery: Sent by bus to your station; you collect it the next day, to Koforidua, fee GH₵25 included in the total.") {
		t.Fatalf("merchant notice = %q, want the delivery line with place and fee", notes)
	}
	// The day book still books the item alone.
	day := dayState(t, dir, "efua")
	if day["sales"] != 1.0 || day["revenue"] != price || day["givenAway"] != 120-price {
		t.Fatalf("Efua day book = %v, want the item figures only", day)
	}
}

func TestFulfilmentDeliveryThenBarePlaceFollowUp(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550007002"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.last(t, buyer)
	c.say(t, buyer, "180")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Rider brings it to you in town") {
		t.Fatalf("fulfilment ask = %q, want Ama's rider terms in her own words", got)
	}
	// Delivery with no place anywhere yet: one short follow-up.
	c.say(t, buyer, "deliver it")
	if got := c.last(t, buyer); got != "Where should it go?" {
		t.Fatalf("delivery with no place = %q, want the one follow-up", got)
	}
	if ordersCount(t, dir) != 0 {
		t.Fatal("an order was recorded before the place was given")
	}
	// A bare place name completes the step.
	c.say(t, buyer, "Kumasi")
	c.finishDetails(t, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Smoked Tilapia: GH₵180\nDelivery to Kumasi: GH₵15\nTotal: GH₵195") || !strings.Contains(got, "Pay here") {
		t.Fatalf("delivery completion = %q", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["fulfilment"] != "delivery" || orders[0]["location"] != "Kumasi" || orders[0]["total"] != 195.0 {
		t.Fatalf("orders = %v, want delivery to Kumasi at total 195", orders)
	}
}

func TestFulfilmentPickupPaysNoFee(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550007003"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Handmade Beaded Bag")
	c.last(t, buyer)
	c.say(t, buyer, "850")
	c.replies(t, buyer)
	c.say(t, buyer, "I will come and pick it up myself")
	c.finishDetails(t, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Handmade Beaded Bag: GH₵850\nTotal: GH₵850") || !strings.Contains(got, "Pay here") {
		t.Fatalf("pickup completion = %q", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["fulfilment"] != "pickup" || orders[0]["total"] != 850.0 {
		t.Fatalf("orders = %v, want a pickup at the item price alone", orders)
	}
	if _, present := orders[0]["deliveryFee"]; present {
		t.Fatalf("a pickup recorded a delivery fee: %v", orders[0])
	}
	notes := strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(notes, "Pickup: the buyer will come and pick it up herself.") {
		t.Fatalf("merchant notice = %q, want the pickup line", notes)
	}
}

func TestFulfilmentMeetUpWithPlaceInTheMessage(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550007004"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.last(t, buyer)
	c.say(t, buyer, "180")
	c.replies(t, buyer)
	c.say(t, buyer, "let's meet at Circle")
	c.finishDetails(t, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	// The agent promises nothing about the seller coming: she has
	// been told, and the place is on the order. That is all.
	if !strings.Contains(got, "I have told the seller you want to meet at Circle to collect it. The place is on your order.") || !strings.Contains(got, "Smoked Tilapia: GH₵180\nTotal: GH₵180") {
		t.Fatalf("meet-up completion = %q", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["fulfilment"] != "meet" || orders[0]["location"] != "Circle" || orders[0]["total"] != 180.0 {
		t.Fatalf("orders = %v, want a meet-up at Circle, total the item price", orders)
	}
	if _, present := orders[0]["deliveryFee"]; present {
		t.Fatalf("a meet-up recorded a delivery fee: %v", orders[0])
	}
	notes := strings.Join(c.replies(t, ownerAma), "\n")
	if !strings.Contains(notes, "Meet up: the buyer wants to meet at Circle to collect it.") {
		t.Fatalf("merchant notice = %q, want the meet-up line for the seller to judge", notes)
	}
}

func TestFulfilmentMeetUpAsksThePlaceOnce(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550007005"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.last(t, buyer)
	c.say(t, buyer, "180")
	c.replies(t, buyer)
	c.say(t, buyer, "we can meet")
	if got := c.last(t, buyer); got != "Where should we meet you?" {
		t.Fatalf("meet-up with no place = %q, want the one follow-up", got)
	}
	if ordersCount(t, dir) != 0 {
		t.Fatal("an order was recorded before the meeting place was given")
	}
	c.say(t, buyer, "the station")
	c.finishDetails(t, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "meet at the station") {
		t.Fatalf("meet-up completion = %q", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["fulfilment"] != "meet" || orders[0]["location"] != "the station" {
		t.Fatalf("orders = %v, want a meet-up at the station", orders)
	}
}

func TestFulfilmentCollectorNamedInTheMessage(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550007006"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Shea butter")
	c.last(t, buyer)
	c.say(t, buyer, "120")
	c.replies(t, buyer)
	c.say(t, buyer, "I will send my brother Kwame to pick it up")
	c.finishNote(t, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "my brother Kwame is coming for it") || !strings.Contains(got, "Raw Shea Butter (500g): GH₵120\nTotal: GH₵120") {
		t.Fatalf("collector completion = %q", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["fulfilment"] != "pickup" || orders[0]["collector"] != "my brother Kwame" || orders[0]["total"] != 120.0 {
		t.Fatalf("orders = %v, want a pickup by my brother Kwame at the item price", orders)
	}
	// The seller needs to know who to hand the goods to: the
	// notice carries the collector, by name.
	notes := strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(notes, "Pickup: a collector is coming for it: my brother Kwame.") {
		t.Fatalf("merchant notice = %q, want the collector named", notes)
	}
}

func TestFulfilmentCollectorUnnamedThenNamed(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550007007"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Handmade Beaded Bag")
	c.last(t, buyer)
	c.say(t, buyer, "850")
	c.replies(t, buyer)
	c.say(t, buyer, "I will send somebody to pick it up")
	if got := c.last(t, buyer); got != "Who is coming for it?" {
		t.Fatalf("unnamed collector = %q, want the one follow-up", got)
	}
	if ordersCount(t, dir) != 0 {
		t.Fatal("an order was recorded before the collector was named")
	}
	c.say(t, buyer, "My rider")
	c.finishNote(t, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "My rider is coming for it") {
		t.Fatalf("collector completion = %q", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["fulfilment"] != "pickup" || orders[0]["collector"] != "My rider" {
		t.Fatalf("orders = %v, want the rider recorded as collector", orders)
	}
	notes := strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(notes, "Pickup: a collector is coming for it: My rider.") {
		t.Fatalf("merchant notice = %q, want the collector carried to the seller", notes)
	}
}

func TestFulfilmentStatedLocationIsNotAskedTwice(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550007008"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	// She says where she is long before any price is agreed, the
	// way buyers do (one buyer's first visible message was his town).
	c.say(t, buyer, "I am in Koforidua")
	c.replies(t, buyer)
	c.say(t, buyer, "Shea butter")
	c.last(t, buyer)
	_, ask := haggleToAgreement(t, c, buyer, 100)
	if strings.Contains(ask, "where are you?") {
		t.Fatalf("fulfilment ask = %q, she was asked for a place she already gave", ask)
	}
	if !strings.Contains(ask, "It goes to Koforidua.") || !strings.Contains(ask, "Which one?") {
		t.Fatalf("fulfilment ask = %q, want her place confirmed in the delivery line", ask)
	}
	// Choosing delivery completes at once: no where question, ever.
	c.say(t, buyer, "deliver it")
	c.finishDetails(t, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "Where should it go?") || !strings.Contains(got, "Pay here") {
		t.Fatalf("delivery with a known place = %q", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["location"] != "Koforidua" {
		t.Fatalf("orders = %v, want her stated place on the order", orders)
	}
}

func TestFulfilmentCancelDropsTheSaleCleanly(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550007009"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Shea butter")
	c.last(t, buyer)
	c.say(t, buyer, "120")
	c.replies(t, buyer)
	c.say(t, buyer, "cancel")
	if got := c.last(t, buyer); !strings.Contains(got, "Okay, cancelled") {
		t.Fatalf("cancel at fulfilment = %q", got)
	}
	if ordersCount(t, dir) != 0 {
		t.Fatal("a cancelled fulfilment recorded an order")
	}
	// Nothing haunts her: the same item is still in front of her,
	// and a fresh agreement completes normally.
	c.say(t, buyer, "120")
	c.replies(t, buyer)
	c.say(t, buyer, "I will pick it up")
	c.finishDetails(t, buyer)
	if ordersCount(t, dir) != 1 {
		t.Fatal("buying after a cancel did not complete")
	}
	day := dayState(t, dir, "efua")
	if day["sales"] != 1.0 || day["revenue"] != 120.0 {
		t.Fatalf("Efua day book = %v, want exactly the one completed sale", day)
	}
}

func TestFulfilmentQuestionDriftKeepsThePendingSale(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550007010"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.last(t, buyer)
	c.say(t, buyer, "180")
	c.replies(t, buyer)
	// A question about the sale itself, mid-step: answered with
	// the total, and the fulfilment question held up again. The
	// pending sale does not move.
	c.say(t, buyer, "How much is delivery?")
	replies := c.replies(t, buyer)
	got := strings.Join(replies, "\n")
	if !strings.Contains(got, "Delivery is GH₵15.") || !strings.Contains(got, "GH₵195 in total") {
		t.Fatalf("delivery question at fulfilment = %q, want the fee and the one total", got)
	}
	if last := replies[len(replies)-1]; last != "So, how do you want to get it: we send it, or you pick it up?" {
		t.Fatalf("after the question, last reply = %q, want the fulfilment question held up", last)
	}
	if ordersCount(t, dir) != 0 {
		t.Fatal("a question at fulfilment recorded an order")
	}
	c.say(t, buyer, "I will pick it up")
	c.finishDetails(t, buyer)
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["total"] != 180.0 {
		t.Fatalf("orders = %v, want the pending sale completed at the item price", orders)
	}
}

func TestFulfilmentNoTermsDeclinedLocationStillCompletes(t *testing.T) {
	dir, c := setupTwoShops(t)
	buyer := "233550007011"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.last(t, buyer)
	c.say(t, buyer, "I can offer 100")
	c.last(t, buyer)
	c.say(t, buyer, "160")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Where are you based?") {
		t.Fatalf("agreement in a no-terms shop = %q, want the where question", got)
	}
	// She will not say where she is. The sale is not blocked: it
	// completes, and the order and the seller are told plainly
	// that no location was given.
	c.say(t, buyer, "I will not say")
	c.finishDetails(t, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Pay here") {
		t.Fatalf("declined location = %q, want the sale completed anyway", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["fulfilment"] != "arranged" || orders[0]["total"] != 160.0 {
		t.Fatalf("orders = %v, want an arranged sale at the item price", orders)
	}
	if _, present := orders[0]["location"]; present {
		t.Fatalf("a location was invented for a buyer who declined: %v", orders[0])
	}
	notes := strings.Join(c.replies(t, ownerAma), "\n")
	if !strings.Contains(notes, "Delivery: to be arranged with the buyer. No location given.") {
		t.Fatalf("merchant notice = %q, want the no-location line", notes)
	}
}
