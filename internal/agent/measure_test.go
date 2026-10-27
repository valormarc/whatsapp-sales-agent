package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Sold by measure, end to end through HandleInbound.
// The test shop mirrors Efua's shelf: the
// Ankara is priced per 6 yards and cut by the yard (GH₵75 a yard,
// floor GH₵60 a yard); the shea butter is sold whole only. The
// shop states no delivery terms, so a completed sale finishes at
// the where-are-you-based question, like the onboarding tests.

const measureShop = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"products":[` +
	`{"id":"ankara","name":"Ankara Fabric (6 yards)","description":"Bright wax-print cotton, 6 yards.","listPrice":450,"floorPrice":360,"stock":12,"unit":"yard","baseQty":6},` +
	`{"id":"shea","name":"Raw Shea Butter (500g)","description":"Unrefined shea butter from Tamale, for skin and hair.","listPrice":120,"floorPrice":95,"stock":40}]}}}`

const wholeAnkaraShop = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"products":[` +
	`{"id":"ankara","name":"Ankara Fabric (6 yards)","description":"Bright wax-print cotton, 6 yards.","listPrice":450,"floorPrice":360,"stock":12},` +
	`{"id":"shea","name":"Raw Shea Butter (500g)","description":"Unrefined shea butter from Tamale, for skin and hair.","listPrice":120,"floorPrice":95,"stock":40}]}}}`

func setupMeasureShop(t *testing.T, merchantNumber string) (string, *chat) {
	t.Helper()
	dir := setupChat(t, merchantNumber, measureShop)
	return dir, &chat{dir: dir}
}

func enterMeasureShop(t *testing.T, c *chat, buyer string) {
	t.Helper()
	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
}

// completeArrangedSale answers the no-delivery-terms shop's one
// question and returns everything the buyer was sent.
func completeArrangedSale(t *testing.T, c *chat, buyer string) string {
	t.Helper()
	c.say(t, buyer, "Koforidua")
	return strings.Join(c.replies(t, buyer), "\n")
}

// The parser fix, on its own: a number inside a quantity phrase
// is never an offer, on any path that reads prices.
func TestParseOfferSkipsQuantityNumbers(t *testing.T) {
	if got := parseOffer("ankara 3 yards"); got != nil {
		t.Fatalf("parseOffer(\"ankara 3 yards\") = %v, want no offer", *got)
	}
	if got := parseOffer("200 for 3 yards"); got == nil || *got != 200 {
		t.Fatalf("parseOffer(\"200 for 3 yards\") = %v, want 200", got)
	}
	if got := parseOffer("3 yards for 200"); got == nil || *got != 200 {
		t.Fatalf("parseOffer(\"3 yards for 200\") = %v, want 200", got)
	}
	if got := parseOffer("1.5 yards at 100"); got == nil || *got != 100 {
		t.Fatalf("parseOffer(\"1.5 yards at 100\") = %v, want 100", got)
	}
	if got := parseOffer("i can offer 400"); got == nil || *got != 400 {
		t.Fatalf("parseOffer(\"i can offer 400\") = %v, want 400", got)
	}
}

// The live-run fault, exactly as I hit it: "Ankara 3 yards"
// is a quantity question, not a GH₵3 offer. It is quoted on the
// scaled figures, no haggle opens, nothing is sold, and the
// quantity is remembered so her next plain offer lands on it.
func TestQuantityAloneIsNotAnOffer(t *testing.T) {
	merchant := "233550011001"
	buyer := "233550012001"
	dir, c := setupMeasureShop(t, merchant)
	_ = dir
	enterMeasureShop(t, c, buyer)

	c.say(t, buyer, "Ankara 3 yards")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "3 yards of the Ankara Fabric (6 yards) is GH₵225.") {
		t.Fatalf("quantity quote = %q, want the 3 yards at the scaled GH₵225", got)
	}
	if !strings.Contains(got, "The full 6 yards is GH₵450.") {
		t.Fatalf("quantity quote = %q, want the full piece priced beside it", got)
	}
	if strings.Contains(got, "come down") || strings.Contains(got, "yours for") {
		t.Fatalf("a quantity question opened a haggle or a sale: %q", got)
	}
	if n := ordersCount(t, dir); n != 0 {
		t.Fatalf("orders %d after a quantity question, want 0", n)
	}
	day := daybook.Default.GetDayState(MerchantID, daybook.Options{Allowance: 300})
	if day.Sales != 0 {
		t.Fatalf("day book sales %d after a quantity question, want 0", day.Sales)
	}

	// Her next plain offer lands on the 3 yards she was quoted:
	// every figure stays inside the scaled bounds [180, 225],
	// never up at the whole piece's prices.
	c.say(t, buyer, "200")
	counter := c.last(t, buyer)
	for _, f := range figuresIn(counter) {
		if f < 180 || f > 225 {
			t.Fatalf("counter figure %v outside the scaled bounds [180, 225] in %q", f, counter)
		}
	}
}

