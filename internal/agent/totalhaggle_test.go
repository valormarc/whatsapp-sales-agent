package agent

// The typed road around the picks, pinned: "done" typed
// is the Done tap; an offer that can only mean the whole
// hand is haggled on the total and shared back across
// the items on agreement; and an agreement on any pick,
// by any road, starts the settling so no pick is ever
// stranded. The fixture is the owner's own phone test:
// rack 270, ribbed dress 130, halter dress 150 — a 550
// hand, offered 509.

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

const totalHandCatalogue = `{"merchants":{"demo":{"id":"demo","name":"Hand Shop","shopCode":"HAND","ownerWhatsApp":"233550009301","dailyGiveawayAllowance":300,"deliveryFee":20,"deliveryNote":"Sent by bus to your station","products":[
{"id":"rack","name":"Double Clothes Rack","description":"Black metal double clothes rack.","listPrice":270,"floorPrice":250,"stock":2},
{"id":"ribbed","name":"Ribbed Off-Shoulder Bodycon Maxi Dress","description":"Ribbed off-shoulder dress.","listPrice":130,"floorPrice":120,"stock":2},
{"id":"halter","name":"Halter Pleated Maxi Dress","description":"Green halter-neck pleated maxi dress.","listPrice":150,"floorPrice":130,"stock":2}]}}}`

const totalHandOwner = "233550009301"

func pickThree(t *testing.T, c *chat, buyer string) {
	t.Helper()
	c.say(t, buyer, "SHOP-HAND")
	c.replies(t, buyer)
	HandleInbound(Inbound{From: buyer, ListID: "shelf_rack", MessageID: "wamid.TOTAL-P1"})
	HandleInbound(Inbound{From: buyer, ListID: "shelf_ribbed", MessageID: "wamid.TOTAL-P2"})
	HandleInbound(Inbound{From: buyer, ListID: "shelf_halter", MessageID: "wamid.TOTAL-P3"})
}

func TestTypedDoneStartsTheSettling(t *testing.T) {
	dir := setupChat(t, totalHandOwner, totalHandCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009401"

	c.say(t, buyer, "SHOP-HAND")
	c.replies(t, buyer)
	HandleInbound(Inbound{From: buyer, ListID: "shelf_rack", MessageID: "wamid.TOTAL-D1"})
	HandleInbound(Inbound{From: buyer, ListID: "shelf_ribbed", MessageID: "wamid.TOTAL-D2"})

	c.say(t, buyer, "Done")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "one by one") || !strings.Contains(got, "Double Clothes Rack") || !strings.Contains(got, "GH₵270") {
		t.Fatalf("a typed Done should start the settling on the first pick, got %q", got)
	}
	s := sessions[sessionKey(MerchantID, buyer)]
	if s == nil || !s.ShelfSettling || s.Product == nil || s.Product.ID != "rack" || len(s.ShelfPicks) != 1 {
		t.Fatalf("the session should be settling the rack with the dress queued, got %+v", s)
	}
}

var orderTotalRe = regexp.MustCompile(`That makes your order GH₵([\d,]+(?:\.\d+)?) in all`)

func TestTotalOfferSettlesTheWholeHand(t *testing.T) {
	dir := setupChat(t, totalHandOwner, totalHandCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009402"
	pickThree(t, c, buyer)

	c.say(t, buyer, "I will give you 509")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "For all of them together") || !strings.Contains(got, "GH₵550") {
		t.Fatalf("the offer should be taken on the whole hand at its 550 total, got %q", got)
	}
	if !strings.Contains(got, "yours for") {
		figs := figuresIn(got)
		if len(figs) == 0 {
			t.Fatalf("the counter should name a figure, got %q", got)
		}
		c.say(t, buyer, moneyFigure(figs[len(figs)-1]))
		got = strings.Join(c.replies(t, buyer), "\n")
		if !strings.Contains(got, "yours for") {
			t.Fatalf("meeting the counter should agree the total, got %q", got)
		}
	}
	m := orderTotalRe.FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("the deal should name the order total, got %q", got)
	}
	agreedTotal, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil || agreedTotal < 500 || agreedTotal > 550 {
		t.Fatalf("the agreed total should sit between the summed floors and the total, got %v", agreedTotal)
	}
	s := sessions[sessionKey(MerchantID, buyer)]
	if s == nil || len(s.TotalHand) != 0 || len(s.ShelfPicks) != 0 || len(s.Basket) != 2 || s.Product == nil || s.Product.ID != "halter" {
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
	if len(order.Items) != 3 || order.Price != agreedTotal {
		t.Fatalf("order = %+v, want three items totalling %v", order, agreedTotal)
	}
	floors := map[string]float64{"Double Clothes Rack": 250, "Ribbed Off-Shoulder Bodycon Maxi Dress": 120, "Halter Pleated Maxi Dress": 130}
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
	if order.ProductName != "Double Clothes Rack, Ribbed Off-Shoulder Bodycon Maxi Dress and Halter Pleated Maxi Dress" {
		t.Fatalf("order name = %q", order.ProductName)
	}
}

func TestAgreedPickStartsTheSettling(t *testing.T) {
	dir := setupChat(t, "233550009201", shelfSettleCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009403"

	c.say(t, buyer, "SHOP-PICK")
	c.replies(t, buyer)
	HandleInbound(Inbound{From: buyer, ListID: "shelf_dress", MessageID: "wamid.TOTAL-S1"})
	HandleInbound(Inbound{From: buyer, ListID: "shelf_bag", MessageID: "wamid.TOTAL-S2"})

	// No Done anywhere: a plain offer at the bag's price,
	// the item she looked at last. The bag is a pick, so
	// its agreement starts the settling and the dress
	// comes up next instead of being stranded.
	c.say(t, buyer, "80")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Now the next one you picked:") || !strings.Contains(got, "Denim Mini Dress") {
		t.Fatalf("agreeing a pick should park it and present the next pick, got %q", got)
	}
	s := sessions[sessionKey(MerchantID, buyer)]
	if s == nil || len(s.Basket) != 1 || s.Basket[0].Product.ID != "bag" || s.Product == nil || s.Product.ID != "dress" {
		t.Fatalf("the bag should be parked with the dress in front of her, got %+v", s)
	}

	c.say(t, buyer, "190")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "How do you want to get it?") {
		t.Fatalf("settling the last pick should open the fulfilment, got %q", got)
	}
}

func TestNamedOfferIsNotReadOnTheHand(t *testing.T) {
	dir := setupChat(t, "233550009201", shelfSettleCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009404"

	c.say(t, buyer, "SHOP-PICK")
	c.replies(t, buyer)
	HandleInbound(Inbound{From: buyer, ListID: "shelf_dress", MessageID: "wamid.TOTAL-N1"})
	HandleInbound(Inbound{From: buyer, ListID: "shelf_bag", MessageID: "wamid.TOTAL-N2"})

	c.say(t, buyer, "60 for the Striped Crop Top")
	got := strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "For all of them together") {
		t.Fatalf("an offer naming an item is that item's, never the hand's, got %q", got)
	}
	if !strings.Contains(got, "How do you want to get it?") {
		t.Fatalf("the named item should settle on its own road, got %q", got)
	}
	s := sessions[sessionKey(MerchantID, buyer)]
	if s == nil || len(s.ShelfPicks) != 2 || s.ShelfSettling {
		t.Fatalf("her picks should be untouched by another item's sale, got %+v", s)
	}
}
