// Haggling: reading a buyer's offer, running one negotiation
// step through the verified engine, and opening and working a
// haggle on a session.
package agent

import (
	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/haggle"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/persona"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
)

var offerRe = regexp.MustCompile(`(\d+(?:\.\d{1,2})?[kK]\b|\d+(?:\.\d{1,2})?)`)
var numbersRe = regexp.MustCompile(`(\d+(?:\.\d+)?[kK]\b|\d+(?:\.\d+)?)`)

// parseNumbered reads one figure matched by the offer and
// numbers patterns, honouring a trailing k as the thousand
// it says: "1k" and "1.5k" are how buyers and sellers both
// write a thousand cedis, and reading the 1 alone would
// misprice everything it touches.
func parseNumbered(s string) (float64, error) {
	if strings.HasSuffix(s, "k") || strings.HasSuffix(s, "K") {
		v, err := strconv.ParseFloat(s[:len(s)-1], 64)
		return v * 1000, err
	}
	return strconv.ParseFloat(s, 64)
}

// parseOffer reads the first price in a message. A number that
// forms part of a quantity phrase is a quantity, never a price,
// on every path that reads offers (browsing, mid-haggle, the
// final stand, the merchant's own amounts): "Ankara 3 yards"
// carries no offer, and in "200 for 3 yards" the 200 is the offer
// and the 3 is the quantity it applies to (see measure.go).
// budgetFrameRe marks a figure she frames as her budget:
// it is context for the conversation, never an offer of
// that amount for the thing in front of her.
var budgetFrameRe = regexp.MustCompile(`(?i)\bbudget\b[^0-9]{0,14}\d+(?:\.\d+)?|\d+(?:\.\d+)?\s*(?:is\s+)?(?:my\s+)?budget\b`)

func parseOffer(text string) *float64 {
	stripped := strings.ReplaceAll(text, ",", "")
	spans := quantityNumberSpans(stripped)
	// An order reference is a name, not money: the serial
	// inside EFUA-0001 is never an offer of 1, and a message
	// quoting her reference is about that order, so its
	// figures are excluded from offer reading altogether.
	for _, idx := range orderRefRe.FindAllStringSubmatchIndex(stripped, -1) {
		spans = append(spans, [2]int{idx[0], idx[1]})
	}
	for _, idx := range budgetFrameRe.FindAllStringIndex(stripped, -1) {
		spans = append(spans, [2]int{idx[0], idx[1]})
	}
	for _, m := range offerRe.FindAllStringSubmatchIndex(stripped, -1) {
		inside := false
		for _, sp := range spans {
			if m[0] < sp[1] && sp[0] < m[1] {
				inside = true
				break
			}
		}
		if inside {
			continue
		}
		f, err := parseNumbered(stripped[m[2]:m[3]])
		if err != nil {
			continue
		}
		return &f
	}
	return nil
}

func numbersIn(text string) []float64 {
	out := []float64{}
	for _, m := range numbersRe.FindAllStringSubmatch(strings.ReplaceAll(text, ",", ""), -1) {
		if f, err := parseNumbered(m[1]); err == nil {
			out = append(out, f)
		}
	}
	return out
}

// The model's words are used only when every number in them is the
// verified price or the list price it is framed against. A message
// that names any other figure is discarded and the framed counter is
// sent instead: the words a customer reads always match the checked
// price.
func modelMessage(proposal *haggle.Proposal, checked haggle.Result, product catalogue.Product) *string {
	msg := ""
	if proposal != nil {
		msg = strings.TrimSpace(proposal.Message)
	}
	if msg == "" {
		return nil
	}
	nums := numbersIn(msg)
	// Spelled-out figures are figures: a proposed message
	// that names the stop in words ("seven hundred") is
	// refused on exactly the digit rule below.
	for _, wf := range wordFigures(msg) {
		nums = append(nums, wf.value)
	}
	contains := false
	for _, n := range nums {
		if n == checked.Price {
			contains = true
		}
	}
	if !contains {
		return nil
	}
	for _, n := range nums {
		if n != checked.Price && n != product.ListPrice {
			return nil
		}
	}
	return &msg
}

type stepResult struct {
	haggle.Result
	ViaModel bool
	Message  *string
}

