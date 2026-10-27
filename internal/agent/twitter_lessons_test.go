package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/persona"
	"github.com/valormarc/hackathon-agent/internal/wants"
)

// Lessons from my own buying chats
// on X (four threads where I was the buyer). I buy the way real
// people buy: I open with a need and let the seller recommend; I
// ask delivery before price and want one total, never two figures
// to add up; my "Ok", "I'll
// contact you" and "I'll buy" never meant a sale, only payment did;
// and a sourcing request ("alert me when you get it") died because
// no alert ever came. These tests pin the agent to all of it, in
// two shops whose delivery terms differ, so nothing crosses.

// The two Twitter-lesson shops both take their delivery money
// themselves ("merchant"): the lessons these tests pin were
// written when the fee always rode inside the total, and fee-in-
// total is now the merchant's explicit setting, so the fixture
// says so outright. Carrier mode, the default the seeds carry,
// is pinned separately in delivery_money_test.go.
const twitterShopCatalogue = `{"merchants":{` +
	`"efua":{"id":"efua","name":"Efua's Store","shopCode":"EFUA","ownerWhatsApp":"` + ownerEfua + `","payoutPhone":"` + ownerEfua + `","dailyGiveawayAllowance":300,"deliveryFee":25,"deliveryNote":"Sent by bus to your station; you collect it the next day.","deliveryPaidTo":"merchant","products":[` +
	`{"id":"shea","name":"Raw Shea Butter (500g)","description":"Unrefined shea butter from Tamale, for skin and hair; keeps the moisture in.","listPrice":120,"floorPrice":95,"stock":40},` +
	`{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":3}]},` +
	`"ama":{"id":"ama","name":"Ama's Stall","shopCode":"AMA","ownerWhatsApp":"` + ownerAma + `","payoutPhone":"` + ownerAma + `","dailyGiveawayAllowance":200,"deliveryFee":15,"deliveryNote":"Rider brings it to you in town, same day.","deliveryPaidTo":"merchant","products":[` +
	`{"id":"smoked-tilapia","name":"Smoked Tilapia","description":"Smoked tilapia from Lake Volta, well dried.","listPrice":180,"floorPrice":140,"stock":10},` +
	`{"id":"groundnut-paste","name":"Groundnut Paste (1kg)","description":"Stone-ground groundnut paste, nothing added.","listPrice":95,"floorPrice":75,"stock":25}]}` +
	`}}`

func setupTwitterShops(t *testing.T) (string, *chat) {
	t.Helper()
	dir := setupChat(t, "233550009999", twitterShopCatalogue)
	return dir, &chat{dir: dir}
}

func ordersCount(t *testing.T, dir string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "data", "orders.json"))
	if err != nil {
		return 0
	}
	var all map[string]map[string]any
	if err := json.Unmarshal(data, &all); err != nil {
		t.Fatal(err)
	}
	return len(all)
}

func TestNeedRecommendsTheMatchingProductOnHerOwnShelf(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550006001"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	// The dots thread, in the buyer's own shape: a problem, not a
	// product, and a check that the seller understood.
	c.say(t, buyer, "I need something to keep the moisture in my hair. You understand what I mean right?")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "I understand what you mean") {
		t.Fatalf("need reply = %q, want the understanding confirmed first", got)
	}
	if !strings.Contains(got, "Raw Shea Butter") || !strings.Contains(got, "GH₵120") {
		t.Fatalf("need reply = %q, want the shea butter recommended with its price anchored", got)
	}

	// The recommendation is the viewed item: her next plain offer
	// lands on it without her naming it again.
	c.say(t, buyer, "Can you do it 100 please?")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Shea") || !strings.Contains(got, "GH₵") {
		t.Fatalf("offer after a recommendation = %q, want the haggle open on the shea butter", got)
	}
	if strings.Contains(got, "Tell me what you are after") {
		t.Fatalf("offer after a recommendation hit the fallback: %q", got)
	}
}

func TestNeedWithNoMatchOffersHerOwnShelfOnly(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550006002"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "I need something to keep the moisture in my hair")
	got := strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "Shea") {
		t.Fatalf("Ama recommended another shop's product: %q", got)
	}
	if !strings.Contains(got, "Smoked Tilapia") || !strings.Contains(got, "I will alert you") {
		t.Fatalf("no-match need reply = %q, want Ama's shelf offered and the alert promised", got)
	}
	// Her want went into Ama's book, and only Ama's.
	if list := wants.Default.List("ama"); len(list) != 1 || list[0].Buyer != buyer {
		t.Fatalf("ama's wants = %+v", list)
	}
	if list := wants.Default.List("efua"); len(list) != 0 {
		t.Fatalf("the want leaked into Efua's book: %+v", list)
	}
}

