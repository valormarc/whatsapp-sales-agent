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
	"math"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

const totalHandCatalogue = `{"merchants":{"demo":{"id":"demo","name":"Hand Shop","shopCode":"HAND","ownerWhatsApp":"233550009301","dailyGiveawayAllowance":300,"deliveryFee":20,"deliveryNote":"Sent by bus to your station","products":[
{"id":"rack","name":"Double Clothes Rack","description":"Black metal double clothes rack.","listPrice":270,"floorPrice":250,"stock":2},
{"id":"ribbed","name":"Ribbed Off-Shoulder Bodycon Maxi Dress","description":"Ribbed off-shoulder dress.","listPrice":130,"floorPrice":120,"stock":2},
{"id":"halter","name":"Halter Pleated Maxi Dress","description":"Green halter-neck pleated maxi dress.","listPrice":150,"floorPrice":130,"stock":2}]}}}`

const totalHandOwner = "233550009301"

// pickThree stands a buyer up in the hand shop and picks
// all three items. Message ids are unique per test — the
// message book is process-wide and a repeated id is
// dropped as a redelivery.
func pickThree(t *testing.T, c *chat, buyer, idPrefix string) {
	t.Helper()
	c.say(t, buyer, "SHOP-HAND")
	c.replies(t, buyer)
	HandleInbound(Inbound{From: buyer, ListID: "shelf_rack", MessageID: "wamid." + idPrefix + "-P1"})
	HandleInbound(Inbound{From: buyer, ListID: "shelf_ribbed", MessageID: "wamid." + idPrefix + "-P2"})
	HandleInbound(Inbound{From: buyer, ListID: "shelf_halter", MessageID: "wamid." + idPrefix + "-P3"})
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
	s := sessions.get(sessionKey(MerchantID, buyer))
	if s == nil || !s.ShelfSettling || s.Product == nil || s.Product.ID != "rack" || len(s.ShelfPicks) != 1 {
		t.Fatalf("the session should be settling the rack with the dress queued, got %+v", s)
	}
}

var orderTotalRe = regexp.MustCompile(`That makes your order GH₵([\d,]+(?:\.\d+)?) in all`)

func TestTotalOfferSettlesTheWholeHand(t *testing.T) {
	dir := setupChat(t, totalHandOwner, totalHandCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009402"
	pickThree(t, c, buyer, "TOTAL")

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
	s := sessions.get(sessionKey(MerchantID, buyer))
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
	s := sessions.get(sessionKey(MerchantID, buyer))
	if s == nil || len(s.Basket) != 1 || s.Basket[0].Product.ID != "bag" || s.Product == nil || s.Product.ID != "dress" {
		t.Fatalf("the bag should be parked with the dress in front of her, got %+v", s)
	}

	c.say(t, buyer, "190")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "How do you want to get it?") {
		t.Fatalf("settling the last pick should open the fulfilment, got %q", got)
	}
}

// Her declared scope is read at any figure: "250 for all"
// is below the dearest pick (the 270 rack), so the bare-
// figure rule alone would send it to one item — but she
// said what the figure is for, and the haggle opens on
// the whole 550 hand.
func TestDeclaredTotalOfferBelowDearestItem(t *testing.T) {
	dir := setupChat(t, totalHandOwner, totalHandCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009405"
	pickThree(t, c, buyer, "DECL")

	c.say(t, buyer, "I will give you 250 for all")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "For all of them together") || !strings.Contains(got, "GH₵550") {
		t.Fatalf("a declared total offer should open the haggle on the whole hand, got %q", got)
	}
	s := sessions.get(sessionKey(MerchantID, buyer))
	if s == nil || len(s.TotalHand) != 3 {
		t.Fatalf("the haggle should be over the three-item hand, got %+v", s)
	}
}

// A pick whose stock has gone since she chose it is not
// in the hand any more: the total is struck over what
// the shop can still supply, and the figures she is shown
// never counted it either.
func TestTotalHaggleSkipsAStockedOutPick(t *testing.T) {
	dir := setupChat(t, totalHandOwner, totalHandCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009406"
	pickThree(t, c, buyer, "STOK")
	c.replies(t, buyer)

	if _, err := catalogue.SetStock(MerchantID, "Ribbed Off-Shoulder Bodycon Maxi Dress", 0); err != nil {
		t.Fatal(err)
	}
	s := sessions.get(sessionKey(MerchantID, buyer))
	if count, total := picksFigure(s, MerchantID); count != 2 || total != 420 {
		t.Fatalf("the picks figure should count only suppliable goods, got %d / %v", count, total)
	}

	c.say(t, buyer, "I will give you 509")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "GH₵420") || strings.Contains(got, "Ribbed") {
		t.Fatalf("the total should be struck over the two suppliable picks only, got %q", got)
	}
	s = sessions.get(sessionKey(MerchantID, buyer))
	if s == nil || len(s.TotalHand) != 0 || len(s.Basket) != 1 || s.Product == nil || s.Product.ID != "halter" {
		t.Fatalf("the two-item hand should be apportioned into the order, got %+v", s)
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
	s := sessions.get(sessionKey(MerchantID, buyer))
	if s == nil || len(s.ShelfPicks) != 2 || s.ShelfSettling {
		t.Fatalf("her picks should be untouched by another item's sale, got %+v", s)
	}
}

// Run-2 regression: pesewa-priced hands. The old float
// shape rounded every share to whole cedis, so the traced
// hand (10.50 over 10.20, twice, total 20.50) came back
// 20.40, and a share could round above its own list
// price. Pesewa arithmetic closes the sum exactly.
func TestApportionTotalPesewaHandsCloseExactly(t *testing.T) {
	hand := []catalogue.Product{
		{ListPrice: 10.50, FloorPrice: 10.20},
		{ListPrice: 10.50, FloorPrice: 10.20},
	}
	prices := apportionTotal(hand, 20.50)
	if len(prices) != 2 || prices[0] != 10.25 || prices[1] != 10.25 {
		t.Fatalf("the pesewa hand must apportion 10.25 + 10.25, got %v", prices)
	}

	rng := rand.New(rand.NewSource(11))
	for trial := 0; trial < 3000; trial++ {
		n := 2 + rng.Intn(4)
		hand := make([]catalogue.Product, n)
		var listSum, floorSum float64
		for i := range hand {
			list := float64(100+rng.Intn(90000)) / 100
			floor := float64(int64(list*100)*int64(50+rng.Intn(45)) / 100) / 100
			hand[i] = catalogue.Product{ListPrice: list, FloorPrice: floor}
			listSum += list
			floorSum += floor
		}
		total := floorSum + rng.Float64()*(listSum-floorSum)
		total = float64(int64(total*100)) / 100
		prices := apportionTotal(hand, total)
		var sum float64
		for i, p := range prices {
			sum += p
			if p < hand[i].FloorPrice-0.001 || p > hand[i].ListPrice+0.001 {
				t.Fatalf("trial %d: share %v outside [%v, %v]", trial, p, hand[i].FloorPrice, hand[i].ListPrice)
			}
		}
		if math.Round(sum*100) != math.Round(total*100) {
			t.Fatalf("trial %d: shares sum to %v, agreed total %v (hand %v)", trial, sum, total, hand)
		}
	}
}
