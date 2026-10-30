package agent

// Buy again, same details, and take it at the price: the
// lanes where understanding replaces interrogation. Each
// test drives the real inbound path with no model, so the
// structural roads are what is proven here; the thinking
// road that names the same acts is proven live.

import (
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

const reorderOwner = "233550004211"
const reorderBuyer = "233550004212"
const reorderBuyer2 = "233550004213"

const reorderSeed = `{"merchants":{"efua":{"id":"efua","name":"Efua's Store","shopCode":"EFUA","ownerWhatsApp":"` + reorderOwner + `","payoutPhone":"` + reorderOwner + `","dailyGiveawayAllowance":300,"deliveryFee":25,"deliveryPaidTo":"merchant","products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":5}]}}}`

// seedReorderPast plants the completed order the lanes
// repeat: paid, delivered to Osu under the name Kojo Tsey.
func seedReorderPast(buyer string, price float64) {
	orders.Default.RecordOrder("EFUA-0001", orders.Order{
		From: buyer, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ProductName: "Handmade Beaded Bag", Price: price, Total: price + 30,
		Status: "paid", MerchantID: "efua", PaymentPath: "seev",
		Fulfilment: "delivery", Location: "Osu", PackageName: "Kojo Tsey",
		DeliveryFee: 30, DeliveryState: "received",
	})
}

func TestReorderByPlainWords(t *testing.T) {
	setupChat(t, reorderOwner, reorderSeed)
	c := &chat{seen: map[string]int{}}
	seedReorderPast(reorderBuyer, 850)

	c.say(t, reorderBuyer, "SHOP-EFUA")
	c.replies(t, reorderBuyer)
	c.say(t, reorderBuyer, "Can I buy this again?")
	got := c.joined(t, reorderBuyer)
	if !strings.Contains(got, "Same again") || !strings.Contains(got, "Kojo Tsey") || !strings.Contains(got, "Osu") {
		t.Fatalf("reorder recap = %q", got)
	}
	c.say(t, reorderBuyer, "Yes")
	c.replies(t, reorderBuyer)
	o := orders.Default.NewestFor("efua", reorderBuyer)
	if o == nil || o.Reference == "EFUA-0001" || o.PackageName != "Kojo Tsey" || o.Location != "Osu" || o.Fulfilment != "delivery" || o.Price != 850 {
		t.Fatalf("repeated order = %+v", o)
	}
	if o.DeliveryFee != 25 {
		t.Fatalf("repeat delivery fee should be today's set fee 25, got %v", o.DeliveryFee)
	}
}

func TestReorderNamesAMovedPrice(t *testing.T) {
	setupChat(t, reorderOwner, reorderSeed)
	c := &chat{seen: map[string]int{}}
	seedReorderPast(reorderBuyer, 730) // she haggled it down last time

	c.say(t, reorderBuyer, "SHOP-EFUA")
	c.replies(t, reorderBuyer)
	c.say(t, reorderBuyer, "I want the same thing again")
	got := c.joined(t, reorderBuyer)
	if !strings.Contains(got, "was GH₵730") || !strings.Contains(got, "GH₵850 now") {
		t.Fatalf("moved price must be named: %q", got)
	}
}

// A reference presented from a different number is
// refused, however the story is told: the recap recites
// the original buyer's delivery place and package name,
// references are serial, and a guessable number is not a
// credential. Her "other number" may be true, but the lane
// cannot tell it from a stranger, so it answers neutrally
// and no detail crosses. The order's own buyer, quoting
// the same reference with reorder words, is served as
// before.
func TestReorderByReferenceFromAnotherNumberIsRefused(t *testing.T) {
	setupChat(t, reorderOwner, reorderSeed)
	c := &chat{seen: map[string]int{}}
	seedReorderPast(reorderBuyer, 850)

	c.say(t, reorderBuyer2, "SHOP-EFUA")
	c.replies(t, reorderBuyer2)
	c.say(t, reorderBuyer2, "This is my other number. My order was EFUA-0001, can I repurchase it?")
	got := c.joined(t, reorderBuyer2)
	if !strings.Contains(got, "different number") {
		t.Fatalf("a foreign reference must get the neutral answer: %q", got)
	}
	for _, leak := range []string{"Same again", "Kojo Tsey", "Osu", "Handmade Beaded Bag"} {
		if strings.Contains(got, leak) {
			t.Fatalf("the foreign reference leaked %q: %q", leak, got)
		}
	}
	if o := orders.Default.NewestFor("efua", reorderBuyer2); o != nil {
		t.Fatalf("no order may be built for the stranger: %+v", o)
	}

	// The same reference in its own buyer's mouth, with
	// reorder words the net once missed ("I want this order
	// again" was answered as a status check), opens the lane.
	c.say(t, reorderBuyer, "SHOP-EFUA")
	c.replies(t, reorderBuyer)
	c.say(t, reorderBuyer, "I want this order again EFUA-0001")
	got = c.joined(t, reorderBuyer)
	if !strings.Contains(got, "Same again") {
		t.Fatalf("the owner reordering by reference = %q", got)
	}
}

func TestTakeItAtTheListPriceClosesAtOnce(t *testing.T) {
	setupChat(t, reorderOwner, reorderSeed)
	c := &chat{seen: map[string]int{}}

	c.say(t, reorderBuyer, "SHOP-EFUA")
	c.replies(t, reorderBuyer)
	c.say(t, reorderBuyer, "Handmade Beaded Bag")
	c.replies(t, reorderBuyer)
	c.say(t, reorderBuyer, "I will take it at 850")
	got := c.joined(t, reorderBuyer)
	if !strings.Contains(got, "is yours for GH₵850") {
		t.Fatalf("take at list should close at once: %q", got)
	}
	if strings.Contains(got, "What do you say?") {
		t.Fatalf("no haggle may open over a full-price take: %q", got)
	}
}

func TestSameDetailsAdoptedWholeAtTheNameStep(t *testing.T) {
	setupChat(t, reorderOwner, reorderSeed)
	c := &chat{seen: map[string]int{}}
	seedReorderPast(reorderBuyer, 850)

	c.say(t, reorderBuyer, "SHOP-EFUA")
	c.replies(t, reorderBuyer)
	c.say(t, reorderBuyer, "Handmade Beaded Bag")
	c.replies(t, reorderBuyer)
	c.say(t, reorderBuyer, "I will take it at 850")
	c.replies(t, reorderBuyer) // deal; the usual question or the choice follows
	c.say(t, reorderBuyer, "Different")
	c.replies(t, reorderBuyer)
	c.say(t, reorderBuyer, "Delivery")
	c.replies(t, reorderBuyer)
	c.say(t, reorderBuyer, "Osu")
	got := c.joined(t, reorderBuyer)
	if !strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("expected the name step, got %q", got)
	}
	c.say(t, reorderBuyer, "Use the same details as last time")
	got = c.joined(t, reorderBuyer)
	if strings.Contains(got, "message for the seller") || strings.Contains(got, "Use the same details") {
		t.Fatalf("her instruction must be applied, never printed or re-asked: %q", got)
	}
	if !strings.Contains(got, "Kojo Tsey") {
		t.Fatalf("the recap must carry the remembered name: %q", got)
	}
	c.say(t, reorderBuyer, "Yes")
	c.replies(t, reorderBuyer)
	o := orders.Default.NewestFor("efua", reorderBuyer)
	if o == nil || o.Reference == "EFUA-0001" || o.PackageName != "Kojo Tsey" {
		t.Fatalf("order after same-details adoption = %+v", o)
	}
}