// An offer made with its quantity haggles on the scaled list and
// floor, in either word order, and settles at her own price. The
// close records the quantity and unit on the order, the day book
// draws the giveaway from the scaled list, and the merchant's
// notice names what to cut.
func TestOfferWithQuantityHagglesOnScaledFigures(t *testing.T) {
	merchant := "233550011002"

	for i, opening := range []string{"200 for 3 yards", "3 yards for 200"} {
		buyer := "2335500121" + string(rune('0'+i)) + "2"
		dir, c := setupMeasureShop(t, merchant)
		enterMeasureShop(t, c, buyer)

		c.say(t, buyer, opening)
		counter := c.last(t, buyer)
		figures := figuresIn(counter)
		if len(figures) == 0 {
			t.Fatalf("opening %q: counter %q names no figure", opening, counter)
		}
		for _, f := range figures {
			if f < 180 || f > 225 {
				t.Fatalf("opening %q: counter figure %v outside the scaled bounds [180, 225] in %q", opening, f, counter)
			}
		}
		c.say(t, buyer, "200")
		deal := strings.Join(c.replies(t, buyer), "\n")
		if !strings.Contains(deal, "yours for GH₵200") {
			t.Fatalf("opening %q: repeated 200 should settle at her price, got %q", opening, deal)
		}
		if !strings.Contains(deal, "Where are you based?") {
			t.Fatalf("opening %q: the agreed deal should reach the fulfilment question, got %q", opening, deal)
		}
		if got := completeArrangedSale(t, c, buyer); !strings.Contains(got, "Pay here") {
			t.Fatalf("opening %q: her place should complete the sale with the link, got %q", opening, got)
		}

		order := orders.Default.NewestUnpaidFor(MerchantID, buyer)
		if order == nil {
			t.Fatalf("opening %q: no order recorded", opening)
		}
		if order.Price != 200 || order.Quantity != 3 || order.Unit != "yard" {
			t.Fatalf("opening %q: order = price %v, quantity %v, unit %q; want 200, 3, yard", opening, order.Price, order.Quantity, order.Unit)
		}
		if order.ProductName != "Ankara Fabric (6 yards)" {
			t.Fatalf("opening %q: order product = %q", opening, order.ProductName)
		}
		day := daybook.Default.GetDayState(MerchantID, daybook.Options{Allowance: 300})
		if day.Sales != 1 || day.Revenue != 200 || day.GivenAway != 25 {
			t.Fatalf("opening %q: day book = %+v, want 1 sale, GH₵200 in, GH₵25 given away off the scaled list 225", opening, day)
		}
		notes := strings.Join(c.replies(t, merchant), "\n")
		if !strings.Contains(notes, "Sold: 3 yards of Ankara Fabric (6 yards) at GH₵200") {
			t.Fatalf("opening %q: merchant notice should name the cut, got %q", opening, notes)
		}
		_ = dir
	}
}

// The scaled floor is a floor like any other: an offer under it
// for the quantity is ground down to the final stand and let go,
// never settled, however often it is repeated.
func TestScaledFloorNeverCrossed(t *testing.T) {
	merchant := "233550011003"
	buyer := "233550012003"
	dir, c := setupMeasureShop(t, merchant)
	enterMeasureShop(t, c, buyer)

	for i := 0; i < 7; i++ {
		c.say(t, buyer, "150 for 3 yards")
		for _, reply := range c.replies(t, buyer) {
			if strings.Contains(reply, "yours for") || strings.Contains(reply, "Pay here") {
				t.Fatalf("offer %d: a below-floor offer settled: %q", i, reply)
			}
			for _, f := range figuresIn(reply) {
				if f != 150 && f < 180 {
					t.Fatalf("offer %d: figure %v crossed the scaled floor 180 in %q", i, f, reply)
				}
			}
		}
	}
	if n := ordersCount(t, dir); n != 0 {
		t.Fatalf("orders %d, want 0: nothing below the floor was sold", n)
	}
	day := daybook.Default.GetDayState(MerchantID, daybook.Options{Allowance: 300})
	if day.Sales != 0 {
		t.Fatalf("day book sales %d, want 0", day.Sales)
	}
}

