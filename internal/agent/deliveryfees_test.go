package agent

// The delivery fee system. A "set" shop's place fees: her
// standard fee everywhere, her own fee where she named the
// area, resolved from the buyer's stated place and locked
// into the order. A "quote" shop's per-order fees: the sale
// pauses, the seller is asked in her own chat, her figure
// carries the sale forward inside the buyer's total, her
// refusal hands the buyer the choice again, and her silence
// past the timeout offers the buyer the other ways. The
// commands that set all of it are pinned too.

import (
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

const shopQuoteStyle = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"deliveryStyle":"quote","deliveryPaidTo":"merchant","products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":3}]}}}`

const shopAreaFees = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"deliveryFee":25,"deliveryAreas":[{"area":"Osu","fee":30},{"area":"East Legon","fee":50}],"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":3}]}}}`

func TestDeliveryFeeForAreas(t *testing.T) {
	m := &catalogue.Merchant{DeliveryFee: 25, DeliveryAreas: []catalogue.AreaFee{{Area: "Osu", Fee: 30}, {Area: "East Legon", Fee: 50}, {Area: "Legon", Fee: 40}}}
	if got := deliveryFeeFor(m, "Osu"); got != 30 {
		t.Fatalf("Osu fee = %v, want 30", got)
	}
	if got := deliveryFeeFor(m, "east legon, near the mall"); got != 50 {
		t.Fatalf("East Legon fee = %v, want 50 (the longest match beats Legon)", got)
	}
	if got := deliveryFeeFor(m, "Kasoa"); got != 25 {
		t.Fatalf("unlisted place fee = %v, want the standard 25", got)
	}
}

func TestParseAreaFees(t *testing.T) {
	got := parseAreaFees("Osu 30, Kasoa 45, East Legon 50")
	if len(got) != 3 || got[0].Area != "Osu" || got[0].Fee != 30 || got[2].Area != "East Legon" || got[2].Fee != 50 {
		t.Fatalf("parsed = %+v", got)
	}
	if got := parseAreaFees("no figures here"); len(got) != 0 {
		t.Fatalf("a part with no figure must be skipped, got %+v", got)
	}
}