func TestDeliveryAskedWhileBrowsingThenCloseChargesOneTotal(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550006003"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Handmade Beaded Bag")
	c.last(t, buyer)
	// The jeweller's order of business: delivery before payment, always.
	c.say(t, buyer, "How do I get it?")
	got := strings.Join(c.replies(t, buyer), "\n")
	for _, want := range []string{"Sent by bus", "GH₵25", "GH₵875"} {
		if !strings.Contains(got, want) {
			t.Fatalf("delivery answer = %q, missing %q (note, fee and one total)", got, want)
		}
	}

	// She pays the list price. The agreement lands in the
	// fulfilment step first: the deal, then how she gets it, the
	// bus terms in the shop's own words. No fee is charged blind.
	c.say(t, buyer, "850")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "yours for GH₵850") || !strings.Contains(got, "How do you want to get it?") || !strings.Contains(got, "Sent by bus") {
		t.Fatalf("agreement at the list price = %q, want the deal and the fulfilment question", got)
	}
	if ordersCount(t, dir) != 0 {
		t.Fatal("an order was recorded before she said how the goods reach her")
	}
	// She chooses delivery, with the place in the same breath;
	// the completion totals it up for her, as one figure.
	c.say(t, buyer, "Send it to Koforidua")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Item GH₵850 + delivery GH₵25 = GH₵875 total") || !strings.Contains(got, "Pay here") {
		t.Fatalf("close with delivery = %q, want the breakdown and the link", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 {
		t.Fatalf("orders = %v, want 1", orders)
	}
	o := orders[0]
	if o["price"] != 850.0 || o["deliveryFee"] != 25.0 || o["total"] != 875.0 {
		t.Fatalf("order = %v, want item 850, delivery 25, total 875 as separate fields", o)
	}
	if o["fulfilment"] != "delivery" || o["location"] != "Koforidua" {
		t.Fatalf("order = %v, want fulfilment delivery to Koforidua", o)
	}
	// The day book books the item alone: delivery is a pass-through
	// and must not inflate the day's revenue or shrink its giveaway.
	day := dayState(t, dir, "efua")
	if day["sales"] != 1.0 || day["revenue"] != 850.0 || day["givenAway"] != 0.0 {
		t.Fatalf("Efua day book = %v, want the item figures only", day)
	}
	notes := strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(notes, "Item GH₵850 + delivery GH₵25 = GH₵875 total") {
		t.Fatalf("merchant notice = %q, want the same breakdown", notes)
	}
	if got := c.replies(t, ownerAma); len(got) != 0 {
		t.Fatalf("Ama was told about Efua's sale: %q", got)
	}
}

func TestTotalQuestionMidHaggleUsesTheStandingPrice(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550006004"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.last(t, buyer)
	c.say(t, buyer, "I can offer 100")
	counter := c.last(t, buyer)
	figures := figuresIn(counter)
	if len(figures) == 0 {
		t.Fatalf("counter = %q", counter)
	}
	standing := figures[len(figures)-1]

	// The bookseller's question, word for word in spirit: one figure.
	c.say(t, buyer, "How much in total?")
	got := c.last(t, buyer)
	answerFigures := figuresIn(got)
	wantTotal := standing + 15
	found := false
	for _, f := range answerFigures {
		if f == wantTotal {
			found = true
		}
	}
	if !strings.Contains(got, "Your total is") || !found {
		t.Fatalf("total answer = %q, want one total of GH₵%v (standing %v plus GH₵15 delivery)", got, wantTotal, standing)
	}

	// Asking cost her nothing: the haggle is exactly where she left
	// it, and meeting the standing price still closes.
	c.say(t, buyer, moneyFigure(standing))
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "yours for") {
		t.Fatalf("meeting the standing price after the total question = %q", got)
	}
}

// moneyFigure renders a whole-cedi figure the way a buyer types it.
func moneyFigure(f float64) string {
	return strconv.FormatInt(int64(f), 10)
}

