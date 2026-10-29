package agent

// The understanding step: a message the rules did not match
// is read for what the person means, and the agent acts on
// the meaning with its own verified facts. These tests drive
// the action layer with understandings supplied directly, the
// way the live proof drives it with the model's own words:
// payment trouble answered from the order, an expired link
// re-issued under a fresh key, a drafted answer refused whole
// when it carries an invented figure.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

func seedOpenOrder(t *testing.T, ref, buyer string, attempts int) {
	t.Helper()
	provider := "PAY-UNDER-1"
	orders.Default.RecordOrder(ref, orders.Order{
		From: buyer, Status: "pending", ProductName: "Handmade Beaded Bag",
		Price: 850, DeliveryFee: 25, Total: 875, ProviderRef: &provider,
		MerchantID: MerchantID, LinkAttempts: attempts,
	})
}

func TestUnderstoodParsing(t *testing.T) {
	good := `{"intent":"payment_blocked","item":"","reply":""}`
	if u := parseUnderstood(&good); u == nil || u.Intent != intentPaymentBlocked {
		t.Fatalf("plain JSON = %+v", u)
	}
	wrapped := "Here is my reading: {\"intent\":\"ask\",\"item\":\"\",\"reply\":\"Yes, we deliver.\"} end"
	if u := parseUnderstood(&wrapped); u == nil || u.Intent != intentAsk || u.Reply != "Yes, we deliver." {
		t.Fatalf("wrapped JSON = %+v", u)
	}
	bogus := `{"intent":"teleport","item":"","reply":""}`
	if u := parseUnderstood(&bogus); u != nil {
		t.Fatalf("an unknown intent must not parse, got %+v", u)
	}
	if u := parseUnderstood(nil); u != nil {
		t.Fatal("no model answer must give no understanding")
	}
}

func TestFiguresClean(t *testing.T) {
	if !figuresClean("The bag goes for GH₵850, delivery is GH₵25.", 850, 25) {
		t.Fatal("facts should pass")
	}
	if !figuresClean("No figures here at all.") {
		t.Fatal("a figure-free draft should pass")
	}
	if figuresClean("The bag goes for GH₵500.", 850, 25) {
		t.Fatal("an invented figure must fail the whole draft")
	}
	if figuresClean("Pay GH₵875 in all.", 850, 25) {
		t.Fatal("a total that is not itself a fact must fail: only recorded figures may be spoken")
	}
}

func TestPaymentBlockedIsAnsweredFromTheOrder(t *testing.T) {
	merchant := "233550001801"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550001802"
	seedOpenOrder(t, "TEST-3001", buyer, 1)
	shop := catalogue.GetMerchant(MerchantID)

	if !actOnBuyerUnderstanding(buyer, &Session{Stage: "agreed"}, shop, MerchantID, &understood{Intent: intentPaymentBlocked}) {
		t.Fatal("payment trouble with an open order must be answered")
	}
	buyerGot := strings.Join(c.replies(t, buyer), "\n")
	for _, want := range []string{"Nothing has been charged", "TEST-3001", "still open", "the same link I sent you will work"} {
		if !strings.Contains(buyerGot, want) {
			t.Fatalf("buyer answer missing %q:\n%s", want, buyerGot)
		}
	}
	merchantGot := strings.Join(c.replies(t, merchant), "\n")
	if !strings.Contains(merchantGot, "Payment trouble") || !strings.Contains(merchantGot, "TEST-3001") {
		t.Fatalf("the merchant must hear a buyer is trying to pay, got %q", merchantGot)
	}

	// With no open order the same understanding is not acted
	// on: there is nothing to hold open for her.
	stranger := "233550001899"
	if actOnBuyerUnderstanding(stranger, &Session{}, shop, MerchantID, &understood{Intent: intentPaymentBlocked}) {
		t.Fatal("payment trouble with no open order must fall through")
	}
}

func TestPaymentLaterHoldsTheOrder(t *testing.T) {
	merchant := "233550001811"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550001812"
	seedOpenOrder(t, "TEST-3002", buyer, 1)

	if !actOnBuyerUnderstanding(buyer, &Session{Stage: "agreed"}, catalogue.GetMerchant(MerchantID), MerchantID, &understood{Intent: intentPaymentLater}) {
		t.Fatal("pay later with an open order must be answered")
	}
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "held for you") || !strings.Contains(got, "GH₵875") {
		t.Fatalf("pay later answer = %q", got)
	}
}