func TestAreaFeeLandsOnTheOrder(t *testing.T) {
	merchant := "233550003001"
	dir := setupChat(t, merchant, shopAreaFees)
	c := &chat{dir: dir}
	buyer := "233550003002"

	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Handmade Beaded Bag")
	c.last(t, buyer)
	c.say(t, buyer, "850")
	c.replies(t, buyer)
	c.say(t, buyer, "Delivery to Osu")
	c.finishDetails(t, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "GH₵880") {
		t.Fatalf("the buyer must see the area fee inside her total (850 + 30), got %q", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["deliveryFee"] != 30.0 {
		t.Fatalf("orders = %v, want the Osu area fee of 30 on the order", orders)
	}
}

func TestQuoteStylePausesAsksAndCompletes(t *testing.T) {
	merchant := "233550003011"
	dir := setupChat(t, merchant, shopQuoteStyle)
	c := &chat{dir: dir}
	buyer := "233550003012"

	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Handmade Beaded Bag")
	c.last(t, buyer)
	c.say(t, buyer, "850")
	c.replies(t, buyer)
	c.say(t, buyer, "Delivery to Osu")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "I have asked her") {
		t.Fatalf("the buyer must be told the fee is being asked, got %q", got)
	}
	if got := strings.Join(c.replies(t, merchant), "\n"); !strings.Contains(got, "Delivery fee needed") || !strings.Contains(got, "Osu") {
		t.Fatalf("the seller must be asked for the fee, got %q", got)
	}
	if s := sessions.get(sessionKey("demo", buyer)); s == nil || s.FulfilStep != "feewait" {
		t.Fatalf("the buyer must be waiting on the fee, got %+v", s)
	}

	// Her figure carries the sale forward: the buyer hears
	// the fee, then the package details, and the fee is
	// inside the total she confirms (this shop takes the
	// delivery money herself).
	c.say(t, merchant, "fee 30")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "says delivery to Osu is GH₵30") {
		t.Fatalf("the buyer must hear the quoted fee, got %q", got)
	}
	c.finishDetails(t, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "GH₵880") {
		t.Fatalf("the quoted fee must be inside the total she pays, got %q", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["deliveryFee"] != 30.0 || orders[0]["total"] != 880.0 {
		t.Fatalf("orders = %v, want fee 30 and total 880", orders)
	}
}

func TestQuoteDeclineHandsTheChoiceBack(t *testing.T) {
	merchant := "233550003021"
	dir := setupChat(t, merchant, shopQuoteStyle)
	c := &chat{dir: dir}
	buyer := "233550003022"

	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Handmade Beaded Bag")
	c.last(t, buyer)
	c.say(t, buyer, "850")
	c.replies(t, buyer)
	c.say(t, buyer, "Delivery to Osu")
	c.replies(t, buyer)
	c.replies(t, merchant)

	c.say(t, merchant, "no delivery")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "cannot deliver to Osu") {
		t.Fatalf("the buyer must be told plainly, got %q", got)
	}
	if s := sessions.get(sessionKey("demo", buyer)); s == nil || s.FulfilStep != "choice" {
		t.Fatalf("the buyer must be back at the choice, got %+v", s)
	}
}

func TestQuoteTimeoutOffersTheWaysOut(t *testing.T) {
	merchant := "233550003031"
	dir := setupChat(t, merchant, shopQuoteStyle)
	c := &chat{dir: dir}
	buyer := "233550003032"

	bindings.put(buyer, "demo")
	sessions.put(sessionKey("demo", buyer), &Session{
		Stage: "fulfilment", MerchantID: "demo", FulfilStep: "feewait",
		FeeQuotePlace: "Osu", AgreedPrice: 850,
		Product: &catalogue.Product{ID: "beaded-bag", Name: "Handmade Beaded Bag", ListPrice: 850, FloorPrice: 680},
	})
	openFeeQuote(feeQuote{ShopID: "demo", Buyer: buyer, Item: "Handmade Beaded Bag", Price: 850, Place: "Osu"})
	// Age the request past the timeout.
	all := loadFeeQuotes()
	for i := range all {
		all[i].CreatedAt = time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	}
	saveFeeQuotes(all)

	CheckFeeQuotes(time.Now().UTC())
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "has not given the delivery fee") || !strings.Contains(got, "say wait") {
		t.Fatalf("the timeout must offer the ways out, got %q", got)
	}
	if s := sessions.get(sessionKey("demo", buyer)); s == nil || s.FulfilStep != "feeexpired" {
		t.Fatalf("the step must be feeexpired, got %+v", s)
	}

	// She chooses to wait: the seller is asked again.
	c.say(t, buyer, "wait")
	if got := strings.Join(c.replies(t, merchant), "\n"); !strings.Contains(got, "Delivery fee needed") {
		t.Fatalf("waiting must re-ask the seller, got %q", got)
	}
	if q := oldestOpenFeeQuote("demo"); q == nil {
		t.Fatal("a fresh open quote must exist after she chooses to wait")
	}
}

func TestDeliveryStyleAndAreaCommands(t *testing.T) {
	merchant := "233550003041"
	dir := setupChat(t, merchant, shopWithBag)
	// The merchant speaks in her own chat.
	mc := &chat{dir: dir}
	mc.say(t, merchant, "set area fees Osu 30, Kasoa 45")
	if got := strings.Join(mc.replies(t, merchant), "\n"); !strings.Contains(got, "Osu GH₵30") || !strings.Contains(got, "Kasoa GH₵45") {
		t.Fatalf("area fee reply = %q", got)
	}
	m := catalogue.GetMerchant("demo")
	if m == nil || len(m.DeliveryAreas) != 2 {
		t.Fatalf("areas on the merchant = %+v", m)
	}
	mc.say(t, merchant, "set delivery per order")
	if got := strings.Join(mc.replies(t, merchant), "\n"); !strings.Contains(got, "whenever a buyer chooses delivery") {
		t.Fatalf("quote style reply = %q", got)
	}
	if got := deliveryStyleOf(catalogue.GetMerchant("demo")); got != "quote" {
		t.Fatalf("style = %q, want quote", got)
	}
	mc.say(t, merchant, "my catalogue")
	if got := strings.Join(mc.replies(t, merchant), "\n"); !strings.Contains(got, "you quote the fee for each order") {
		t.Fatalf("catalogue must show the quote style, got %q", got)
	}
}
