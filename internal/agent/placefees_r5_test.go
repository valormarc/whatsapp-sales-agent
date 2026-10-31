package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// Run-5 cluster H: place and fee bind together on every
// road that can move a place — the typed correction,
// the form completion — and the allowance note's own
// answer shapes are never read as fee quotes.

// A typed place correction at the confirm step rebinds
// the fee from the shop's terms for the new place, so
// the re-sent recap prices delivery for where the goods
// are actually going.
func TestPlaceCorrectionRebindsTheFee(t *testing.T) {
	buyer := "233550006201"
	setupChat(t, stockOwner, shopAreaFees)
	s := &Session{Stage: "browsing", MerchantID: "demo", AgreedPrice: 850, Product: catalogue.FindProduct("demo", "beaded bag")}
	sessions.put(sessionKey("demo", buyer), s)
	f := fulfilment{Mode: "delivery", Location: "Osu", Fee: 30, PackageName: "Ama Serwaa"}
	reading := stageReading{Intent: stageIntentChange, Field: "place", Value: "East Legon"}
	if !applyStatedCorrection(buyer, s, f, "change the place to East Legon", reading) {
		t.Fatal("the stated correction should apply")
	}
	got := sessions.get(sessionKey("demo", buyer))
	if got.PendingFulfil.Location != "East Legon" || got.PendingFulfil.Fee != 50 {
		t.Fatalf("the correction must carry the new place's fee, got %+v", got.PendingFulfil)
	}
}

// At a quote-style shop the same correction cannot
// recap at a stale fee: the fulfilment parks and a
// fresh quote opens for the new place, pausing the
// sale at feewait.
func TestPlaceCorrectionAtAQuoteShopParksForAFreshQuote(t *testing.T) {
	buyer := "233550006202"
	setupChat(t, stockOwner, shopQuoteStyle)
	s := &Session{Stage: "browsing", MerchantID: "demo", AgreedPrice: 850, Product: catalogue.FindProduct("demo", "beaded bag")}
	sessions.put(sessionKey("demo", buyer), s)
	f := fulfilment{Mode: "delivery", Location: "Osu", Fee: 45, PackageName: "Ama Serwaa"}
	reading := stageReading{Intent: stageIntentChange, Field: "place", Value: "Kasoa"}
	if !applyStatedCorrection(buyer, s, f, "change the place to Kasoa", reading) {
		t.Fatal("the stated correction should apply")
	}
	got := sessions.get(sessionKey("demo", buyer))
	if got.FulfilStep != "feewait" {
		t.Fatalf("the sale must pause for the fresh quote, step %q", got.FulfilStep)
	}
	if got.FeePark == nil && len(openFeeQuotes("demo")) == 0 {
		t.Fatal("the fulfilment must be parked against the open quote")
	}
	quotes := openFeeQuotes("demo")
	if len(quotes) != 1 || quotes[0].Place != "Kasoa" {
		t.Fatalf("a fresh quote must stand open for the new place, got %+v", quotes)
	}
	if quotes[0].Parked == nil || quotes[0].Parked.Location != "Kasoa" {
		t.Fatalf("the parked fulfilment must carry the new place, got %+v", quotes[0].Parked)
	}
}

// A form completion that lands at the confirm step has
// changed the order under the recap she confirmed
// against: the recap is read back to her in full before
// any yes can complete the sale.
func TestFlowCompletionAtConfirmResendsTheRecap(t *testing.T) {
	buyer := "233550006203"
	dir := setupChat(t, stockOwner, shopAreaFees)
	c := &chat{dir: dir}
	s := &Session{Stage: "browsing", MerchantID: "demo", AgreedPrice: 850,
		FulfilStep:  "confirm",
		PendingFlow: "delivery-confirm", FlowKind: flowKindDelivery,
		Product: catalogue.FindProduct("demo", "beaded bag"),
		PendingFulfil: fulfilment{Mode: "delivery", Location: "Osu", Fee: 30, PackageName: "Ama Serwaa"}}
	sessions.put(sessionKey("demo", buyer), s)
	bindings.put(buyer, "demo")

	HandleInbound(Inbound{From: buyer, FlowReply: `{"flow_token":"delivery-confirm","town":"East Legon"}`})
	got := sessions.get(sessionKey("demo", buyer))
	if got.PendingFulfil.Location != "East Legon" || got.PendingFulfil.Fee != 50 {
		t.Fatalf("the completion must rebind place and fee, got %+v", got.PendingFulfil)
	}
	joined := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(joined, "East Legon") || !strings.Contains(joined, "GH₵50") {
		t.Fatalf("the recap must be re-sent with the new place and fee, got %q", joined)
	}
}

