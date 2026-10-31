package agent

import (
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Regression for the audit finding that a yes/no tap was
// executed against whichever confirmation a dispatcher
// reached first, not the question the buttons were rendered
// for. A buyer's dispute-confirm tap closed her dispute as
// refund-received while the fulfilment question it belonged
// to never moved; the merchant mirror deleted a product with
// a tap meant for an in-hand confirmation.
func TestBuyerYesNoTapAnswersOnlyItsOwnQuestion(t *testing.T) {
	setupChat(t, trustOwner, shopTrust)
	buyer := "233557777777"
	ref := "EFUA-0001-TST"
	paidOrderFor("efua", ref, buyer, 850)
	orders.Default.Update(ref, func(o *orders.Order) { o.DisputeState = "refund_claimed" })

	bindings.put(buyer, "efua")
	s := &Session{Stage: "browsing", MerchantID: "efua"}
	sessions.put(sessionKey("efua", buyer), s)
	s.DisputeConfirmRef = ref
	askYesNo(buyer, s, "dispute", ref, "Efua's Store says she has refunded GH₵850 for order "+ref+". Have you received the refund?", "Yes, received", "Not yet")
	disputeTap := "ynq_" + s.YesNoQ.ID + "_y"

	// A second question is armed in her chat before she
	// answers the first: the payment-link confirm for a new
	// purchase. Its buttons are now the ones in front of her.
	bag := catalogue.FindByName("efua", "Handmade Beaded Bag")
	if bag == nil {
		t.Fatal("fixture product missing")
	}
	s.Product = bag
	s.PendingClose = true
	s.ClosePrice = 850
	askYesNo(buyer, s, "close", bag.Name, "Shall I send the payment link?", "Yes, send it", "Not yet")

	// The stale dispute tap executes nothing.
	HandleInbound(Inbound{From: buyer, ButtonID: disputeTap})
	if got := orders.Default.GetOrder(ref); got == nil || got.DisputeState != "refund_claimed" {
		t.Fatalf("stale dispute tap changed the dispute: %+v", got)
	}
	if !s.PendingClose {
		t.Fatal("stale dispute tap consumed the close question")
	}

	// The current question's own tap answers it, and the
	// dispute stays exactly where it was.
	HandleInbound(Inbound{From: buyer, ButtonID: "ynq_" + s.YesNoQ.ID + "_y"})
	if s.PendingClose {
		t.Fatal("the close question's own tap did not answer it")
	}
	if got := orders.Default.GetOrder(ref); got == nil || got.DisputeState != "refund_claimed" {
		t.Fatalf("the close tap closed the dispute: %+v", got)
	}

	// Re-asked, the dispute's own tap closes it on her word.
	askYesNo(buyer, s, "dispute", ref, "Have you received the refund?", "Yes, received", "Not yet")
	HandleInbound(Inbound{From: buyer, ButtonID: "ynq_" + s.YesNoQ.ID + "_y"})
	if got := orders.Default.GetOrder(ref); got == nil || got.DisputeState != "resolved_refund" {
		t.Fatalf("the dispute tap did not close the dispute: %+v", got)
	}
}

// The typed mirror: while the add question is the armed one,
// a typed yes belongs to it, and the dispute confirmation
// armed from the merchant's chat must not take it.
func TestBuyerTypedYesBelongsToTheArmedQuestion(t *testing.T) {
	setupChat(t, trustOwner, shopTrust)
	buyer := "233557777777"
	ref := "EFUA-0002-TST"
	paidOrderFor("efua", ref, buyer, 850)
	orders.Default.Update(ref, func(o *orders.Order) { o.DisputeState = "refund_claimed" })

	c := &chat{seen: map[string]int{}}
	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Hello")
	c.replies(t, buyer)
	s := sessions.get(sessionKey("efua", buyer))
	if s == nil {
		t.Fatal("buyer session missing")
	}
	s.DisputeConfirmRef = ref
	bag := catalogue.FindByName("efua", "Handmade Beaded Bag")
	s.Product = bag
	s.PendingAdd = bag
	s.Stage = "fulfilment"
	s.FulfilStep = "choice"
	askYesNo(buyer, s, "add", bag.Name, "Add the Handmade Beaded Bag?", "Yes, add it", "No, leave it")

	c.say(t, buyer, "yes")
	if got := orders.Default.GetOrder(ref); got == nil || got.DisputeState != "refund_claimed" {
		t.Fatalf("a typed yes for the add question closed the dispute: %+v", got)
	}
	if s.PendingAdd != nil {
		t.Fatal("the add question did not take its own typed yes")
	}
}

func TestMerchantYesNoTapAnswersOnlyItsOwnQuestion(t *testing.T) {
	setupChat(t, trustOwner, shopTrust)
	buyer := "233557777777"
	ms := merchantSessionFor("efua", trustOwner)
	openInHand(inHandRequest{ShopID: "efua", Buyer: buyer, Item: "Handmade Beaded Bag", Price: 850})
	askYesNo(trustOwner, ms, "inhand", buyer, "Do you have it in hand now?", "Yes, in hand", "Not in hand")
	inhandTap := "ynq_" + ms.YesNoQ.ID + "_y"

	// She then starts a removal; its question is armed last.
	ms.PendingRemove = "Handmade Beaded Bag"
	askYesNo(trustOwner, ms, "remove", "Handmade Beaded Bag", "Remove Handmade Beaded Bag from your catalogue?", "Yes, remove it", "No, keep it")

	// The stale in-hand tap executes nothing.
	HandleInbound(Inbound{From: trustOwner, ButtonID: inhandTap})
	if catalogue.FindByName("efua", "Handmade Beaded Bag") == nil {
		t.Fatal("a stale in-hand tap removed the product")
	}
	if oldestOpenInHand("efua") == nil {
		t.Fatal("a stale tap settled the in-hand wait")
	}

	// The removal question's own tap removes the product,
	// and the in-hand wait still stands.
	HandleInbound(Inbound{From: trustOwner, ButtonID: "ynq_" + ms.YesNoQ.ID + "_y"})
	if catalogue.FindByName("efua", "Handmade Beaded Bag") != nil {
		t.Fatal("the removal tap did not remove the product")
	}
	if oldestOpenInHand("efua") == nil {
		t.Fatal("the removal tap settled the in-hand wait")
	}
}
