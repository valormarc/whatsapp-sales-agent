package agent

// The merchant's side of thinking first. Her questions about
// a payment are answered from her book and a live check of
// the link, never bounced back at her as a question about
// what she meant. Her instruction to ask the buyer is an
// errand the agent runs: the buyer is asked, and her answer
// is carried back. And a question about a command (her own
// quoted line, with "explain" in front of it) is never
// executed as the command itself.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

func TestMerchantOrderPaymentAnswersFromTheRecord(t *testing.T) {
	merchant := "233550002001"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550002002"
	seedOpenOrder(t, "TEST-5001", buyer, 1)
	order := orders.Default.GetOrder("TEST-5001")
	createClaim(buyer, catalogue.GetMerchant(MerchantID), order, nil, "", false)
	fakeSeevReadback(t, "pending", 875)

	shop := catalogue.GetMerchant(MerchantID)
	if !actOnMerchantUnderstanding(merchant, shop, MerchantID, "Is it made to seev link or my momo direct?", &understood{Intent: intentOrderPayment}) {
		t.Fatal("the route question must be answered")
	}
	got := strings.Join(c.replies(t, merchant), "\n")
	for _, want := range []string{"TEST-5001", "straight to your MoMo number", "waiting on your word", "checked the payment link", "nothing has come through there"} {
		if !strings.Contains(got, want) {
			t.Fatalf("answer is missing %q: %q", want, got)
		}
	}
}

func TestMerchantOrderPaymentSettlesALandedLinkPayment(t *testing.T) {
	merchant := "233550002011"
	setupChat(t, merchant, shopWithBag)
	buyer := "233550002012"
	seedOpenOrder(t, "TEST-5002", buyer, 1)
	fakeSeevReadback(t, "completed", 875)

	if !actOnMerchantUnderstanding(merchant, catalogue.GetMerchant(MerchantID), MerchantID, "what is happening with the payment", &understood{Intent: intentOrderPayment}) {
		t.Fatal("the payment question must be answered")
	}
	if o := orders.Default.GetOrder("TEST-5002"); o == nil || o.Status != "paid" {
		t.Fatalf("a link payment the check finds landed must settle, got %+v", o)
	}
}

func TestMerchantAskBuyerAndTheAnswerComesBack(t *testing.T) {
	merchant := "233550002021"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550002022"
	seedOpenOrder(t, "TEST-5003", buyer, 1)
	order := orders.Default.GetOrder("TEST-5003")
	createClaim(buyer, catalogue.GetMerchant(MerchantID), order, nil, "", false)
	c.replies(t, merchant)

	shop := catalogue.GetMerchant(MerchantID)
	if !actOnMerchantUnderstanding(merchant, shop, MerchantID, "Ask the buyer", &understood{Intent: intentAskBuyer}) {
		t.Fatal("the instruction to ask the buyer must be carried out")
	}
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "TEST-5003") || !strings.Contains(got, "payment link") {
		t.Fatalf("the buyer must be asked the route question, got %q", got)
	}
	if got := strings.Join(c.replies(t, merchant), "\n"); !strings.Contains(got, "I have asked your buyer") {
		t.Fatalf("the merchant must be told the errand is done, got %q", got)
	}

	// Her answer names the link: the agent checks it and
	// reports the outcome to both sides.
	fakeSeevReadback(t, "pending", 875)
	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "I paid through the link")
	if got := strings.Join(c.replies(t, merchant), "\n"); !strings.Contains(got, "has not come through on the link yet") {
		t.Fatalf("the merchant must receive the checked answer, got %q", got)
	}
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "has not come through yet") {
		t.Fatalf("the buyer must be told the result, got %q", got)
	}
}

func TestExplainQuestionNeverRunsTheCommand(t *testing.T) {
	merchant := "233550002031"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	// Her own allowance line quoted back at her, with the
	// figures inside it, under an explain question: the
	// question is answered (here by the day answer, the model
	// being off), and the allowance is never set by it.
	c.say(t, merchant, "Explain this, I don't understand: you have not set a daily giveaway allowance, so it stands at GH₵0. To set it, say: set allowance 300.")
	// The fixture's allowance is 300; the danger in the wild
	// was the quoted GH₵0 being executed as the amount.
	if got := catalogue.GetMerchant(MerchantID).DailyGiveawayAllowance; got != 300 {
		t.Fatalf("an explain question must not move the allowance, got %v", got)
	}
	for _, line := range c.replies(t, merchant) {
		if strings.Contains(line, "Done. Your daily giveaway allowance is now") {
			t.Fatalf("the quoted figures were executed as a command: %q", line)
		}
	}
}

func TestVoiceGuardRefusesPidginAndTitles(t *testing.T) {
	merchant := "233550002041"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550002042"
	shop := catalogue.GetMerchant(MerchantID)

	if actOnBuyerUnderstanding(buyer, &Session{Stage: "browsing"}, shop, MerchantID, &understood{Intent: intentAsk, Reply: "Madam, make I check the price for you."}) {
		t.Fatal("a Pidgin draft carrying a title must never be sent")
	}
	if got := c.replies(t, buyer); len(got) != 0 {
		t.Fatalf("nothing should have been sent, got %v", got)
	}
	if actOnMerchantUnderstanding(merchant, shop, MerchantID, "", &understood{Intent: intentAsk, Reply: "Madam, your bag is listed at GH₵850."}) {
		t.Fatal("a draft addressing the owner as Madam must never be sent")
	}
}
