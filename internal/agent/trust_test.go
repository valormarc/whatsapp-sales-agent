package agent

// The scam protections. The in-hand gate on the sales where
// a phantom item hurts most; a seller's no ending a sale
// before money moves; a dispute freezing an order on Stall's
// side with the rail's limit stated, never a pretend hold;
// a claimed refund counting only on the buyer's word; and
// the clock turning silence into a strike that follows the
// number across the marketplace.

import (
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

const shopOneOff = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"products":[{"id":"kente","name":"Kente Cloth","description":"Handwoven kente.","listPrice":400,"floorPrice":350,"stock":1}]}}}`

const trustOwner = "233550004099"

const shopTrust = `{"merchants":{"efua":{"id":"efua","name":"Efua's Store","shopCode":"EFUA","ownerWhatsApp":"` + trustOwner + `","payoutPhone":"` + trustOwner + `","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":3}]}}}`

// paidOrderFor seeds a paid order directly in the book, the
// state the dispute roads work on.
func paidOrderFor(shopID, ref, buyer string, price float64) {
	orders.Default.RecordOrder(ref, orders.Order{
		From: buyer, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ProductName: "Handmade Beaded Bag", Price: price, Total: price,
		Status: "paid", MerchantID: shopID, PaymentPath: "seev",
	})
}

func buyOneOff(t *testing.T, c *chat, buyer string) {
	t.Helper()
	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Kente Cloth")
	c.last(t, buyer)
	c.say(t, buyer, "400")
	c.replies(t, buyer)
	c.say(t, buyer, "I will pick it up myself")
	c.finishDetails(t, buyer)
}

func TestOneOffSaleWaitsForTheInHandWord(t *testing.T) {
	merchant := "233550004001"
	dir := setupChat(t, merchant, shopOneOff)
	c := &chat{dir: dir}
	buyer := "233550004002"

	buyOneOff(t, c, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "only one the shop has") || !strings.Contains(got, "confirms it is in hand") {
		t.Fatalf("the buyer must be told the sale waits on the in-hand word, got %q", got)
	}
	if strings.Contains(got, "Pay here") {
		t.Fatalf("no payment button may go out before the word, got %q", got)
	}
	if got := strings.Join(c.replies(t, merchant), "\n"); !strings.Contains(got, "Do you have it in hand now?") {
		t.Fatalf("the seller must be asked, got %q", got)
	}
	if n := len(orders.Default.All()); n != 0 {
		t.Fatal("no order may be recorded before the word")
	}

	c.say(t, merchant, "yes")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Pay here") {
		t.Fatalf("her yes must send the payment button, got %q", got)
	}
	if n := len(orders.Default.All()); n != 1 {
		t.Fatal("the order must be recorded after her yes")
	}
}

func TestInHandNoEndsTheSaleBeforeMoney(t *testing.T) {
	merchant := "233550004011"
	dir := setupChat(t, merchant, shopOneOff)
	c := &chat{dir: dir}
	buyer := "233550004012"

	buyOneOff(t, c, buyer)
	c.replies(t, buyer)
	c.replies(t, merchant)

	c.say(t, merchant, "no")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "does not have the Kente Cloth in hand") || !strings.Contains(got, "Nothing has been charged") {
		t.Fatalf("a no must end the sale plainly, got %q", got)
	}
	if n := len(orders.Default.All()); n != 0 {
		t.Fatal("no order may stand after a no")
	}
}

func TestDisputeFreezesAndGoodsCloseIt(t *testing.T) {
	dir := setupChat(t, trustOwner, shopTrust)
	c := &chat{dir: dir}
	buyer := "233550004022"
	paidOrderFor("efua", "EFUA-0001", buyer, 130)

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "This is a scam, she took my money and never sent it")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "opened a dispute on order EFUA-0001") || !strings.Contains(got, "cannot pull back money") {
		t.Fatalf("the dispute must open with the honest position, got %q", got)
	}
	if got := strings.Join(c.replies(t, trustOwner), "\n"); !strings.Contains(got, "Dispute opened") {
		t.Fatalf("the seller must be told at once, got %q", got)
	}
	if o := orders.Default.GetOrder("EFUA-0001"); o == nil || o.DisputeState != "open" {
		t.Fatalf("the order must stand disputed, got %+v", o)
	}

	// The goods arriving closes it clean.
	shop := catalogue.GetMerchant("efua")
	sess := sessions[sessionKey("efua", buyer)]
	if !actOnBuyerUnderstanding(buyer, sess, shop, "efua", &understood{Intent: intentOrderReceived}) {
		t.Fatal("the arrival confirmation should be served")
	}
	if o := orders.Default.GetOrder("EFUA-0001"); o == nil || o.DisputeState != "resolved_received" {
		t.Fatalf("arrival must close the dispute, got %+v", o)
	}
	if strikesOf(trustOwner) != 0 {
		t.Fatal("a resolved dispute leaves no strike")
	}
}

func TestClaimedRefundCountsOnlyOnTheBuyersWord(t *testing.T) {
	dir := setupChat(t, trustOwner, shopTrust)
	c := &chat{dir: dir}
	buyer := "233550004032"
	paidOrderFor("efua", "EFUA-0001", buyer, 130)

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "This is a scam, she never sent my order")
	c.replies(t, buyer)
	c.replies(t, trustOwner)

	c.say(t, trustOwner, "I have refunded order EFUA-0001")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Have you received the refund?") {
		t.Fatalf("the buyer must be asked to confirm the refund, got %q", got)
	}
	c.say(t, buyer, "no")
	c.replies(t, buyer)
	if o := orders.Default.GetOrder("EFUA-0001"); o == nil || o.DisputeState != "struck" {
		t.Fatalf("a denied refund must strike, got %+v", o)
	}
	if strikesOf(trustOwner) != 1 {
		t.Fatalf("the strike must stand on her number, got %d", strikesOf(trustOwner))
	}
}

func TestDisputeClockStrikesAfterThreeDays(t *testing.T) {
	setupChat(t, trustOwner, shopTrust)
	buyer := "233550004042"
	paidOrderFor("efua", "EFUA-0001", buyer, 130)
	orders.Default.Update("EFUA-0001", func(o *orders.Order) {
		o.DisputeState = "open"
		o.DisputeBy = buyer
		o.DisputeAt = time.Now().UTC().Add(-4 * 24 * time.Hour).Format(time.RFC3339)
	})
	CheckDisputes(time.Now().UTC())
	if o := orders.Default.GetOrder("EFUA-0001"); o == nil || o.DisputeState != "struck" {
		t.Fatalf("an unresolved dispute must strike after the limit, got %+v", o)
	}
	if strikesOf(trustOwner) != 1 {
		t.Fatalf("the strike must be recorded, got %d", strikesOf(trustOwner))
	}
}

func TestSellerStandingCountsFromTheBook(t *testing.T) {
	setupChat(t, trustOwner, shopTrust)
	buyer := "233550004052"
	if !sellerIsNew(trustOwner) {
		t.Fatal("a seller with no deliveries stands as new")
	}
	for i := 1; i <= 3; i++ {
		ref := "EFUA-000" + string(rune('0'+i))
		paidOrderFor("efua", ref, buyer, 100)
		orders.Default.Update(ref, func(o *orders.Order) { o.DeliveryState = "received" })
	}
	if sellerIsNew(trustOwner) {
		t.Fatal("three confirmed deliveries end newness")
	}
	if addStrike(trustOwner) != 1 || addStrike(trustOwner) != 2 || addStrike(trustOwner) != 3 {
		t.Fatal("strikes must count up")
	}
	if !flaggedNumber(trustOwner) {
		t.Fatal("three strikes must flag the number")
	}
}
