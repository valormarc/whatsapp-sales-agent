package agent

import (
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/haggle"
)

// Regression for the audit finding that figures spelled out
// in words walked past every figure gate: the verifier read
// only digits, so "five thousand cedis" passed with no
// figures allowed at all, and a haggle message naming the
// private stop in words was sent verbatim.
func TestFiguresCleanReadsWords(t *testing.T) {
	if figuresClean("The price is five thousand cedis.") {
		t.Fatal("a spelled-out figure with no figures allowed must be refused")
	}
	if !figuresClean("The price is five thousand cedis.", 5000) {
		t.Fatal("a spelled-out figure that IS the allowed fact must pass")
	}
	if figuresClean("It comes to seven hundred.", 850) {
		t.Fatal("a large spelled figure is money even without a currency word")
	}
	if !figuresClean("I have one bag left, and two more coming.") {
		t.Fatal("small bare number words are ordinary English, not figures")
	}
	if !figuresClean("There are fifty cedis of delivery on it.", 50) {
		t.Fatal("a small spelled figure beside its currency word, when allowed, must pass")
	}
	if figuresClean("There are fifty cedis of delivery on it.", 60) {
		t.Fatal("a small spelled figure beside its currency word must still be checked")
	}
	if figuresClean("The price is GH₵500.") {
		t.Fatal("the digit control must still be refused")
	}
}

// The haggle guard now sees word figures: a proposed message
// naming the stop in words is discarded exactly like the
// digit form, and a clean message whose price is spelled
// out is usable.
func TestModelMessageReadsWords(t *testing.T) {
	product := catalogue.Product{Name: "Bag", ListPrice: 850, FloorPrice: 700}
	checked := haggle.Result{Price: 750}
	stop := &haggle.Proposal{Message: "I can go to 750, but seven hundred is my last last."}
	if got := modelMessage(stop, checked, product); got != nil {
		t.Fatalf("a message naming the stop in words must be discarded, got %q", *got)
	}
	digitStop := &haggle.Proposal{Message: "I can go to 750, but 700 is my last last."}
	if got := modelMessage(digitStop, checked, product); got != nil {
		t.Fatalf("the digit control must be discarded, got %q", *got)
	}
	clean := &haggle.Proposal{Message: "I can do seven hundred and fifty for you."}
	if got := modelMessage(clean, checked, product); got == nil {
		t.Fatal("a clean message spelling the checked price must be usable")
	}
}

// The discovery door no longer prefers an unverified draft:
// a draft carrying any figure falls back to the factual
// line (the audit watched it return a "900" draft).
func TestDiscoveryReplyOrVerifiesFigures(t *testing.T) {
	u := &discoveryUnderstanding{Reply: "That will be 900, no problem."}
	if got := discoveryReplyOr(u, "the factual line"); got != "the factual line" {
		t.Fatalf("a draft with an unverified figure must fall back, got %q", got)
	}
	u2 := &discoveryUnderstanding{Reply: "No problem at all. What are you looking to buy?"}
	if got := discoveryReplyOr(u2, "the factual line"); got != u2.Reply {
		t.Fatalf("a figure-free draft is used, got %q", got)
	}
}