// One haggling step. The model proposes inside the persona; the engine
// verifies. Any doubt at all (model off, no reply, unparseable reply,
// a price outside the bounds) and Decide makes the call instead.
func negotiateStep(from string, s *Session, offer float64, merchant *catalogue.Merchant) stepResult {
	p := s.Product
	fallback := func() stepResult {
		return stepResult{Result: haggle.Decide(haggle.DecideParams{
			Offer: offer, Ask: s.Ask, Floor: s.Floor, Round: s.Round, StepScale: s.StepScale,
		}), ViaModel: false}
	}
	if !model.Enabled() {
		return fallback()
	}
	day := daybook.Default.GetDayState(merchant.ID, daybook.Options{Allowance: merchant.DailyGiveawayAllowance})
	history := s.History
	if len(history) > 8 {
		history = history[len(history)-8:]
	}
	// On a measured haggle the model's context names the amount
	// in play ("3 yards of Ankara Fabric (6 yards)") and bounds
	// it by the scaled figures on the session's product copy: the
	// model negotiates the cut, never the whole piece by accident.
	promptProduct := *p
	if s.Qty > 0 {
		promptProduct.Name = s.itemLabel()
	}
	handNote := ""
	if len(s.TotalHand) > 0 {
		// A whole hand is haggled as one lot: the drafting
		// speaks of all of them together and never strings
		// the item names into the sentence.
		handNote = " The product here is a whole hand of items being bought together: in the message, speak of them as 'all of them' and never list the item names."
	}
	userContent := "The customer offers " + money.GHS(offer) + " for the " + promptProduct.Name + "." + handNote + " Decide the next step and answer with ONE JSON object only, no other text: " +
		`{"action":"accept"|"counter"|"final","price":<number>,"message":"<the exact short WhatsApp message to send the customer>"}. ` +
		`"accept" settles at the customer's offer. "counter" names your new price. "final" is your last price. ` +
		"Frame the message as movement from the list price. Never propose below your private stop."
	msgs := make([]model.Message, 0, len(history)+1)
	msgs = append(msgs, history...)
	msgs = append(msgs, model.Message{Role: "user", Content: userContent})
	ctx := persona.Context{
		Merchant:  *merchant,
		Product:   &promptProduct,
		Bounds:    &persona.Bounds{ListPrice: p.ListPrice, Stop: s.Floor, Ask: s.Ask},
		Day:       &day,
		Catalogue: catalogue.ListProducts(merchant.ID),
	}
	raw := model.Chat(msgs, model.Options{Context: &ctx, Turn: turnTokenFor(from)})
	proposal := model.ParseProposal(raw)
	var checked *haggle.Result
	if proposal != nil {
		checked = haggle.CheckProposal(proposal, haggle.Bounds{Offer: offer, Ask: s.Ask, Floor: s.Floor})
	}
	if checked == nil {
		return fallback()
	}
	return stepResult{Result: *checked, ViaModel: true, Message: modelMessage(proposal, *checked, *p)}
}

// openHaggle starts a negotiation session for one product, with the
// day book setting the posture for this customer: how much of the
// day's giveaway allowance is left decides the operative floor and
// how far each concession steps.
func openHaggle(shopID string, merchant *catalogue.Merchant, product *catalogue.Product) *Session {
	day := daybook.Default.GetDayState(shopID, daybook.Options{Allowance: merchant.DailyGiveawayAllowance})
	ctx := daybook.HaggleContext(*product, day)
	variation := 0.95 + rand.Float64()*0.1 // a trader's feel varies a little
	return &Session{
		Stage:      "haggling",
		MerchantID: shopID,
		Product:    product,
		Ask:        product.ListPrice,
		Round:      0,
		Floor:      ctx.Floor,
		StepScale:  ctx.StepScale * variation,
	}
}

// counterFrame frames a counter figure. A whole hand is
// spoken of as all of them together, the items themselves
// having been listed when the haggle opened; a single item
// is framed on its own name.
func counterFrame(s *Session, listPrice, price float64) string {
	if len(s.TotalHand) > 0 {
		if price >= listPrice {
			return "I can do " + money.GHS(price) + " for all of them."
		}
		return "From " + money.GHS(listPrice) + ", I have come down to " + money.GHS(price) + " for all of them."
	}
	return persona.FrameCounter(listPrice, price, s.itemLabel())
}

