package agent

// The words around a settled payment are composed, not
// scripted. The seller set the format (2026-10-28): once a
// payment is confirmed, the buyer hears, in the agent's own
// words, that her payment is confirmed and for how much, her
// order reference, what happens with the goods from the
// order's own facts, and thanks for buying from the shop. No
// line of it is hard coded, and nothing generic stands in for
// her order: the model drafts from the facts of THIS order,
// and hard code verifies the draft before it can be sent: the
// amount and the reference must be exact, every figure must be
// one of the order's own figures, the voice laws hold, and no
// promise the shop has not made may appear (no rider numbers,
// no calls, no packing or dispatch claims). A draft that fails
// any check, and a quiet model, both fall back to a bare
// factual confirmation: the facts with no decoration on them.

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/seevwebhook"
)

func init() {
	seevwebhook.BuyerText = ComposeBuyerSettleText
}

// settlePromiseRe bars the promises an agent may not make on a
// shop's behalf: it does not ride, dispatch or telephone, and
// it does not know the packing table. A draft carrying one is
// not sent.
var settlePromiseRe = regexp.MustCompile(`(?i)(rider|driver|courier).{0,40}(number|phone|call)|\bwill call you\b|\bcall you before\b|dispatched|on its way|\bpacked\b`)

// settleFacts lays one order's facts out for the drafting: the
// same record the settlement itself stands on, nothing else.
func settleFacts(o *orders.Order) string {
	shop := ""
	if m := catalogue.GetMerchant(o.MerchantID); m != nil {
		shop = m.Name
	}
	var b strings.Builder
	b.WriteString("Shop: " + shop + "\n")
	b.WriteString("Product: " + o.ProductName + "\n")
	b.WriteString("Amount paid: " + money.GHS(o.Charged()) + "\n")
	b.WriteString("Order reference: " + o.Reference + "\n")
	if o.PackageName != "" {
		b.WriteString("The package carries the name: " + o.PackageName + "\n")
	}
	switch o.Fulfilment {
	case "delivery":
		if o.Location != "" {
			b.WriteString("How the goods move: delivery to " + o.Location + ".\n")
		} else {
			b.WriteString("How the goods move: delivery, arranged with the buyer.\n")
		}
		if o.DeliveryFee > 0 {
			if o.DeliveryPaidTo == "merchant" {
				b.WriteString("The delivery fee was paid with the order.\n")
			} else {
				b.WriteString("The delivery fee of " + money.GHS(o.DeliveryFee) + " is paid to whoever brings the item, on arrival; it was not part of this payment.\n")
			}
		}
	case "pickup":
		if o.Collector != "" {
			b.WriteString("How the goods move: " + o.Collector + " collects them from the shop.\n")
		} else {
			b.WriteString("How the goods move: the buyer collects them from the shop herself.\n")
		}
	case "meet":
		b.WriteString("How the goods move: the seller has the meeting place on the order: " + o.Location + ".\n")
	default:
		b.WriteString("How the goods move: delivery is arranged with the seller.\n")
		if o.Location != "" {
			b.WriteString("The place she gave: " + o.Location + ".\n")
		}
	}
	return b.String()
}

// checkSettleDraft verifies a drafted settlement confirmation
// the way the haggle guardrail verifies a price: the amount
// and the reference must appear exactly, every cedi figure in
// the draft must be one of the order's own, the voice laws
// hold, and no unmade promise appears.
func checkSettleDraft(draft string, o *orders.Order) bool {
	if draft == "" || settlePromiseRe.MatchString(draft) || !voiceClean(draft) {
		return false
	}
	if !strings.Contains(draft, money.GHS(o.Charged())) || !strings.Contains(draft, o.Reference) {
		return false
	}
	// Every figure in the draft must be one of the order's
	// own. The probe drops the reference (already verified
	// above; its digits are the order's name, not a figure)
	// and the thousands commas a written amount carries.
	allowed := map[string]bool{plainFigure(o.Charged()): true}
	if o.DeliveryFee > 0 {
		allowed[plainFigure(o.DeliveryFee)] = true
	}
	probe := strings.ReplaceAll(draft, o.Reference, "")
	probe = strings.ReplaceAll(probe, ",", "")
	for _, fig := range figureTokenRe.FindAllString(probe, -1) {
		if !allowed[fig] {
			return false
		}
	}
	return true
}