func TestBareOkSettlesNothingAnywhere(t *testing.T) {
	dir, c := setupTwitterShops(t)

	// While browsing: acknowledged, door held open, no sale.
	buyer := "233550006005"
	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Handmade Beaded Bag")
	c.last(t, buyer)
	c.say(t, buyer, "Ok")
	got := c.last(t, buyer)
	if !strings.Contains(got, "Handmade Beaded Bag") || !strings.Contains(got, "GH₵850") {
		t.Fatalf("browsing ok = %q, want the viewed item held up with its price", got)
	}
	if strings.Contains(got, "yours for") || ordersCount(t, dir) != 0 {
		t.Fatalf("a bare ok closed a sale: %q", got)
	}

	// At the final stand: "Ok" is still not the word. Five low
	// offers take this haggle to its last price.
	buyer2 := "233550006006"
	c.say(t, buyer2, "SHOP-AMA")
	c.replies(t, buyer2)
	c.say(t, buyer2, "How much is the smoked tilapia?")
	c.last(t, buyer2)
	for i := 0; i < 5; i++ {
		c.say(t, buyer2, "100")
	}
	if got := strings.Join(c.replies(t, buyer2), "\n"); !strings.Contains(got, "last price") {
		t.Fatalf("five low offers should reach the final stand, got %q", got)
	}
	c.say(t, buyer2, "Ok")
	got = c.last(t, buyer2)
	if !strings.Contains(got, "My last price stands at") || strings.Contains(got, "yours for") {
		t.Fatalf("ok at the final stand = %q, want the stand held, nothing closed", got)
	}
	if ordersCount(t, dir) != 0 {
		t.Fatal("a bare ok at the final stand recorded an order")
	}
	// The real word still agrees it, at the standing price. The
	// sale then waits on fulfilment, like every agreement; her
	// pickup completes it.
	c.say(t, buyer2, "yes")
	if got := strings.Join(c.replies(t, buyer2), "\n"); !strings.Contains(got, "yours for") {
		t.Fatalf("yes at the final stand = %q, want the deal", got)
	}
	if ordersCount(t, dir) != 0 {
		t.Fatal("the yes at the final stand recorded an order before fulfilment was settled")
	}
	c.say(t, buyer2, "I will pick it up myself")
	if ordersCount(t, dir) != 1 {
		t.Fatal("her pickup at the fulfilment step recorded no order")
	}
}

func TestIllBuyAsksFirstAndYesAgreesThenFulfils(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550006007"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Handmade Beaded Bag")
	c.last(t, buyer)
	c.say(t, buyer, "I'll buy it")
	got := strings.Join(c.replies(t, buyer), "\n")
	// The quote names the price and the delivery fee for what it
	// is, a charge that only applies if the goods are sent. No
	// final total is promised before she says how she gets it.
	if !strings.Contains(got, "GH₵850") || !strings.Contains(got, "plus GH₵25 delivery if you want it sent to you") || !strings.Contains(got, "Shall I send the payment link?") {
		t.Fatalf("I'll buy reply = %q, want the price, the conditional delivery fee and the plain question", got)
	}
	if ordersCount(t, dir) != 0 {
		t.Fatal("\"I'll buy\" alone recorded an order; only payment is an order")
	}
	// Her yes agrees the price and lands in the fulfilment step,
	// not at the link: how the goods reach her comes first.
	c.say(t, buyer, "yes")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "yours for GH₵850") || !strings.Contains(got, "How do you want to get it?") {
		t.Fatalf("yes at the confirmation = %q, want the deal and the fulfilment question", got)
	}
	if ordersCount(t, dir) != 0 {
		t.Fatal("the confirmation yes recorded an order before fulfilment was settled")
	}
	c.say(t, buyer, "deliver it")
	if got := c.last(t, buyer); !strings.Contains(got, "Where should it go?") {
		t.Fatalf("delivery with no place known = %q, want the one follow-up", got)
	}
	c.say(t, buyer, "Koforidua")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "= GH₵875 total") || !strings.Contains(got, "Pay here") {
		t.Fatalf("close after fulfilment = %q, want the one total and the link", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["total"] != 875.0 || orders[0]["price"] != 850.0 {
		t.Fatalf("orders = %v, want one order at item 850, total 875", orders)
	}
	if orders[0]["fulfilment"] != "delivery" || orders[0]["location"] != "Koforidua" {
		t.Fatalf("order = %v, want fulfilment delivery to Koforidua", orders[0])
	}
}

func TestIllBuyNoClosesNothingAndTheItemStaysInView(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550006008"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "Tilapia smoked")
	c.last(t, buyer)
	c.say(t, buyer, "I'll take it")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "GH₵180") || !strings.Contains(got, "plus GH₵15 delivery if you want it sent to you") {
		t.Fatalf("I'll take it reply = %q, want the price with Ama's GH₵15 delivery named as conditional", got)
	}
	c.say(t, buyer, "no")
	if got := c.last(t, buyer); !strings.Contains(got, "whenever you are ready") {
		t.Fatalf("no at the confirmation = %q", got)
	}
	if ordersCount(t, dir) != 0 {
		t.Fatal("a no at the confirmation recorded an order")
	}
	// She is still standing in front of the tilapia: a plain offer
	// lands on it without her naming it again.
	c.say(t, buyer, "150")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "GH₵") || strings.Contains(got, "Which item") {
		t.Fatalf("offer after the cancelled confirmation = %q", got)
	}
}