func TestExpiredLinkIsReissuedUnderAFreshKey(t *testing.T) {
	merchant := "233550001821"
	shopJSON := strings.Replace(shopWithBag, `"dailyGiveawayAllowance":300`, `"dailyGiveawayAllowance":300,"payoutPhone":"233550001821"`, 1)
	dir := setupChat(t, merchant, shopJSON)
	c := &chat{dir: dir}
	buyer := "233550001822"
	seedOpenOrder(t, "TEST-3003", buyer, 1)
	s := &Session{Stage: "agreed"}

	if !actOnBuyerUnderstanding(buyer, s, catalogue.GetMerchant(MerchantID), MerchantID, &understood{Intent: intentPaymentExpired}) {
		t.Fatal("an expired link must be re-issued")
	}
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "fresh one") || !strings.Contains(got, "pay.example.test/TEST-3003-2") {
		t.Fatalf("re-issue answer = %q", got)
	}
	if s.PaymentURL != "https://pay.example.test/TEST-3003-2" {
		t.Fatalf("the session must carry the fresh link, got %q", s.PaymentURL)
	}
	if o := orders.Default.GetOrder("TEST-3003"); o == nil || o.LinkAttempts != 2 {
		t.Fatalf("the order must record the second attempt, got %+v", o)
	}

	// Two more reports walk to the cap; the third is answered
	// honestly instead of with another link — and never with a
	// number: the button stays the only door.
	actOnBuyerUnderstanding(buyer, s, catalogue.GetMerchant(MerchantID), MerchantID, &understood{Intent: intentPaymentExpired})
	c.replies(t, buyer)
	if !actOnBuyerUnderstanding(buyer, s, catalogue.GetMerchant(MerchantID), MerchantID, &understood{Intent: intentPaymentExpired}) {
		t.Fatal("the capped answer must still answer")
	}
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "a few times now") || !strings.Contains(got, "payment button is the way to complete it") {
		t.Fatalf("capped answer = %q", got)
	}
	if strings.Contains(got, "MoMo directly") {
		t.Fatalf("the capped answer must never offer a direct route: %q", got)
	}
	if o := orders.Default.GetOrder("TEST-3003"); o.LinkAttempts != 3 {
		t.Fatalf("attempts must stop at the cap, got %d", o.LinkAttempts)
	}
}

func TestUnderstoodItemAndAskAndPoisonedDraft(t *testing.T) {
	merchant := "233550001831"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550001832"
	shop := catalogue.GetMerchant(MerchantID)
	s := &Session{Stage: "browsing"}

	if !actOnBuyerUnderstanding(buyer, s, shop, MerchantID, &understood{Intent: intentItem, Item: "Handmade Beaded Bag"}) {
		t.Fatal("an understood item ask must be answered")
	}
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Handmade Beaded Bag") || s.LastViewed == nil {
		t.Fatalf("item answer = %q, LastViewed = %v", got, s.LastViewed)
	}

	if !actOnBuyerUnderstanding(buyer, s, shop, MerchantID, &understood{Intent: intentAsk, Reply: "The bag goes for GH₵850."}) {
		t.Fatal("a clean drafted answer must be sent")
	}
	c.replies(t, buyer)
	if actOnBuyerUnderstanding(buyer, s, shop, MerchantID, &understood{Intent: intentAsk, Reply: "The bag goes for GH₵500."}) {
		t.Fatal("a drafted answer with an invented figure must be refused")
	}
	if got := c.replies(t, buyer); len(got) != 0 {
		t.Fatalf("nothing may be sent for a poisoned draft, got %v", got)
	}

	// Off-topic still walks the lane, now on understanding
	// rather than on keywords.
	if !actOnBuyerUnderstanding(buyer, s, shop, MerchantID, &understood{Intent: intentOffTopic}) {
		t.Fatal("an understood off-topic turn must be steered")
	}
	if s.OffTopic != 1 {
		t.Fatalf("the lane strike must land, strikes = %d", s.OffTopic)
	}
}

func TestMerchantUnderstandingAnswersAndRefuses(t *testing.T) {
	merchant := "233550001841"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	shop := catalogue.GetMerchant(MerchantID)

	if !actOnMerchantUnderstanding(merchant, shop, MerchantID, "", &understood{Intent: intentAsk, Reply: "Your bag is listed at GH₵850."}) {
		t.Fatal("a clean answer to the owner must be sent")
	}
	c.replies(t, merchant)
	if actOnMerchantUnderstanding(merchant, shop, MerchantID, "", &understood{Intent: intentAsk, Reply: "You have made GH₵5,000 today."}) {
		t.Fatal("an invented day figure must be refused for the owner too")
	}
	if got := c.replies(t, merchant); len(got) != 0 {
		t.Fatalf("nothing may be sent for a poisoned draft, got %v", got)
	}
}

