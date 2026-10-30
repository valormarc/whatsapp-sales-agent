package agent

// The details forms, pinned: a completion is read only
// against the token its own session issued; a buyer's
// delivery form fills the fulfilment it was offered beside
// and moves the walk on; a merchant's payout form records
// the number and the name in the book; and a form finished
// after its moment passed attaches to nothing.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

func TestDeliveryFormFillsTheFulfilmentAndMovesTheWalkOn(t *testing.T) {
	owner := "233550010001"
	buyer := "233550010101"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	s := &Session{
		Stage: "browsing", MerchantID: MerchantID,
		PendingFulfil: fulfilment{Mode: "delivery", Location: "Accra"},
		FulfilStep:    "pkgname",
		PendingFlow:   "delivery-test1", FlowKind: flowKindDelivery,
	}
	sessions[sessionKey(MerchantID, buyer)] = s
	bindings[buyer] = MerchantID

	HandleInbound(Inbound{From: buyer, FlowReply: `{"flow_token":"delivery-test1","buyer_name":"Ama Serwaa","phone":"0244123456","town":"Kasoa","landmark":"Near the station"}`})
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Got them, thank you.") || !strings.Contains(got, "Any message for the seller") {
		t.Fatalf("the completion should thank her and ask the walk's next question, got %q", got)
	}
	f := s.PendingFulfil
	if f.PackageName != "Ama Serwaa" || f.Location != "Kasoa" || f.Phone != "0244123456" || f.Landmark != "Near the station" {
		t.Fatalf("the form's answers should sit on the fulfilment, got %+v", f)
	}
	if s.FulfilStep != "pkgmessage" {
		t.Fatalf("the walk should have moved to its message step, at %q", s.FulfilStep)
	}
	if s.PendingFlow != "" {
		t.Fatal("the issued token should be spent by its completion")
	}
}

func TestAFormCompletionWithAnotherTokenFillsNothing(t *testing.T) {
	owner := "233550010002"
	buyer := "233550010102"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	s := &Session{
		Stage: "browsing", MerchantID: MerchantID,
		PendingFulfil: fulfilment{Mode: "delivery"},
		FulfilStep:    "pkgname",
		PendingFlow:   "delivery-mine", FlowKind: flowKindDelivery,
	}
	sessions[sessionKey(MerchantID, buyer)] = s
	bindings[buyer] = MerchantID

	HandleInbound(Inbound{From: buyer, FlowReply: `{"flow_token":"delivery-someone-elses","buyer_name":"Ama Serwaa","phone":"0244123456","town":"Kasoa"}`})
	if got := c.replies(t, buyer); len(got) != 0 {
		t.Fatalf("a foreign completion should be let go quietly, got %q", got)
	}
	if s.PendingFulfil.PackageName != "" || s.PendingFlow != "delivery-mine" {
		t.Fatalf("a foreign completion changed the session: %+v", s.PendingFulfil)
	}
}

func TestPayoutFormRecordsTheNumberAndTheName(t *testing.T) {
	owner := "233550010003"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	s := &Session{Stage: "merchant", MerchantID: MerchantID, PendingFlow: "payout-test1", FlowKind: flowKindPayout}
	sessions[sessionKey(MerchantID, owner)] = s

	HandleInbound(Inbound{From: owner, FlowReply: `{"flow_token":"payout-test1","holder":"Efua Mensah","phone":"0244987654"}`})
	shop := catalogue.GetMerchant(MerchantID)
	if shop == nil || shop.PayoutPhone != "0244987654" || shop.PayoutHolderName != "Efua Mensah" {
		t.Fatalf("the payout form should record the number and the name, got %+v", shop)
	}
	got := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "0244987654") && !strings.Contains(got, "244987654") {
		t.Fatalf("she should be told where her money now settles, got %q", got)
	}
}

func TestAFormFinishedAfterItsMomentAttachesToNothing(t *testing.T) {
	owner := "233550010004"
	buyer := "233550010104"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	s := &Session{Stage: "browsing", MerchantID: MerchantID, PendingFlow: "delivery-late", FlowKind: flowKindDelivery}
	sessions[sessionKey(MerchantID, buyer)] = s
	bindings[buyer] = MerchantID

	HandleInbound(Inbound{From: buyer, FlowReply: `{"flow_token":"delivery-late","buyer_name":"Ama Serwaa","phone":"0244123456","town":"Kasoa"}`})
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "moved on") {
		t.Fatalf("a late completion should be answered plainly, got %q", got)
	}
	if s.PendingFulfil.PackageName != "" {
		t.Fatalf("a late completion attached itself: %+v", s.PendingFulfil)
	}
}

func TestDeliveryFormIsOfferedOnlyWhenConfigured(t *testing.T) {
	owner := "233550010005"
	buyer := "233550010105"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	s := &Session{Stage: "browsing", MerchantID: MerchantID, PendingFulfil: fulfilment{Mode: "delivery"}}
	sessions[sessionKey(MerchantID, buyer)] = s

	offerDeliveryDetailsFlow(buyer, s)
	if got := c.replies(t, buyer); len(got) != 0 {
		t.Fatalf("with no form configured, no offer should be sent, got %q", got)
	}

	t.Setenv("STALL_FLOW_DELIVERY_ID", "flow-123")
	offerDeliveryDetailsFlow(buyer, s)
	got := c.replies(t, buyer)
	if len(got) != 1 || s.PendingFlow == "" || s.FlowKind != flowKindDelivery {
		t.Fatalf("a configured form should be offered once, with its token kept, got %q / %+v", got, s)
	}
}