func TestWantsAreRecordedAndOnlyHerOwnerSeesThem(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyerA := "233550006009"
	buyerE := "233550006010"

	c.say(t, buyerA, "SHOP-AMA")
	c.replies(t, buyerA)
	c.say(t, buyerA, "I need a tv")
	c.replies(t, buyerA)
	c.say(t, buyerE, "SHOP-EFUA")
	c.replies(t, buyerE)
	c.say(t, buyerE, "I need lace fabric")
	c.replies(t, buyerE)

	c.say(t, ownerAma, "wants")
	got := strings.Join(c.replies(t, ownerAma), "\n")
	if !strings.Contains(got, "I need a tv") || !strings.Contains(got, buyerA) {
		t.Fatalf("Ama's wants = %q", got)
	}
	if strings.Contains(got, "lace") {
		t.Fatalf("Ama saw Efua's want: %q", got)
	}
	c.say(t, ownerEfua, "buyer wants")
	got = strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(got, "I need lace fabric") || strings.Contains(got, "I need a tv") {
		t.Fatalf("Efua's wants = %q", got)
	}
}

func onboardForTest(t *testing.T, c *chat, owner, name, description, list, floor string) {
	t.Helper()
	c.say(t, owner, "add a product")
	c.last(t, owner)
	c.say(t, owner, name)
	c.last(t, owner)
	c.say(t, owner, description)
	c.last(t, owner)
	c.say(t, owner, list)
	c.last(t, owner)
	c.say(t, owner, floor)
	c.last(t, owner)
	// The measure question comes before the summary; these
	// products are sold whole.
	c.say(t, owner, "only as a whole")
	c.last(t, owner)
	c.say(t, owner, "yes")
	if got := c.last(t, owner); !strings.Contains(got, "Saved") {
		t.Fatalf("onboarding %s = %q, want it saved", name, got)
	}
}

func TestAddingTheWantedProductAlertsTheWaitingBuyerOnce(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550006011"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "I need a tv")
	c.replies(t, buyer)

	// The sourcing close the bookseller never got: the day it lands, the
	// buyer hears, in that moment.
	onboardForTest(t, c, ownerAma, "TV", "32 inch digital television.", "1050", "900")
	got := strings.Join(c.replies(t, buyer), "\n")
	if strings.Count(got, "Good news") != 1 || !strings.Contains(got, "TV is now at Ama's Stall, GH₵1050. Reply to buy.") {
		t.Fatalf("want alert = %q, want exactly one alert with the product, shop and price", got)
	}

	// A second matching arrival alerts nobody again: the want was
	// marked when the first alert went out.
	onboardForTest(t, c, ownerAma, "TV Stand", "Stand for the television.", "150", "100")
	if got := c.replies(t, buyer); len(got) != 0 {
		t.Fatalf("buyer alerted twice for one want: %q", got)
	}
}

func TestAddingAnUnrelatedProductAlertsNobody(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550006012"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "I need a tv")
	c.replies(t, buyer)

	onboardForTest(t, c, ownerAma, "Rice Cooker", "Electric rice cooker, 2 litres.", "300", "250")
	if got := c.replies(t, buyer); len(got) != 0 {
		t.Fatalf("an unrelated product alerted the waiting buyer: %q", got)
	}
}

var lastPriceFigure = regexp.MustCompile(`last price .*GH₵(\d+(?:\.\d+)?)`)