func TestPaymentWantServesTheLinkFromTheOrder(t *testing.T) {
	merchant := "233550001851"
	shopJSON := strings.Replace(shopWithBag, `"dailyGiveawayAllowance":300`, `"dailyGiveawayAllowance":300,"payoutPhone":"233550001851"`, 1)
	dir := setupChat(t, merchant, shopJSON)
	c := &chat{dir: dir}
	buyer := "233550001852"
	seedOpenOrder(t, "TEST-3004", buyer, 1)
	shop := catalogue.GetMerchant(MerchantID)

	// The order carries a provider reference, so the link is
	// rebuilt from the book and served even on a cold session.
	if !actOnBuyerUnderstanding(buyer, &Session{Stage: "browsing"}, shop, MerchantID, &understood{Intent: intentPaymentWant}) {
		t.Fatal("wants-to-pay must be answered")
	}
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Here is the payment link for your order TEST-3004") || !strings.Contains(got, "pay.seevplus.com/PAY-UNDER-1") {
		t.Fatalf("wants-to-pay answer = %q", got)
	}

	// An order with nothing to resend gets a fresh link minted
	// instead of a dead end repeated.
	orders.Default.RecordOrder("TEST-3005", orders.Order{
		From: buyer, Status: "pending", ProductName: "Handmade Beaded Bag",
		Price: 850, Total: 850, MerchantID: MerchantID, LinkAttempts: 1,
	})
	if !actOnBuyerUnderstanding(buyer, &Session{Stage: "browsing"}, shop, MerchantID, &understood{Intent: intentPaymentWant}) {
		t.Fatal("wants-to-pay with nothing to send must still be answered")
	}
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "pay.example.test/TEST-3005-2") {
		t.Fatalf("the fresh link answer = %q", got)
	}
}

func TestDirectRouteReportIsNotAClaim(t *testing.T) {
	merchant := "233550001861"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550001862"
	seedOpenOrder(t, "TEST-3006", buyer, 1)

	// She said the money went somewhere other than the payment
	// link. There is no claim any more: Stall cannot see or
	// confirm that payment, opens nothing, and asks no one to
	// confirm anything. She is told so plainly, with the button.
	if !actOnBuyerUnderstanding(buyer, &Session{Stage: "agreed"}, catalogue.GetMerchant(MerchantID), MerchantID, &understood{Intent: intentPaymentClaim, DirectRoute: true}) {
		t.Fatal("an understood payment report must be acted on")
	}
	o := orders.Default.GetOrder("TEST-3006")
	if o == nil || o.Status != "pending" {
		t.Fatalf("the order must stay open, got %+v", o)
	}
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "cannot check it or confirm it") {
		t.Fatalf("the buyer must be told the plain truth, got %q", got)
	}
	if got := c.replies(t, merchant); len(got) != 0 {
		t.Fatalf("no claim may reach the merchant, got %v", got)
	}
}

func TestOrderStatusAndShelfFromThinking(t *testing.T) {
	merchant := "233550001871"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550001872"
	seedOpenOrder(t, "TEST-3007", buyer, 1)
	shop := catalogue.GetMerchant(MerchantID)
	s := &Session{Stage: "browsing"}

	if !actOnBuyerUnderstanding(buyer, s, shop, MerchantID, &understood{Intent: intentOrderStatus}) {
		t.Fatal("an order status ask must be answered")
	}
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Order TEST-3007") || !strings.Contains(got, "not paid yet") {
		t.Fatalf("status answer = %q", got)
	}
	if !actOnBuyerUnderstanding(buyer, s, shop, MerchantID, &understood{Intent: intentShelf}) {
		t.Fatal("a shelf ask must be answered")
	}
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Handmade Beaded Bag") {
		t.Fatalf("shelf answer = %q", got)
	}
}

func TestBuyerIsRecoveredFromHerOpenOrder(t *testing.T) {
	merchant := "233550001881"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550001882"
	seedOpenOrder(t, "TEST-3008", buyer, 1)

	// The agent restarts: every session and binding is gone,
	// but her order is still open in the book.
	sessions = map[string]*Session{}
	bindings = map[string]string{}

	HandleInbound(Inbound{From: buyer, Text: "send me the payment link"})
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "pay.seevplus.com/PAY-UNDER-1") {
		t.Fatalf("a recovered buyer must be served her link from the book, got %q", got)
	}
	if bindings[buyer] != MerchantID {
		t.Fatalf("the buyer must be bound again to her shop, bindings = %v", bindings)
	}
}
