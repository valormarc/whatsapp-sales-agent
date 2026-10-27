package agent

// Sold by measure (taught from a live
// buyer run): goods in the real market move by measure as often
// as whole. A 6-yard piece at GH₵450 is GH₵75 a yard, so the
// seller can cut and sell 3 yards for GH₵225; or she does not
// split pieces at all, and says so plainly. It is not only cloth:
// litres of oil, kilos of grain, olonkas and tins of anything.
//
// This file carries the reading of quantity phrases and the buyer
// behaviour around them. Two faults from the live run die here:
//
//   - A number inside a quantity phrase ("3 yards") is a
//     quantity, never a price. parseOffer skips those numbers on
//     every path, so "Ankara 3 yards" opens no haggle, while
//     "200 for 3 yards" reads as quantity 3, offer 200, the offer
//     applying to the 3 yards.
//   - A product with a measure is quoted, haggled and closed on
//     the scaled figures for the quantity in play (see the
//     scaling maths on catalogue.Product), and a product without
//     one answers a split request in the merchant's real words
//     instead of haggling over a piece it does not sell.

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// unitSynonyms maps every spelling a buyer or merchant types to
// the measure's canonical singular lower-case name, the only form
// the catalogue stores.
var unitSynonyms = map[string]string{
	"yard": "yard", "yards": "yard",
	"metre": "metre", "metres": "metre", "meter": "metre", "meters": "metre",
	"litre": "litre", "litres": "litre", "liter": "litre", "liters": "litre", "l": "litre",
	"kilo": "kilo", "kilos": "kilo", "kg": "kilo",
	"gram": "gram", "grams": "gram", "g": "gram",
	"olonka": "olonka", "olonkas": "olonka",
	"tin": "tin", "tins": "tin",
}