func TestFinalStandCarriesTheDeliveryTotal(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550006013"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.last(t, buyer)
	var stand string
	for i := 0; i < 5; i++ {
		c.say(t, buyer, "100")
		stand = strings.Join(c.replies(t, buyer), "\n")
	}
	m := lastPriceFigure.FindStringSubmatch(stand)
	if m == nil {
		t.Fatalf("no final stand reached: %q", stand)
	}
	// The next step rides with the stand: the total with delivery.
	if !strings.Contains(stand, "With delivery, the total is GH₵") || !strings.Contains(stand, "payment link") {
		t.Fatalf("final stand = %q, want the delivery total and the link offered as the next step", stand)
	}
}

func TestMerchantSetsDeliveryFeeAndNote(t *testing.T) {
	_, c := setupTwitterShops(t)

	c.say(t, ownerAma, "set delivery fee 20")
	if got := c.last(t, ownerAma); !strings.Contains(got, "GH₵20") || !strings.Contains(got, "one total") {
		t.Fatalf("set delivery fee reply = %q", got)
	}
	if got := catalogue.GetMerchant("ama").DeliveryFee; got != 20 {
		t.Fatalf("ama delivery fee on disk = %v, want 20", got)
	}
	if got := catalogue.GetMerchant("efua").DeliveryFee; got != 25 {
		t.Fatalf("Efua's fee moved to %v under Ama's command", got)
	}

	c.say(t, ownerAma, "set delivery note By rider, same day in town")
	if got := c.last(t, ownerAma); !strings.Contains(got, "By rider, same day in town") {
		t.Fatalf("set delivery note reply = %q, want her words confirmed back exactly", got)
	}
	if got := catalogue.GetMerchant("ama").DeliveryNote; got != "By rider, same day in town" {
		t.Fatalf("ama delivery note on disk = %q", got)
	}

	c.say(t, ownerAma, "my catalogue")
	got := c.last(t, ownerAma)
	if !strings.Contains(got, "GH₵20") || !strings.Contains(got, "By rider, same day in town") {
		t.Fatalf("my catalogue = %q, want the delivery fee and note shown to her", got)
	}
}

func TestNoDeliveryFeeMeansThePlainCloseAsBefore(t *testing.T) {
	dir, c := setupTwoShops(t)
	buyer := "233550006014"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "Tilapia smoked")
	c.last(t, buyer)
	c.say(t, buyer, "How much in total?")
	if got := c.last(t, buyer); !strings.Contains(got, "GH₵180") || !strings.Contains(got, "arranged with the seller") {
		t.Fatalf("total with no fee set = %q, want the item price and the plain arrangement line", got)
	}
	c.say(t, buyer, "I'll buy")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Shall I send the payment link?") || strings.Contains(got, "in total") {
		t.Fatalf("confirmation with no fee set = %q, want the plain price and the question", got)
	}
	// Her yes agrees the price. A shop with no delivery terms
	// asks the one question it needs (where she is based) and
	// completes at the plain item price when she answers.
	c.say(t, buyer, "yes")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "yours for GH₵180") || !strings.Contains(got, "Where are you based?") || strings.Contains(got, "delivery GH₵") {
		t.Fatalf("agreement with no terms set = %q, want the deal and the where question, no delivery figures", got)
	}
	if ordersCount(t, dir) != 0 {
		t.Fatal("an order was recorded before she said where she is")
	}
	c.say(t, buyer, "Koforidua")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Pay here") || !strings.Contains(got, "GH₵180") || strings.Contains(got, "delivery GH₵") {
		t.Fatalf("close with no fee set = %q, want the plain close at the item price", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["price"] != 180.0 || orders[0]["total"] != 180.0 {
		t.Fatalf("orders = %v, want item and total both 180", orders)
	}
	if _, present := orders[0]["deliveryFee"]; present {
		t.Fatalf("order carries a delivery fee that was never charged: %v", orders[0])
	}
	if orders[0]["fulfilment"] != "arranged" || orders[0]["location"] != "Koforidua" {
		t.Fatalf("order = %v, want fulfilment arranged with her location recorded", orders[0])
	}
}

