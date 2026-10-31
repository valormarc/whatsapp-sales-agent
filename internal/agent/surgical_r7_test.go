package agent

// The surgical build on run-7's reachable records (the
// audit cycle is closed; these pin the three classes the
// owner directed fixed: stock checked at the point of
// presentation, the missing-report state write, and the
// snapshot-decided flips decided inside the lock).

import (
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// soldOutShop: the beaded bag is gone, the sandals remain.
const soldOutShop = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":0},{"id":"sandals","name":"Leather Sandals","description":"Brown leather sandals.","listPrice":300,"floorPrice":240,"stock":2}]}}}`

func TestSurgicalProductListExcludesSoldOut(t *testing.T) {
	setupChat(t, stockOwner, soldOutShop)
	got := productListText("demo")
	if strings.Contains(got, "Handmade Beaded Bag") {
		t.Fatalf("the priced list must not carry sold-out goods: %q", got)
	}
	if !strings.Contains(got, "Leather Sandals") {
		t.Fatalf("stocked goods stay listed: %q", got)
	}
}

func TestSurgicalEntrySelectionSkipsSoldOut(t *testing.T) {
	setupChat(t, stockOwner, soldOutShop)
	shop := catalogue.GetMerchant("demo")
	if p := entrySelection(shop, "handmade beaded bag 850"); p != nil {
		t.Fatalf("a sold-out product must not be the entry selection, got %+v", p)
	}
	if p := entrySelection(shop, "leather sandals 300"); p == nil {
		t.Fatalf("a stocked product at its price still selects")
	}
}

func TestSurgicalRecommendForNeedSkipsSoldOut(t *testing.T) {
	setupChat(t, stockOwner, soldOutShop)
	if rec := recommendForNeed("demo", "something beaded for the evening"); rec != nil {
		t.Fatalf("a sold-out best match must yield no recommendation, got %+v", rec)
	}
}

func TestSurgicalBrowseOmitsTheSoldOutShop(t *testing.T) {
	setupChat(t, stockOwner, soldOutShop)
	buyer := "233550007001"
	// The demo shop still has the sandals, so browse names
	// the shop with the sandals only.
	if !actOnDiscovery(buyer, "what do you have", &discoveryUnderstanding{Intent: "browse"}) {
		t.Fatalf("browse should answer while anything is stocked")
	}
	setupChat(t, stockOwner, `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag.","listPrice":850,"floorPrice":680,"stock":0}]}}}`)
	if actOnDiscovery(buyer, "what do you have", &discoveryUnderstanding{Intent: "browse"}) {
		t.Fatalf("a shop with nothing stocked is omitted whole, so browse has nothing to say")
	}
}

func TestSurgicalAvailabilityAskOnSoldOutProduct(t *testing.T) {
	dir := setupChat(t, stockOwner, soldOutShop)
	c := &chat{dir: dir}
	buyer := "233550007002"
	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Is the handmade beaded bag available?")
	got := c.joined(t, buyer)
	if strings.Contains(got, "Still available") || strings.Contains(got, "GH₵850") {
		t.Fatalf("a sold-out product must not be answered as available or priced: %q", got)
	}
	if !strings.Contains(got, "sold out") {
		t.Fatalf("the answer names the truth: %q", got)
	}
}

func TestSurgicalAvailabilityAskOnStockedProductUnchanged(t *testing.T) {
	dir := setupChat(t, stockOwner, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550007003"
	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Is the handmade beaded bag available?")
	got := c.joined(t, buyer)
	if !strings.Contains(got, "Still available") {
		t.Fatalf("a stocked product answers exactly as before: %q", got)
	}
}

func TestSurgicalQuantityRoadSoldOut(t *testing.T) {
	dir := setupChat(t, stockOwner, soldOutShop)
	c := &chat{dir: dir}
	buyer := "233550007004"
	merchant := catalogue.GetMerchant("demo")
	s := &Session{Stage: "browsing", MerchantID: "demo"}
	if !inTurn(buyer, func() bool { return handleQuantity(buyer, s, merchant, "3 of the handmade beaded bag", "3 of the handmade beaded bag") }) {
		t.Fatalf("the quantity road should answer")
	}
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "sold out") || strings.Contains(got, "as it comes") {
		t.Fatalf("a sold-out quantity ask gets the truth, not a quote: %q", got)
	}
}

func TestSurgicalMeasureOverAskGetsOnlySoMuchLeft(t *testing.T) {
	onePiece := `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"products":[{"id":"ankara","name":"Ankara Fabric (6 yards)","description":"Bright wax-print cotton, 6 yards.","listPrice":450,"floorPrice":360,"stock":1,"unit":"yard","baseQty":6}]}}}`
	dir := setupChat(t, stockOwner, onePiece)
	c := &chat{dir: dir}
	buyer := "233550007005"
	merchant := catalogue.GetMerchant("demo")
	s := &Session{Stage: "browsing", MerchantID: "demo", LastViewed: catalogue.FindByID("demo", "ankara")}
	if !inTurn(buyer, func() bool { return handleQuantity(buyer, s, merchant, "10 yards of ankara fabric", "10 yards of ankara fabric") }) {
		t.Fatalf("the quantity road should answer")
	}
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "only 6 yards") {
		t.Fatalf("an over-ask gets the only-so-much-left truth: %q", got)
	}
}

