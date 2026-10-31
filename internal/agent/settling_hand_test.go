package agent

// The settling misread, pinned. A buyer who has tapped
// Done and is settling her picks one by one offers on
// the whole hand — "I will give you 330 for all" over a
// 350 hand, in the owner's own phone test. Before this
// fix the settling read that figure item by item: above
// each item's own price, it closed each item at its full
// price and said Deal twice, and her offer was never
// heard. These tests replay that script against the
// hand fixture (rack 270, halter 150; 420 in all) and
// pin the roads: a hand-shaped offer mid-settling opens
// the total haggle while nothing has settled; once an
// item stands at an agreed price, the agent asks the
// one scope question and spends the figure only as she
// declares; and an offer above a single item's price
// still closes at the price, never above it.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

// pickRackAndHalter stands a buyer up in the hand shop,
// picks the rack and the halter dress, and taps Done, so
// the settling opens on the rack.
func pickRackAndHalter(t *testing.T, c *chat, buyer, idPrefix string) {
	t.Helper()
	c.say(t, buyer, "SHOP-HAND")
	c.replies(t, buyer)
	HandleInbound(Inbound{From: buyer, ListID: "shelf_rack", MessageID: "wamid." + idPrefix + "-P1"})
	HandleInbound(Inbound{From: buyer, ListID: "shelf_halter", MessageID: "wamid." + idPrefix + "-P2"})
	HandleInbound(Inbound{From: buyer, ListID: "shelfdone", MessageID: "wamid." + idPrefix + "-D"})
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "one by one") || !strings.Contains(got, "Double Clothes Rack") {
		t.Fatalf("the settling should open on the rack, got %q", got)
	}
}

// meetTheCounter offers the last figure named in the
// replies and returns the new replies, for the haggle
// dances that need one more step to agree.
func meetTheCounter(t *testing.T, c *chat, buyer, got string) string {
	t.Helper()
	if strings.Contains(got, "yours for") {
		return got
	}
	figs := figuresIn(got)
	if len(figs) == 0 {
		t.Fatalf("the counter should name a figure, got %q", got)
	}
	c.say(t, buyer, moneyFigure(figs[len(figs)-1]))
	return strings.Join(c.replies(t, buyer), "\n")
}

func TestSettlingHandOfferOpensTheTotalHaggle(t *testing.T) {
	dir := setupChat(t, totalHandOwner, totalHandCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009411"
	pickRackAndHalter(t, c, buyer, "SH1")

	c.say(t, buyer, "I will give you 400 for all")
	got := strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "Double Clothes Rack is yours for") {
		t.Fatalf("the hand offer must not close the rack at its own price, got %q", got)
	}
	if !strings.Contains(got, "For all of them together") || !strings.Contains(got, "GH₵420") {
		t.Fatalf("the offer should open the haggle on the whole 420 hand, got %q", got)
	}
	got = meetTheCounter(t, c, buyer, got)
	m := orderTotalRe.FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("the deal should name the order total, got %q", got)
	}
	agreedTotal, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil || agreedTotal < 380 || agreedTotal > 420 {
		t.Fatalf("the agreed total should sit between the summed floors (380) and the hand (420), got %v", agreedTotal)
	}
	s := sessions.get(sessionKey(MerchantID, buyer))
	if s == nil || len(s.TotalHand) != 0 || len(s.Basket) != 1 || s.Product == nil || s.Product.ID != "halter" {
		t.Fatalf("the hand should be apportioned into the basket by agreement time, got %+v", s)
	}

	c.say(t, buyer, "Send it to Koforidua")
	if got := c.last(t, buyer); !strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("the package name should be asked once, got %q", got)
	}
	c.say(t, buyer, "Efua Mensima")
	c.last(t, buyer)
	c.say(t, buyer, "no")
	c.last(t, buyer)
	c.say(t, buyer, "yes")
	c.replies(t, buyer)

	all := orders.Default.All()
	if len(all) != 1 {
		t.Fatalf("the hand should complete as one order, got %d", len(all))
	}
	order := &all[0]
	if len(order.Items) != 2 || order.Price != agreedTotal {
		t.Fatalf("order = %+v, want two items totalling %v", order, agreedTotal)
	}
	floors := map[string]float64{"Double Clothes Rack": 250, "Halter Pleated Maxi Dress": 130}
	var sum float64
	for _, it := range order.Items {
		if it.Price < floors[it.Name] {
			t.Fatalf("item %q settled at %v, below its own last price %v", it.Name, it.Price, floors[it.Name])
		}
		sum += it.Price
	}
	if sum != order.Price {
		t.Fatalf("the shares should sum to the total exactly: items %v, order %v", sum, order.Price)
	}
}