// A whole-only product asked for by measure refuses in the
// merchant's real words, opens no haggle, and still sells whole
// to a plain offer afterwards.
func TestWholeOnlyProductRefusesSplit(t *testing.T) {
	merchant := "233550011004"
	buyer := "233550012004"
	dir, c := setupMeasureShop(t, merchant)
	enterMeasureShop(t, c, buyer)

	c.say(t, buyer, "shea butter 2 litres")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "We do not sell the Raw Shea Butter (500g) piece by piece.") {
		t.Fatalf("refusal = %q", got)
	}
	if !strings.Contains(got, "It is GH₵120 as it comes.") {
		t.Fatalf("refusal = %q, want the whole price stated as it comes", got)
	}
	if n := ordersCount(t, dir); n != 0 {
		t.Fatalf("orders %d after a refusal, want 0", n)
	}

	// The plain offer that follows is for the whole item, on the
	// whole item's own bounds.
	c.say(t, buyer, "100")
	counter := c.last(t, buyer)
	figures := figuresIn(counter)
	if len(figures) == 0 {
		t.Fatalf("counter %q names no figure", counter)
	}
	for _, f := range figures {
		if f < 95 || f > 120 {
			t.Fatalf("whole-item counter figure %v outside [95, 120] in %q", f, counter)
		}
	}
}

// Naming the pack as listed ("500g") is naming the goods, not
// asking to split them: the item is quoted as it comes.
func TestPackDescriptorIsNotASplitRequest(t *testing.T) {
	merchant := "233550011005"
	buyer := "233550012005"
	_, c := setupMeasureShop(t, merchant)
	enterMeasureShop(t, c, buyer)

	c.say(t, buyer, "Raw Shea Butter (500g)")
	got := c.last(t, buyer)
	if strings.Contains(got, "piece by piece") {
		t.Fatalf("naming the pack earned a refusal: %q", got)
	}
	if !strings.Contains(got, "It goes for GH₵120") {
		t.Fatalf("pack-named view = %q, want the ordinary quote", got)
	}
}

// The wrong measure for a by-measure product earns one light
// clarifier naming the product's own unit, and no haggle.
func TestWrongUnitGetsOneClarifier(t *testing.T) {
	merchant := "233550011006"
	buyer := "233550012006"
	_, c := setupMeasureShop(t, merchant)
	enterMeasureShop(t, c, buyer)

	c.say(t, buyer, "ankara 2 kilos")
	got := c.last(t, buyer)
	if !strings.Contains(got, "The Ankara Fabric (6 yards) is sold by the yard.") || !strings.Contains(got, "How many yards do you want?") {
		t.Fatalf("wrong-unit reply = %q, want the clarifier naming the yard", got)
	}
	// Her answer, in the shop's own measure, is quoted normally.
	c.say(t, buyer, "3 yards")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "is GH₵225.") {
		t.Fatalf("quote after the clarifier = %q, want the 3 yards at GH₵225", got)
	}
}

// The per-unit question is answered with the unit price and the
// full price, and opens no haggle: a plain offer afterwards is
// still for the whole item.
func TestPerUnitPriceQuestion(t *testing.T) {
	merchant := "233550011007"
	buyer := "233550012007"
	_, c := setupMeasureShop(t, merchant)
	enterMeasureShop(t, c, buyer)

	c.say(t, buyer, "Ankara")
	c.last(t, buyer)
	c.say(t, buyer, "how much per yard?")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "1 yard of the Ankara Fabric (6 yards) is GH₵75.") {
		t.Fatalf("per-unit answer = %q, want GH₵75 a yard", got)
	}
	if !strings.Contains(got, "The full 6 yards is GH₵450.") {
		t.Fatalf("per-unit answer = %q, want the full price beside it", got)
	}
	c.say(t, buyer, "200")
	counter := c.last(t, buyer)
	figures := figuresIn(counter)
	if len(figures) == 0 || figures[len(figures)-1] < 300 {
		t.Fatalf("offer after a per-unit question should haggle the whole piece from GH₵450, got %q", counter)
	}
}