func TestSellerVoiceInPromptAndRotatingReplies(t *testing.T) {
	// The persona carries the voice: a market seller, short
	// messages, the buyer's length matched, no script.
	prompt := persona.BuildSystemPrompt(persona.Context{Merchant: catalogue.Merchant{Name: "Efua's Store"}})
	for _, want := range []string{"real Ghanaian market seller", "one or two short sentences", "never a paragraph"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("seller prompt missing voice guidance %q", want)
		}
	}

	// The deterministic holding reply rotates through its variants
	// while the standing figure never moves.
	_, c := setupTwitterShops(t)
	buyer := "233550006015"
	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.last(t, buyer)
	c.say(t, buyer, "I can offer 100")
	c.last(t, buyer)
	c.say(t, buyer, "any type")
	first := c.last(t, buyer)
	c.say(t, buyer, "any type")
	second := c.last(t, buyer)
	if first == second {
		t.Fatalf("holding reply repeated word for word: %q", first)
	}
	f1, f2 := figuresIn(first), figuresIn(second)
	if len(f1) == 0 || len(f2) == 0 || f1[len(f1)-1] != f2[len(f2)-1] {
		t.Fatalf("rotated holding replies moved the price: %q then %q", first, second)
	}
}

func TestRestatedPriceConfirmsThePendingClose(t *testing.T) {
	// The confirmation question is open and the buyer answers
	// in her own words, by naming the figure. A first answer
	// naming the delivery fee is not the price and only earns
	// the plain re-ask; the restated price after it confirms,
	// it does not get the sale let go.
	dir, c := setupTwitterShops(t)
	buyer := "233550006021"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "Tilapia smoked")
	c.last(t, buyer)
	c.say(t, buyer, "I'll take it")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "GH₵180") || !strings.Contains(got, "Shall I send the payment link?") {
		t.Fatalf("confirmation quote = %q, want the price and the question", got)
	}
	c.say(t, buyer, "I will pay 15")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Say yes or no") || strings.Contains(got, "yours for") {
		t.Fatalf("fee figure at the confirmation = %q, want the plain re-ask and no deal", got)
	}
	c.say(t, buyer, "I will pay 180")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "yours for GH₵180") || !strings.Contains(got, "How do you want to get it?") {
		t.Fatalf("restated price at the confirmation = %q, want the deal and the fulfilment question", got)
	}
	if ordersCount(t, dir) != 0 {
		t.Fatal("the restated price recorded an order before fulfilment was settled")
	}
	c.say(t, buyer, "I will come and pick it up myself")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Pay here") || !strings.Contains(got, "GH₵180") {
		t.Fatalf("close after the restated price = %q, want the link at 180", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["price"] != 180.0 || orders[0]["total"] != 180.0 {
		t.Fatalf("orders = %v, want one order at 180", orders)
	}
}

func TestRestatedTotalConfirmsWhenMerchantTakesDeliveryMoney(t *testing.T) {
	// Where the merchant takes the delivery money, the quote
	// names the price and the fee side by side, and a buyer
	// who answers with the two added together has confirmed:
	// the fulfilment step then settles how the goods reach
	// her, at the quoted price.
	_, c := setupTwitterShops(t)
	buyer := "233550006022"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Handmade Beaded Bag")
	c.last(t, buyer)
	c.say(t, buyer, "I'll buy it")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "GH₵850") || !strings.Contains(got, "Shall I send the payment link?") {
		t.Fatalf("confirmation quote = %q, want the price and the question", got)
	}
	c.say(t, buyer, "I will pay 875")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "yours for GH₵850") || !strings.Contains(got, "How do you want to get it?") {
		t.Fatalf("restated total at the confirmation = %q, want the deal at the quoted price and the fulfilment question", got)
	}
}

func TestDifferentFigureIsNotAConfirmation(t *testing.T) {
	// Only the quoted figure confirms. A different figure is
	// an unclear answer like any other: the question is
	// re-asked once, plainly, and a no after it still leaves
	// the item on the shelf with nothing recorded.
	dir, c := setupTwitterShops(t)
	buyer := "233550006023"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "Tilapia smoked")
	c.last(t, buyer)
	c.say(t, buyer, "I'll take it")
	c.replies(t, buyer)
	c.say(t, buyer, "I will pay 150")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Say yes or no") || strings.Contains(got, "yours for") {
		t.Fatalf("different figure at the confirmation = %q, want the plain re-ask and no deal", got)
	}
	c.say(t, buyer, "no")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "here whenever you are ready") {
		t.Fatalf("no after the re-ask = %q, want the item left on the shelf", got)
	}
	if ordersCount(t, dir) != 0 {
		t.Fatal("an unconfirmed close recorded an order")
	}
}