func TestSurgicalOfferAddRefusesSoldOut(t *testing.T) {
	dir := setupChat(t, stockOwner, soldOutShop)
	c := &chat{dir: dir}
	buyer := "233550007006"
	merchant := catalogue.GetMerchant("demo")
	s := &Session{Stage: "fulfilment", MerchantID: "demo"}
	candidate := catalogue.FindByName("demo", "Handmade Beaded Bag")
	if candidate == nil {
		t.Fatalf("the resolver still resolves sold-out goods; the guard is at the sink")
	}
	if !inTurn(buyer, func() bool { return offerAdd(buyer, s, merchant, candidate) }) {
		t.Fatalf("offerAdd should answer a sold-out candidate")
	}
	if s.PendingAdd != nil {
		t.Fatalf("no add may be armed on sold-out goods")
	}
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "sold out") {
		t.Fatalf("the refusal names the truth: %q", got)
	}
	if got := namedBasketCandidate(s, "add the handmade beaded bag"); got != nil {
		t.Fatalf("the structural candidate read skips sold-out goods too, got %+v", got)
	}
}

func TestSurgicalBindShownPickSoldOut(t *testing.T) {
	setupChat(t, stockOwner, soldOutShop)
	buyer := "233550007007"
	sessions.put(sessionKey("", buyer), &Session{Stage: "browsing", DiscoveryShown: []ShownProduct{{ShopID: "demo", ProductID: "beaded-bag"}}})
	if !bindShownPick(buyer, ShownProduct{ShopID: "demo", ProductID: "beaded-bag"}) {
		t.Fatalf("the pick is answered")
	}
	if got := bindings.get(buyer); got != "" {
		t.Fatalf("a sold-out shown pick binds nothing, bound to %q", got)
	}
}

func TestSurgicalReorderSoldOutLineIsMissing(t *testing.T) {
	soldOutReorder := `{"merchants":{"efua":{"id":"efua","name":"Efua's Store","shopCode":"EFUA","ownerWhatsApp":"` + reorderOwner + `","payoutPhone":"` + reorderOwner + `","dailyGiveawayAllowance":300,"deliveryFee":25,"deliveryPaidTo":"merchant","products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":0}]}}}`
	setupChat(t, reorderOwner, soldOutReorder)
	c := &chat{seen: map[string]int{}}
	seedReorderPast(reorderBuyer, 850)
	c.say(t, reorderBuyer, "SHOP-EFUA")
	c.replies(t, reorderBuyer)
	c.say(t, reorderBuyer, "Can I buy this again?")
	got := c.joined(t, reorderBuyer)
	if !strings.Contains(got, "cannot be repeated") {
		t.Fatalf("a sold-out line joins the missing lines: %q", got)
	}
}

func TestSurgicalRestoredSessionDropsSoldOutSale(t *testing.T) {
	setupChat(t, stockOwner, soldOutShop)
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	s := &Session{Stage: "haggling", MerchantID: "demo", Product: bag, LastViewed: bag}
	revalidateRestoredSession(s)
	if s.Product != nil {
		t.Fatalf("a restored sale on sold-out goods is put down, got %+v", s.Product)
	}
	if s.LastViewed != nil {
		t.Fatalf("a sold-out LastViewed is cleared at restore")
	}
}

func TestSurgicalShelfTapWhileSettlingOnSoldOut(t *testing.T) {
	setupChat(t, stockOwner, soldOutShop)
	buyer := "233550007008"
	bindings.put(buyer, "demo")
	sessions.put(sessionKey("demo", buyer), &Session{Stage: "browsing", MerchantID: "demo", ShelfSettling: true})
	routeShelfTap(buyer, shelfRowPrefix+"beaded-bag")
	s := sessions.get(sessionKey("demo", buyer))
	if s.LastViewed != nil {
		t.Fatalf("a sold-out tap while settling presents nothing and views nothing, got %+v", s.LastViewed)
	}
}