// Naming a different quantity mid-haggle restarts the haggle on
// the new quantity's scaled figures, counters reset, with one
// line acknowledging the new amount.
func TestMidHaggleQuantityChangeRestartsScaled(t *testing.T) {
	merchant := "233550011008"
	buyer := "233550012008"
	_, c := setupMeasureShop(t, merchant)
	enterMeasureShop(t, c, buyer)

	c.say(t, buyer, "How much is the ankara?")
	if got := c.last(t, buyer); !strings.Contains(got, "GH₵450") {
		t.Fatalf("whole quote = %q", got)
	}
	c.say(t, buyer, "400")
	if got := c.last(t, buyer); !strings.Contains(got, "GH₵4") {
		t.Fatalf("whole counter = %q", got)
	}
	c.say(t, buyer, "4 yards")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "For 4 yards then.") {
		t.Fatalf("quantity change = %q, want the acknowledgement line", got)
	}
	if !strings.Contains(got, "4 yards of the Ankara Fabric (6 yards) is GH₵300.") {
		t.Fatalf("quantity change = %q, want the fresh quote at the scaled GH₵300", got)
	}
	// The restarted haggle stands on the 4-yard bounds [240, 300]:
	// 290 beats the first counter the reset engine makes, so it
	// settles at her price.
	c.say(t, buyer, "290")
	deal := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(deal, "yours for GH₵290") {
		t.Fatalf("290 in the restarted haggle should settle, got %q", deal)
	}
	completeArrangedSale(t, c, buyer)
	order := orders.Default.NewestUnpaidFor(MerchantID, buyer)
	if order == nil || order.Quantity != 4 || order.Unit != "yard" || order.Price != 290 {
		t.Fatalf("order = %+v, want 4 yards at GH₵290", order)
	}
	day := daybook.Default.GetDayState(MerchantID, daybook.Options{Allowance: 300})
	if day.GivenAway != 10 {
		t.Fatalf("given away %v, want 10 off the scaled list 300", day.GivenAway)
	}
}

// A fractional quantity scales and rounds by the stated rules:
// 1.5 yards lists at GH₵113 (112.5, half up) over a floor of
// GH₵90 exactly, and the close records the fraction.
func TestFractionalQuantityScalesAndRounds(t *testing.T) {
	merchant := "233550011009"
	buyer := "233550012009"
	_, c := setupMeasureShop(t, merchant)
	enterMeasureShop(t, c, buyer)

	c.say(t, buyer, "1.5 yards of ankara")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "1.5 yards of the Ankara Fabric (6 yards) is GH₵113.") {
		t.Fatalf("fractional quote = %q, want GH₵113 for 1.5 yards", got)
	}
	c.say(t, buyer, "100")
	c.last(t, buyer)
	c.say(t, buyer, "100")
	deal := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(deal, "yours for GH₵100") {
		t.Fatalf("repeated 100 (floor 90) should settle, got %q", deal)
	}
	completeArrangedSale(t, c, buyer)
	order := orders.Default.NewestUnpaidFor(MerchantID, buyer)
	if order == nil || order.Quantity != 1.5 || order.Price != 100 {
		t.Fatalf("order = %+v, want 1.5 yards at GH₵100", order)
	}
	day := daybook.Default.GetDayState(MerchantID, daybook.Options{Allowance: 300})
	if day.GivenAway != 13 {
		t.Fatalf("given away %v, want 13 off the scaled list 113", day.GivenAway)
	}
}