// plainFigure writes an amount the way figureTokenRe reads
// figures out of a draft: digits and a decimal part, no
// currency mark, no thousands commas, no trailing zeros.
func plainFigure(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}

// fallbackSettleText is the bare factual net under the
// drafting: the facts of the order with nothing decorative on
// them, used when the model is quiet or a draft fails its
// checks.
func fallbackSettleText(o *orders.Order) string {
	shop := ""
	if m := catalogue.GetMerchant(o.MerchantID); m != nil {
		shop = m.Name
	}
	line := "Payment confirmed: " + money.GHS(o.Charged()) + " for the " + o.ProductName + ". Order " + o.Reference + "."
	switch o.Fulfilment {
	case "delivery":
		if o.Location != "" {
			line += " It goes to " + o.Location + "."
		}
		if o.DeliveryFee > 0 && o.DeliveryPaidTo != "merchant" {
			line += " Please keep " + money.GHS(o.DeliveryFee) + " for the delivery when it reaches you."
		}
	case "pickup":
		if o.Collector != "" {
			line += " " + o.Collector + " can collect it from the shop."
		}
	case "meet":
		if o.Location != "" {
			line += " The meeting place on your order is " + o.Location + "."
		}
	default:
		if o.Location != "" {
			line += " The shop has " + o.Location + " noted for the delivery."
		}
	}
	if shop != "" {
		line += " Thanks for buying from " + shop + "."
	}
	return line
}

// ComposeBuyerSettleText drafts the buyer's settlement
// confirmation from her order's facts, in the agent's own
// words and the seller's format, and verifies the draft
// before it stands. It is the registered composer behind
// seevwebhook.BuyerText, so every road that settles a link
// payment (the webhook, the agent's own read-back, the watch)
// speaks this way, and the direct path's confirmation in
// payment.go calls it as well.
func ComposeBuyerSettleText(o *orders.Order, _ string) string {
	return composeBuyerSettle(o, false)
}

// ComposeDirectSettleText is the same composition for the
// direct path, where the confirmation is the seller's own
// word on money she received on her number, and the facts
// say so.
func ComposeDirectSettleText(o *orders.Order) string {
	return composeBuyerSettle(o, true)
}

func composeBuyerSettle(o *orders.Order, direct bool) string {
	if o == nil {
		return ""
	}
	if model.Enabled() {
		facts := settleFacts(o)
		if direct {
			facts += "How it was confirmed: the seller received the money on her own MoMo number and has confirmed it herself.\n"
		}
		if draft := draftWithBudget(settleCharter, "The facts of the settled order:\n"+facts+"\nWrite the confirmation to the buyer now, following the format, in your own words."); draft != "" && checkSettleDraft(draft, o) {
			return draft
		}
	}
	return fallbackSettleText(o)
}

// draftWithBudget runs one drafting call with a patience
// budget. Composed words are worth a few seconds of a
// buyer's wait, never more: past the budget the caller falls
// back to its bare factual net, and the late draft is
// discarded. A settlement must never be held hostage by a
// slow model.
func draftWithBudget(system, facts string) string {
	ch := make(chan string, 1)
	go func() {
		r := model.Chat([]model.Message{{Role: "user", Content: facts}}, model.Options{System: &system})
		if r == nil {
			ch <- ""
		} else {
			ch <- strings.TrimSpace(*r)
		}
	}()
	select {
	case d := <-ch:
		return d
	case <-time.After(6 * time.Second):
		return ""
	}
}

// settleCharter is the format, stated as a format: the shape
// the seller set, to be followed in the agent's own words from
// the facts given, never recited.
const settleCharter = `You are the sales agent for a small shop on WhatsApp. A buyer's payment has just been confirmed. Write her confirmation message now, in your own words, following this format:

- Open with the confirmation itself: her payment has come through, naming the product and the exact amount paid.
- Give her the order reference.
- Say what happens with the goods, from the facts given: how they reach her, the place or the person named on the order, the name the package carries, and the delivery money arrangement exactly as the facts state it.
- Close by thanking her for buying from the shop, naming the shop.

Rules: use only the facts given; invent nothing. Never promise a rider's or driver's number, a phone call, a packing state, a dispatch or a delivery time: the shop makes its own arrangements and you report only what the order holds. Keep it short and warm, plain correct English, complete sentences, no titles for anyone, no Pidgin. The amount and the reference must appear exactly as given.`
