package agent

// The clarifier: what the agent says when a buyer's words do
// not clearly ask for anything it can act on. This used to be
// one fixed sentence everywhere ("Tell me what you are after
// and I will sort you out"), and a fixed sentence is exactly
// what makes the agent feel canned: the same words whatever
// she said, whatever the shop sells. Now the model phrases
// the ask from the shop's own facts when it is reachable,
// and the fixed sentence survives only as the fallback for
// when the model cannot be read, which is what templates
// are for.

import (
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
)

// clarifyLine drafts one short line asking the buyer what
// she is after, grounded in this shop's shelf. It returns
// "" when the model is off, unreachable, over the turn's
// drafting budget, or when its draft fails verification
// (a figure that is not a list price on this shelf, or a
// reply that is not one short line): the caller then sends
// its template, so the buyer is never left unanswered.
func clarifyLine(from string, merchant *catalogue.Merchant, said string) string {
	if merchant == nil || !model.Enabled() {
		return ""
	}
	products := catalogue.ListProducts(merchant.ID)
	var facts []string
	var prices []float64
	for _, p := range products {
		if p.Stock <= 0 {
			continue
		}
		facts = append(facts, p.Name+" at "+money.GHS(p.ListPrice))
		prices = append(prices, p.ListPrice)
	}
	if len(facts) == 0 {
		return ""
	}
	sys := "You are Stall, the assistant selling for " + merchant.Name + " inside a chat. " +
		"The buyer just wrote something that does not clearly ask for anything on the shelf. " +
		"Reply with ONE short, polite sentence asking what they are looking for. " +
		"You may name up to two of the shop's goods as examples, with their prices exactly as given. " +
		"Use no other figures. Plain, correct English, no greeting, no exclamation marks.\n\n" +
		"On the shelf: " + strings.Join(facts, "; ") + "."
	reply := model.Chat([]model.Message{{Role: "user", Content: said}}, model.Options{System: &sys, Kind: model.KindDraft, Turn: turnTokenFor(from)})
	if reply == nil {
		return ""
	}
	line := strings.TrimSpace(*reply)
	if line == "" || len(line) > 240 || strings.Contains(line, "\n") {
		return ""
	}
	if !figuresClean(line, prices...) {
		return ""
	}
	return line
}