// Onboarding asks the measure question after the floor: answered
// in two steps (the unit, then how much of it the price is for)
// or combined in one, the product is saved with its measure and
// sells by it straight away.
func TestOnboardingByMeasure(t *testing.T) {
	merchant := "233550011010"
	buyer := "233550012010"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	onboard := func(name, description, list, floor string) {
		t.Helper()
		c.say(t, merchant, "add a product")
		c.last(t, merchant)
		c.say(t, merchant, name)
		c.last(t, merchant)
		c.say(t, merchant, description)
		c.last(t, merchant)
		c.say(t, merchant, list)
		c.last(t, merchant)
		c.say(t, merchant, floor)
		if got := c.last(t, merchant); !strings.Contains(got, "by measure") {
			t.Fatalf("after the floor for %s, want the measure question, got %q", name, got)
		}
	}

	onboard("Palm Oil", "Pressed palm oil, red and rich.", "200", "160")
	c.say(t, merchant, "by the litre")
	if got := c.last(t, merchant); !strings.Contains(got, "How much of it is the GH₵200 price for?") {
		t.Fatalf("measure quantity question = %q", got)
	}
	c.say(t, merchant, "5 litres")
	summary := c.last(t, merchant)
	if !strings.Contains(summary, "Sold by the litre") || !strings.Contains(summary, "GH₵40 per litre") {
		t.Fatalf("summary = %q, want the measure in plain words", summary)
	}
	c.say(t, merchant, "yes")
	c.last(t, merchant)
	p := catalogue.FindByName(MerchantID, "Palm Oil")
	if p == nil || p.Unit != "litre" || p.BaseQty != 5 {
		t.Fatalf("onboarded palm oil = %+v, want litre measure with base 5", p)
	}

	// The combined answer settles the measure in one message.
	onboard("Cloth", "Plain woven cloth.", "300", "240")
	c.say(t, merchant, "by the yard, 6 yards")
	if got := c.last(t, merchant); !strings.Contains(got, "Sold by the yard") {
		t.Fatalf("combined measure answer should reach the summary, got %q", got)
	}
	c.say(t, merchant, "yes")
	c.last(t, merchant)
	p = catalogue.FindByName(MerchantID, "Cloth")
	if p == nil || p.Unit != "yard" || p.BaseQty != 6 {
		t.Fatalf("onboarded cloth = %+v, want yard measure with base 6", p)
	}

	// And the palm oil sells by its measure straight away.
	enterMeasureShop(t, c, buyer)
	c.say(t, buyer, "2 litres of palm oil")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "2 litres of the Palm Oil is GH₵80.") {
		t.Fatalf("palm oil quote = %q, want 2 litres at GH₵80", got)
	}
}

// The merchant's own commands give a product a measure and take
// it back to whole, in both phrasings, and her catalogue states
// the measure in plain words while she has one.
func TestMerchantMeasureCommands(t *testing.T) {
	merchant := "233550011011"
	buyer := "233550012011"
	dir := setupChat(t, merchant, wholeAnkaraShop)
	c := &chat{dir: dir}

	c.say(t, merchant, "set measure of ankara to 6 yards")
	if got := c.last(t, merchant); !strings.Contains(got, "sold by the yard") || !strings.Contains(got, "GH₵75 per yard") {
		t.Fatalf("set measure reply = %q", got)
	}
	if p := catalogue.FindByName(MerchantID, "ankara"); p == nil || p.Unit != "yard" || p.BaseQty != 6 {
		t.Fatalf("ankara on disk = %+v, want the yard measure", p)
	}

	c.say(t, merchant, "my catalogue")
	if got := c.last(t, merchant); !strings.Contains(got, "GH₵75 per yard, sold by the yard") {
		t.Fatalf("catalogue listing = %q, want the measure in plain words", got)
	}

	enterMeasureShop(t, c, buyer)
	c.say(t, buyer, "ankara 3 yards")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "is GH₵225.") {
		t.Fatalf("quote under the new measure = %q, want the 3 yards at GH₵225", got)
	}

	c.say(t, merchant, "sell ankara whole only")
	if got := c.last(t, merchant); !strings.Contains(got, "sold whole only") {
		t.Fatalf("whole-only reply = %q", got)
	}
	if p := catalogue.FindByName(MerchantID, "ankara"); p == nil || p.ByMeasure() {
		t.Fatalf("ankara on disk = %+v, want sold whole only", p)
	}
	c.say(t, buyer, "ankara 3 yards")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "piece by piece") || !strings.Contains(got, "GH₵450 as it comes.") {
		t.Fatalf("after whole-only, a split ask should refuse at the whole price, got %q", got)
	}

	// The bare phrasing clears it too.
	c.say(t, merchant, "set measure of ankara to 6 yards")
	c.last(t, merchant)
	c.say(t, merchant, "ankara whole only")
	if got := c.last(t, merchant); !strings.Contains(got, "sold whole only") {
		t.Fatalf("bare whole-only reply = %q", got)
	}
	if p := catalogue.FindByName(MerchantID, "ankara"); p == nil || p.ByMeasure() {
		t.Fatalf("ankara on disk = %+v, want sold whole only after the bare command", p)
	}
}