// correctOverOffer speaks the correction when a buyer's
// figure stands above the price itself (the owner's
// ruling, from his own phone test): the agent must tell her the
// price she is calling is a wrong price — "the price is
// GH₵80, you cannot offer GH₵100" — at the moment she
// calls it, not silently take the offer and quietly
// settle her at the price at the end, which reads as
// having accepted the wrong figure. The sale then closes
// at the true price as it always did; only the silence
// is gone. An offer that merely meets a standing ask
// below the list price is ordinary trading and draws no
// correction: the figure she beat was a counter, not
// the price.
func correctOverOffer(from string, s *Session, offer float64) {
	p := s.Product
	if p == nil || offer <= p.ListPrice {
		return
	}
	if len(s.TotalHand) > 0 {
		whatsapp.SendText(from, "That comes to "+money.GHS(p.ListPrice)+" in all, not "+money.GHS(offer)+". You cannot offer above the price.")
		return
	}
	whatsapp.SendText(from, "The "+s.itemLabel()+" is "+money.GHS(p.ListPrice)+", not "+money.GHS(offer)+". You cannot offer above the price.")
}

// processOffer runs one buyer offer through the negotiation: the
// model proposes (or the rule engine decides), haggle verification
// bounds the price, and the outcome is an acceptance, a counter, or
// the final stand after which the agent holds its last price.
func processOffer(from string, s *Session, merchant *catalogue.Merchant, offer float64, msg string) {
	key := sessionKey(s.MerchantID, from)
	p := s.Product
	// Her words for this turn are already on the session's
	// record (noteSpeakerWords at the turn's head); the haggle
	// adds its own replies below.
	// A fixed-price product is never haggled, by model or by
	// engine: the seller said the price does not move, so there
	// is no negotiation to run. An offer at the price buys it;
	// anything below it gets the plain fixed answer, and the
	// talk stays open at the price.
	if p.Fixed {
		if offer >= s.Ask {
			correctOverOffer(from, s, offer)
			beginFulfilment(from, s, s.Ask)
			return
		}
		sessions.put(key, s)
		whatsapp.SendText(from, "The "+s.itemLabel()+" is "+money.GHS(s.Ask)+", and that price is fixed. I cannot take "+money.GHS(offer)+" for it. If you want it at "+money.GHS(s.Ask)+", say the word and it is yours.")
		return
	}
	// An opening offer that meets the asking price in full
	// is not an opening at all: she has taken it at the
	// price, and nobody behind a counter haggles against a
	// full-price yes. It closes at the price here, by code,
	// so no road that reaches this point can drag it out.
	if s.Round == 0 && offer >= s.Ask {
		correctOverOffer(from, s, offer)
		beginFulfilment(from, s, s.Ask)
		return
	}
	result := negotiateStep(from, s, offer, merchant)
	if result.Type == "accept" {
		correctOverOffer(from, s, offer)
		beginFulfilment(from, s, result.Price)
		return
	}
	s.Ask = result.Price
	s.Round++
	if result.LastPrice || s.Round >= MaxCounters {
		s.Stage = "lastprice"
		sessions.put(key, s)
		// The final stand carries its next step as its own short
		// piece: the total with delivery, and the link ready now.
		// A bare "that is my final price" is exactly where real
		// chats end in silence.
		standFor := "for the " + p.Name
		if len(s.TotalHand) > 0 {
			standFor = "for all of them"
		}
		stand := "I have come down as far as I can today. My last price " + standFor + " is " + money.GHS(s.Ask) + ". Say the word and I will send the payment link, no pressure at all."
		if fee := deliveryFeeOf(merchant); fee > 0 {
			next := "With delivery, the total is " + money.GHS(s.Ask+fee) + ". I can send the payment link now, whenever you say."
			if deliveryPaidToOf(merchant) == "carrier" {
				next = "It is " + money.GHS(s.Ask) + " with me, plus " + money.GHS(fee) + " delivery paid to " + carrierTarget(merchant.DeliveryNote) + " when it reaches you. I can send the payment link now, whenever you say."
			}
			sendPieces(from, stand, next)
		} else {
			whatsapp.SendText(from, stand)
		}
		return
	}
	text := ""
	if result.Message != nil {
		text = *result.Message
	} else {
		text = counterFrame(s, p.ListPrice, result.Price) + " What do you say?"
	}
	s.History = lastN(append(append([]model.Message{}, s.History...), model.Message{Role: "assistant", Content: text}), 12)
	sessions.put(key, s)
	whatsapp.SendText(from, text)
}