// quantityRe finds a quantity phrase: a number (or the word
// "half", for half a yard and its kin) directly before a unit
// word. Longer spellings come first in the alternation so "litres"
// is never read as the single letter "l".
var quantityRe = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?|half)\s*(litres?|liters?|yards?|metres?|meters?|kilos?|kg|grams?|g|olonkas?|tins?|l)\b`)

// quantity is one parsed quantity phrase: how much, of which
// canonical measure, and the phrase as written (the pack check
// below reads it).
type quantity struct {
	Qty  float64
	Unit string
	Text string
}

// parseQuantity reads the first quantity phrase in a message, if
// there is one. "half" before the unit is half a unit.
func parseQuantity(text string) (quantity, bool) {
	m := quantityRe.FindStringSubmatch(text)
	if m == nil {
		return quantity{}, false
	}
	qty := 0.5
	if !strings.EqualFold(m[1], "half") {
		f, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return quantity{}, false
		}
		qty = f
	}
	unit, ok := unitSynonyms[strings.ToLower(m[2])]
	if !ok {
		return quantity{}, false
	}
	return quantity{Qty: qty, Unit: unit, Text: strings.ToLower(m[0])}, true
}

// quantityNumberSpans returns the spans of the numbers that form
// part of quantity phrases in the text. parseOffer skips them:
// "3 yards" carries no offer, and in "3 yards for 200" the only
// price in the message is the 200.
func quantityNumberSpans(text string) [][2]int {
	var spans [][2]int
	for _, m := range quantityRe.FindAllStringSubmatchIndex(text, -1) {
		spans = append(spans, [2]int{m[2], m[3]})
	}
	return spans
}

// canonicalUnit resolves one typed word to its canonical measure,
// when it is a measure word at all.
func canonicalUnit(word string) (string, bool) {
	u, ok := unitSynonyms[strings.ToLower(word)]
	return u, ok
}

// unitMentioned finds a measure named in a message without a
// quantity attached: how a merchant answers the by-measure
// question ("by the yard", "kilo"). Single letters are not read
// as measures here: an "l" alone in prose is too easy to hit by
// accident, and a measure answer names its unit in full.
func unitMentioned(lower string) (string, bool) {
	for _, w := range bareWordSplit.Split(lower, -1) {
		if w == "l" || w == "g" {
			continue
		}
		if u, ok := unitSynonyms[w]; ok {
			return u, true
		}
	}
	return "", false
}

// formatQty renders a quantity the way amounts are rendered
// everywhere here: whole numbers plain, fractions short (3, 1.5).
func formatQty(qty float64) string {
	return money.JSNumber(qty)
}

// unitWord is the unit as it is spoken for a quantity: singular
// for exactly one ("1 yard"), plural otherwise ("3 yards").
func unitWord(unit string, qty float64) string {
	if qty == 1 {
		return unit
	}
	return unit + "s"
}

// qtyLabel names a measured amount of a product, the way model
// context, haggle framing and the merchant's sale notice name it:
// "3 yards of Ankara Fabric (6 yards)".
func qtyLabel(p *catalogue.Product, qty float64) string {
	return formatQty(qty) + " " + unitWord(p.Unit, qty) + " of " + p.Name
}

// itemLabel names what a session is negotiating: the measured
// amount when the session carries a quantity, the product itself
// otherwise.
func (s *Session) itemLabel() string {
	if s.Product == nil {
		return ""
	}
	if s.Qty > 0 && s.Product.Unit != "" {
		return qtyLabel(s.Product, s.Qty)
	}
	return s.Product.Name
}

// measureQuotePieces is the plain quote for a measured amount,
// in pieces: what the amount costs, then what the full base
// quantity costs, so the buyer can check the maths herself.
// "3 yards of the Ankara Fabric (6 yards) is GH₵225." /
// "The full 6 yards is GH₵450."
func measureQuotePieces(p *catalogue.Product, qty float64) []string {
	return []string{
		formatQty(qty) + " " + unitWord(p.Unit, qty) + " of the " + p.Name + " is " + money.GHS(p.ScaledList(qty)) + ".",
		"The full " + formatQty(p.BaseQty) + " " + unitWord(p.Unit, p.BaseQty) + " is " + money.GHS(p.ListPrice) + ".",
	}
}

var (
	perUnitRe = regexp.MustCompile(`\bper\s+([a-z]+)`)
	oneUnitRe = regexp.MustCompile(`\bone\s+([a-z]+)\b`)
)

// asksPerUnit reads a per-unit price question for a by-measure
// product: "how much per yard", "price for one litre". It names
// the product's own unit in a per or one frame; anything else is
// some other question.
func asksPerUnit(lower string, p catalogue.Product) bool {
	if m := perUnitRe.FindStringSubmatch(lower); m != nil {
		if u, ok := canonicalUnit(m[1]); ok && u == p.Unit {
			return true
		}
	}
	if m := oneUnitRe.FindStringSubmatch(lower); m != nil {
		if u, ok := canonicalUnit(m[1]); ok && u == p.Unit &&
			(howMuchRe.MatchString(lower) || strings.Contains(lower, "price") || strings.HasSuffix(strings.TrimSpace(lower), "?")) {
			return true
		}
	}
	return false
}

var nonAlnumRe = regexp.MustCompile(`[^a-z0-9]+`)

// isPackDescriptor reports whether a quantity phrase merely
// repeats the pack as the product is listed: the "(500g)" in the
// shea butter's name, the "(1kg)" in the groundnut paste's. A
// buyer naming the goods as listed is not asking the seller to
// split them, so ordinary quoting owns her message, never the
// refusal.
func isPackDescriptor(p catalogue.Product, q quantity) bool {
	name := nonAlnumRe.ReplaceAllString(strings.ToLower(p.Name), "")
	phrase := nonAlnumRe.ReplaceAllString(q.Text, "")
	return phrase != "" && strings.Contains(name, phrase)
}

// scaledCopy returns the product re-priced for a quantity: list
// and floor scaled by the catalogue's measure maths, everything
// else (name, id, the measure itself) as listed. A haggle on a
// measured amount runs on this copy, so the day book context, the
// operative stop, the verification and the close all read the
// scaled figures and nothing else.
func scaledCopy(p *catalogue.Product, qty float64) catalogue.Product {
	c := *p
	if qty > 0 && p.ByMeasure() {
		c.ListPrice = p.ScaledList(qty)
		c.FloorPrice = p.ScaledFloor(qty)
	}
	return c
}

// openHaggleQty opens a haggle on a measured amount of a product:
// the session's product is the scaled copy, and the session
// carries the quantity and unit, so the close records them and
// every line names them.
func openHaggleQty(shopID string, merchant *catalogue.Merchant, product *catalogue.Product, qty float64) *Session {
	sc := scaledCopy(product, qty)
	s := openHaggle(shopID, merchant, &sc)
	if qty > 0 {
		s.Qty = qty
		s.Unit = product.Unit
	}
	return s
}

// baseProductOf resolves the product as listed in the catalogue,
// by id: the unscaled original a session's scaled copy came from,
// needed when a haggle changes quantity and must rescale from the
// base prices, never from an already scaled copy. It returns nil
// when the product has left the shelf.
func baseProductOf(shopID string, p *catalogue.Product) *catalogue.Product {
	if p == nil {
		return nil
	}
	for _, prod := range catalogue.ListProducts(shopID) {
		if prod.ID == p.ID {
			pp := prod
			return &pp
		}
	}
	return nil
}

// wholeProductOf is baseProductOf with the session's own copy as
// the fallback: what a browsing view should hold after a measured
// negotiation ends, so later quotes price the item itself, never
// the measured amount by accident.
func wholeProductOf(shopID string, p *catalogue.Product) *catalogue.Product {
	if base := baseProductOf(shopID, p); base != nil {
		return base
	}
	return p
}

// handleQuantity runs the by-measure behaviour for one buyer
// message, in browsing or mid-haggle, and reports whether it took
// the message. The product in play is the one she names, else the
// haggle's product, else the item she is viewing. Shapes:
//
//   - No product in play, or a different product named mid-haggle:
//     not ours; the ordinary flow owns the turn.
//   - A per-unit price question: the unit price and the full
//     price, no haggle.
//   - A whole-only product with a quantity attached (and the
//     quantity is not just the pack as listed): the refusal in
//     the merchant's real words, browsing only; mid-haggle the
//     turn falls through to the haggle's own holding reply, with
//     the quantity never read as an offer.
//   - A by-measure product with a different unit named: one light
//     clarifier naming the product's own unit.
//   - A by-measure product with a quantity: quoted, haggled on
//     the scaled figures, or, mid-haggle, the haggle restarts on
//     the new quantity.
func handleQuantity(from string, s *Session, merchant *catalogue.Merchant, msg, lower string) bool {
	shopID := s.MerchantID
	named := catalogue.FindProduct(shopID, lower)
	var context *catalogue.Product
	if s.Stage == "haggling" {
		context = s.Product
	} else {
		context = s.LastViewed
	}
	p := named
	if p == nil {
		p = context
	}
	if p == nil {
		return false
	}
	if s.Stage == "haggling" && named != nil && (context == nil || named.ID != context.ID) {
		return false
	}

	q, hasQty := parseQuantity(lower)

	if !hasQty {
		if p.ByMeasure() && asksPerUnit(lower, *p) {
			if s.Stage == "browsing" {
				s.LastViewed = p
			}
			sendPieces(from, measureQuotePieces(p, 1)...)
			return true
		}
		return false
	}

	if !p.ByMeasure() {
		if isPackDescriptor(*p, q) {
			return false
		}
		if s.Stage == "haggling" {
			return false
		}
		s.LastViewed = p
		s.Qty, s.Unit = 0, ""
		sendPieces(from,
			"We do not sell the "+p.Name+" piece by piece.",
			"It is "+money.GHS(p.ListPrice)+" as it comes.")
		return true
	}

	if q.Unit != p.Unit {
		if s.Stage == "browsing" {
			s.LastViewed = p
		}
		whatsapp.SendText(from, "The "+p.Name+" is sold by the "+p.Unit+". How many "+unitWord(p.Unit, 2)+" do you want?")
		return true
	}

	offer := parseOffer(lower)

	if s.Stage == "haggling" {
		if q.Qty == s.Qty {
			if offer != nil {
				processOffer(from, s, merchant, *offer, msg)
				return true
			}
			return false
		}
		base := baseProductOf(shopID, p)
		if base == nil {
			return false
		}
		ns := openHaggleQty(shopID, merchant, base, q.Qty)
		ns.Location = s.Location
		ns.History = s.History
		sessions[sessionKey(shopID, from)] = ns
		ack := "For " + formatQty(q.Qty) + " " + unitWord(p.Unit, q.Qty) + " then."
		if offer != nil {
			whatsapp.SendText(from, ack)
			processOffer(from, ns, merchant, *offer, msg)
			return true
		}
		sendPieces(from, ack, measureQuotePieces(base, q.Qty)[0])
		return true
	}

	// Browsing: the quantity is remembered on the session, so a
	// plain offer in her next message lands on this quantity.
	s.LastViewed = p
	if buyIntentRe.MatchString(lower) {
		scaled := scaledCopy(p, q.Qty)
		s.Qty, s.Unit = q.Qty, p.Unit
		startConfirmation(from, s, merchant, &scaled, scaled.ListPrice, true)
		return true
	}
	if offer != nil {
		loc := s.Location
		ns := openHaggleQty(shopID, merchant, p, q.Qty)
		ns.Location = loc
		sessions[sessionKey(shopID, from)] = ns
		processOffer(from, ns, merchant, *offer, msg)
		return true
	}
	s.Qty, s.Unit = q.Qty, p.Unit
	sendPieces(from, measureQuotePieces(p, q.Qty)...)
	return true
}