func TestSettlingHandOfferMidHaggleOpensTheTotalHaggle(t *testing.T) {
	dir := setupChat(t, totalHandOwner, totalHandCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009412"
	pickRackAndHalter(t, c, buyer, "SH2")

	// One counter into the rack's own haggle, so the
	// session is mid-haggle when the hand offer lands.
	c.say(t, buyer, "260")
	got := strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "yours for") {
		t.Fatalf("260 should not close the 270 rack outright, got %q", got)
	}
	c.say(t, buyer, "I will give you 400 for all")
	got = strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "is yours for GH₵270") {
		t.Fatalf("the hand offer must not close the rack at its list price mid-haggle, got %q", got)
	}
	if !strings.Contains(got, "For all of them together") || !strings.Contains(got, "GH₵420") {
		t.Fatalf("the offer should open the haggle on the whole 420 hand, got %q", got)
	}
	s := sessions.get(sessionKey(MerchantID, buyer))
	if s == nil || len(s.TotalHand) != 2 {
		t.Fatalf("the haggle should be over the two-item hand, got %+v", s)
	}
}

func TestSettlingHandOfferAfterOneSettledAsksScope(t *testing.T) {
	dir := setupChat(t, totalHandOwner, totalHandCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009413"
	pickRackAndHalter(t, c, buyer, "SH3")

	// The rack settles at its price; the halter comes up.
	c.say(t, buyer, "270")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Double Clothes Rack is yours for GH₵270") || !strings.Contains(got, "Now the next one you picked:") {
		t.Fatalf("the rack should settle at its price and the halter come up, got %q", got)
	}

	// Now the hand-shaped offer: one item already stands,
	// so the agent asks what the figure covers and binds
	// nothing.
	c.say(t, buyer, "I will give you 400 for all")
	got = strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "Halter Pleated Maxi Dress is yours for") {
		t.Fatalf("the hand offer must not close the halter before its scope is declared, got %q", got)
	}
	if !strings.Contains(got, "already settled") || !strings.Contains(got, "GH₵270") || !strings.Contains(got, "for everything") {
		t.Fatalf("the scope question should name the settled item and ask, got %q", got)
	}
	s := sessions.get(sessionKey(MerchantID, buyer))
	if s == nil || s.PendingHandOffer != 400 {
		t.Fatalf("the figure should wait on the session, got %+v", s)
	}

	// "Everything": the figure covers the settled rack
	// too, so 130 remains as her offer on the halter.
	c.say(t, buyer, "everything")
	got = strings.Join(c.replies(t, buyer), "\n")
	got = meetTheCounter(t, c, buyer, got)
	if !strings.Contains(got, "Halter Pleated Maxi Dress is yours for") {
		t.Fatalf("the halter should settle once the scope is declared, got %q", got)
	}
	m := orderTotalRe.FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("the deal should name the order total, got %q", got)
	}
	orderTotal, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil || orderTotal < 400 || orderTotal > 420 {
		t.Fatalf("the order should total between her 400 and the 420 hand, got %v", orderTotal)
	}

	c.say(t, buyer, "Send it to Koforidua")
	c.last(t, buyer)
	c.say(t, buyer, "Efua Mensima")
	c.last(t, buyer)
	c.say(t, buyer, "no")
	c.last(t, buyer)
	c.say(t, buyer, "yes")
	c.replies(t, buyer)

	all := orders.Default.All()
	if len(all) != 1 {
		t.Fatalf("the picks should complete as one order, got %d", len(all))
	}
	order := &all[0]
	if len(order.Items) != 2 || order.Price != orderTotal {
		t.Fatalf("order = %+v, want two items totalling %v", order, orderTotal)
	}
	for _, it := range order.Items {
		if it.Name == "Double Clothes Rack" && it.Price != 270 {
			t.Fatalf("the settled rack must stand at its agreed 270, got %v", it.Price)
		}
		if it.Name == "Halter Pleated Maxi Dress" && (it.Price < 130 || it.Price > 150) {
			t.Fatalf("the halter should settle inside its own bounds, got %v", it.Price)
		}
	}
}

// An offer above a single item's price is corrected
// aloud and closes at the price, never above it: the
// merchant's price is the ceiling of every negotiation,
// and a buyer who offers 300 for a 270 rack is told the
// price is 270, that she cannot offer above it — and
// then pays 270. Silently settling her at the price
// would read as having accepted her wrong figure.
func TestOverOfferOnOneItemClosesAtThePrice(t *testing.T) {
	dir := setupChat(t, totalHandOwner, totalHandCatalogue)
	_ = dir
	c := &chat{dir: dir}
	buyer := "233550009414"

	c.say(t, buyer, "SHOP-HAND")
	c.replies(t, buyer)
	c.say(t, buyer, "I will give you 300 for the Double Clothes Rack")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "is GH₵270, not GH₵300") || !strings.Contains(got, "cannot offer above the price") {
		t.Fatalf("an over-offer should be corrected aloud at the moment it is made, got %q", got)
	}
	if !strings.Contains(got, "Double Clothes Rack is yours for GH₵270") {
		t.Fatalf("an over-offer should close at the 270 price, got %q", got)
	}
	if strings.Contains(got, "yours for GH₵300") {
		t.Fatalf("the buyer must never be charged above the price, got %q", got)
	}
}