// A completion that arrives past the details steps —
// here, while the sale waits on a fee quote — attaches
// nothing and is answered plainly.
func TestFlowCompletionPastTheDetailsStepsAttachesNothing(t *testing.T) {
	buyer := "233550006204"
	dir := setupChat(t, stockOwner, shopAreaFees)
	c := &chat{dir: dir}
	s := &Session{Stage: "browsing", MerchantID: "demo", AgreedPrice: 850,
		FulfilStep:  "feewait",
		PendingFlow: "delivery-late2", FlowKind: flowKindDelivery,
		PendingFulfil: fulfilment{Mode: "delivery", Location: "Osu", Fee: 30}}
	sessions.put(sessionKey("demo", buyer), s)
	bindings.put(buyer, "demo")

	HandleInbound(Inbound{From: buyer, FlowReply: `{"flow_token":"delivery-late2","buyer_name":"Ama Serwaa","town":"Kasoa"}`})
	joined := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(joined, "moved on") {
		t.Fatalf("a completion past its steps should be answered plainly, got %q", joined)
	}
	got := sessions.get(sessionKey("demo", buyer))
	if got.PendingFulfil.PackageName != "" || got.PendingFulfil.Location != "Osu" {
		t.Fatalf("nothing may be attached past the details steps, got %+v", got.PendingFulfil)
	}
}

// The typed package name spends the form's token: the
// form was offered beside that very question, and
// whichever she answers first, the other stands down.
func TestTypedPackageNameSpendsTheFlowToken(t *testing.T) {
	buyer := "233550006205"
	setupChat(t, stockOwner, shopWithBag)
	s := &Session{Stage: "browsing", MerchantID: "demo", FulfilStep: "pkgname",
		PendingFlow: "delivery-typed", FlowKind: flowKindDelivery,
		PendingFulfil: fulfilment{Mode: "delivery", Location: "Osu"}}
	sessions.put(sessionKey("demo", buyer), s)
	inTurn(buyer, func() bool {
		handlePackageDetails(buyer, s, "Ama Serwaa", "ama serwaa", stageReading{})
		return true
	})
	got := sessions.get(sessionKey("demo", buyer))
	if got.PendingFlow != "" || got.FlowKind != "" {
		t.Fatalf("the typed answer must spend the form's token, got %q/%q", got.PendingFlow, got.FlowKind)
	}
	if got.PendingFulfil.PackageName != "Ama Serwaa" {
		t.Fatalf("the typed name must still land, got %+v", got.PendingFulfil)
	}
}

// The allowance note's set shapes are answers to the
// note, never to an open fee quote: with a quote
// standing open, "set it 300" consumes nothing, while a
// plain fee answer still binds.
func TestFeeQuoteLaneLeavesTheNoteShapesAlone(t *testing.T) {
	setupChat(t, stockOwner, shopQuoteStyle)
	merchant := catalogue.GetMerchant("demo")
	openFeeQuote(feeQuote{ShopID: "demo", Buyer: "233550006206", Item: "Handmade Beaded Bag", Price: 850, Place: "Osu"})
	if handleMerchantFeeQuote(stockOwner, merchant, "set it 300", "set it 300") {
		t.Fatal("the allowance note's set shape must not be consumed as a fee quote")
	}
	if len(openFeeQuotes("demo")) != 1 {
		t.Fatal("the quote must still stand open after the note-shaped answer")
	}
	if !handleMerchantFeeQuote(stockOwner, merchant, "fee 30", "fee 30") {
		t.Fatal("a plain fee answer must still bind the open quote")
	}
}