func TestSurgicalPictureCaseOnSoldOut(t *testing.T) {
	dir := setupChat(t, stockOwner, soldOutShop)
	c := &chat{dir: dir}
	buyer := "233550007009"
	shop := catalogue.GetMerchant("demo")
	if !actOnBuyerUnderstanding(buyer, &Session{Stage: "browsing"}, shop, "demo", &understood{Intent: intentPicture, Item: "Handmade Beaded Bag"}) {
		t.Fatalf("the picture case should answer")
	}
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "sold out") {
		t.Fatalf("a picture ask on sold-out goods gets the truth: %q", got)
	}
	if got := strings.Join(c.replies(t, stockOwner), "\n"); got != "" {
		t.Fatalf("no photo is solicited from the merchant for goods that cannot be bought: %q", got)
	}
}

func TestSurgicalOpenDisputeTerminalIsFinal(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233550007010"
	merchant := catalogue.GetMerchant("demo")
	for i, state := range []string{"resolved_received", "resolved_refund", "struck"} {
		ref := "DEMO-310" + string(rune('1'+i))
		orders.Default.RecordOrder(ref, orders.Order{
			From: buyer, CreatedAt: time.Now().UTC().Format(time.RFC3339),
			ProductName: "Beaded Bag", Price: 700, Status: "paid", MerchantID: "demo",
			DeliveryState: "received", DisputeState: state, DisputeAt: "2026-10-20T00:00:00Z",
		})
		o := orders.Default.GetOrder(ref)
		openDispute(buyer, merchant, o, "the bag never came")
		got := orders.Default.GetOrder(ref)
		if got.DisputeState != state || got.DisputeAt != "2026-10-20T00:00:00Z" {
			t.Fatalf("a terminal dispute (%s) is final: no reopen, no restamp, got %+v", state, got)
		}
	}
}

func TestSurgicalReceiptClosesStandingDisputeOnReceivedOrder(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233550007011"
	merchant := catalogue.GetMerchant("demo")
	orders.Default.RecordOrder("DEMO-3201", orders.Order{
		From: buyer, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ProductName: "Beaded Bag", Price: 700, Status: "paid", MerchantID: "demo",
		DeliveryState: "received", DisputeState: "open", DisputeAt: "2026-10-30T00:00:00Z",
	})
	inTurn(buyer, func() bool {
		return buyerConfirmReceived(buyer, merchant, "demo", "DEMO-3201 has arrived, thank you")
	})
	o := orders.Default.GetOrder("DEMO-3201")
	if o.DisputeState != "resolved_received" {
		t.Fatalf("no dispute stands open on a received order, got %+v", o)
	}
}

func TestSurgicalRefundClaimOnOpenDisputeStillApplies(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233550007012"
	merchant := catalogue.GetMerchant("demo")
	orders.Default.RecordOrder("DEMO-3301", orders.Order{
		From: buyer, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ProductName: "Beaded Bag", Price: 700, Status: "paid", MerchantID: "demo",
		DeliveryState: "sent", DisputeState: "open", DisputeAt: "2026-10-30T00:00:00Z",
	})
	msg := "I have refunded order DEMO-3301"
	if !inTurn(stockOwner, func() bool { return handleMerchantRefundClaim(stockOwner, merchant, msg, strings.ToLower(msg)) }) {
		t.Fatalf("a refund claim on a standing dispute is served")
	}
	if o := orders.Default.GetOrder("DEMO-3301"); o.DisputeState != "refund_claimed" {
		t.Fatalf("the claim flips a still-open dispute, got %+v", o)
	}
}

func TestSurgicalAttentionListsMissingOrder(t *testing.T) {
	setupChat(t, attentionOwner, attentionSeed)
	merchant := catalogue.GetMerchant("efua")
	attentionOrder("EFUA-1007", "paid", func(o *orders.Order) {
		o.DeliveryState = "missing"
		o.SentAt = "2026-10-30T00:00:00Z"
	})
	got := merchantAttentionText(merchant, nil)
	if !strings.Contains(got, "EFUA-1007") || !strings.Contains(got, "the buyer says it has not arrived") {
		t.Fatalf("a paid missing order is standing seller work: %q", got)
	}
	if strings.HasPrefix(got, "Nothing needs you") {
		t.Fatalf("the list must not say nothing is needed while a buyer waits: %q", got)
	}
	if words := orderStateWords(orders.Order{Status: "paid", DeliveryState: "missing"}); words != "paid, reported missing" {
		t.Fatalf("the orders desk labels it missing, got %q", words)
	}
}
