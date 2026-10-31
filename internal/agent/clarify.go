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
	var prices []float64
	for _, p := range products {
		if p.Stock <= 0 {
			continue
		}
		prices = append(prices, p.ListPrice)
	}
	if len(prices) == 0 {
		return ""
	}
	sys := "You are Stall, the assistant selling for " + merchant.Name + " inside a chat. " +
		"The buyer just wrote something that does not clearly ask for anything on the shelf. " +
		"Reply with ONE short, polite sentence asking what they are looking for. " +
		"Do not name any of the shop's goods or any price: goods are listed by the platform itself when they are asked for, never strung into a sentence as examples. " +
		"Plain, correct English, no greeting, no exclamation marks."
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
	// The clarifier only asks; a draft that names a good
	// anyway is a list pretending to be talk, and the
	// caller's plain template stands in for it.
	lowerLine := strings.ToLower(line)
	for _, p := range products {
		if strings.Contains(lowerLine, strings.ToLower(p.Name)) {
			return ""
		}
	}
	return line
}
