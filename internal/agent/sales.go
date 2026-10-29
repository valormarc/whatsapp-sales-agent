package agent

// The buyer-side sales layer: the judgment a good
// salesperson exercises on top of the catalogue. Three
// connected capabilities, all of them understanding first
// and platform-bound in what they say.
//
// A budget she states is carried for the conversation: it
// is her context, held on her session, never a profile
// that outlives the chat, and recommendations and
// alternatives respect it without her repeating herself.
//
// A vague ask (a gift, an occasion, a purpose, a tight
// purse) is answered with a recommendation: the strongest
// product for her situation, why it fits, its verified
// price, and one alternative. The model judges and drafts;
// code verifies every name and figure it uses, and a
// draft that cannot be verified is replaced by a factual
// pick made by price alone.
//
// A price objection without a figure is a balk, not an
// offer: before any haggle is invited, a cheaper suitable
// product is put in front of her. If she then names a
// figure, the haggle roads take it, inside the merchant's
// limits as always.

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// salesCandidate is one product the sales layer may put in
// front of the buyer, with the shop it belongs to (a
// recommendation across shops names each product's shop).
type salesCandidate struct {
	Shop    *catalogue.Merchant
	Product *catalogue.Product
}

func (c salesCandidate) label() string {
	return c.Product.Name + " — " + c.Product.Description + " — " + money.GHS(c.Product.ListPrice)
}

// --- The carried budget ---

var budgetRe = regexp.MustCompile(`(?i)\b(?:budget|around|under|below|up to|not more than|max(?:imum)?|spend)\b[^0-9]{0,14}(\d[\d,]*(?:\.\d+)?)|(\d[\d,]*(?:\.\d+)?)\s*(?:is|na)?\s*(?:my|the)?\s*budget\b`)

// budgetFigureIn reports whether a stated budget figure
// appears in her own words, the verification every carried
// fact must pass before the platform holds it.
func budgetFigureIn(text string, budget float64) bool {
	for _, tok := range figureTokenRe.FindAllString(text, -1) {
		if v, err := strconv.ParseFloat(strings.ReplaceAll(tok, ",", ""), 64); err == nil && v == budget {
			return true
		}
	}
	return false
}

// netBudget reads a budget off plain words, for the turns
// no model reads: a figure framed as what she can spend.
func netBudget(lower string) float64 {
	m := budgetRe.FindStringSubmatch(lower)
	if m == nil {
		return 0
	}
	for _, g := range m[1:] {
		if g != "" {
			if v, err := strconv.ParseFloat(strings.ReplaceAll(g, ",", ""), 64); err == nil && v > 0 {
				return v
			}
		}
	}
	return 0
}

// carryBudget holds a stated budget on her sessions: the
// one she is speaking in, and the unbound one, so the
// context survives her choosing a shop.
func carryBudget(from string, s *Session, budget float64) {
	if budget <= 0 {
		return
	}
	if s != nil {
		s.Budget = budget
	}
	if pre, ok := sessions[sessionKey("", from)]; ok && pre != s {
		pre.Budget = budget
	}
}

// budgetOf reads the budget in force for this buyer: the
// session's own, or the one she stated before she chose a
// shop.
func budgetOf(from string, s *Session) float64 {
	if s != nil && s.Budget > 0 {
		return s.Budget
	}
	if pre, ok := sessions[sessionKey("", from)]; ok {
		return pre.Budget
	}
	return 0
}

// --- Recommendation drafting ---

type salesDraft struct {
	Pick        string `json:"pick"`
	PickShop    string `json:"pickShop"`
	Alternative string `json:"alternative"`
	AltShop     string `json:"altShop"`
	Reply       string `json:"reply"`
}

const recommendSystem = `You are the salesperson of a small shop in Ghana, putting one product in front of one buyer. She described a need in her own words: perhaps an occasion, a gift, a purpose, a tight purse. From the candidate products in the facts, and only from them, choose the ONE strongest fit as "pick" and one relevant alternative as "alternative" (leave it empty when nothing else genuinely fits). Write "reply": at most three short pieces. The first says why the pick fits HER situation (the occasion, the purpose, the budget) using the product's own description; the second gives the pick and its exact price; the third names the alternative and its price, briefly. When the facts span more than one shop, name each product's shop in the reply as you present it, so she knows where each one lives. Correct English, warm, never a title (no Sir, Madam, Ma or Boss). When a budget is among the facts, the pick must be at or under it whenever any candidate is; when no candidate fits under it, say so plainly in the reply and pick the closest above it. Prices only from the facts, exact, written as GH₵ with the figure. Answer with exactly one JSON object: {"pick": "...", "pickShop": "...", "alternative": "...", "altShop": "...", "reply": "..."}. pickShop and altShop are the shop codes from the facts; when every candidate is from one shop, still give its code.`

const objectionSystem = `You are the salesperson of a small shop in Ghana. The buyer has just said the product she was looking at is too expensive, without naming a figure she would pay. From the cheaper candidate products in the facts, and only from them, choose the ONE that best keeps what she wanted (the same kind of thing, her occasion and budget where the facts carry them) as "pick". Write "reply": at most three short pieces. The first meets her where she is, without arguing; the second offers the pick as the cheaper way to the same end, with its exact price and one line on why it still fits; the third keeps the first product open, inviting her to make an offer on it if she would rather haggle. Correct English, warm, never a title (no Sir, Madam, Ma or Boss). When a budget is among the facts and any candidate fits under it, the pick must be one of those. Prices only from the facts, exact, written as GH₵ with the figure. Answer with exactly one JSON object: {"pick": "...", "pickShop": "...", "alternative": "", "altShop": "", "reply": "..."}.`

// draftSalesPick asks the model to judge and draft, then
// verifies everything it used: both products must be among
// the candidates, and every figure in the reply must be a
// candidate price or the budget itself.
func draftSalesPick(system string, ask string, budget float64, candidates []salesCandidate) (pick, alt *salesCandidate, reply string, ok bool) {
	if !model.Enabled() || len(candidates) == 0 {
		return nil, nil, "", false
	}
	var b strings.Builder
	b.WriteString("Her words: \"" + ask + "\"\n")
	if budget > 0 {
		b.WriteString("Her budget for this conversation: " + money.GHS(budget) + ".\n")
	}
	b.WriteString("Candidate products:\n")
	for _, c := range candidates {
		b.WriteString("SHOP-" + c.Shop.ShopCode + ": " + c.label() + "\n")
	}
	// The pick and its words are a drafting job, separately
	// budgeted from the turn's one understanding call.
	out := model.Chat([]model.Message{{Role: "user", Content: b.String()}}, model.Options{System: &system, Kind: model.KindDraft})
	if out == nil {
		return nil, nil, "", false
	}
	text := *out
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, nil, "", false
	}
	var d salesDraft
	if err := json.Unmarshal([]byte(text[start:end+1]), &d); err != nil {
		return nil, nil, "", false
	}
	find := func(name, shopCode string) *salesCandidate {
		// The facts render a code as SHOP-<CODE>, and the
		// thinking may hand it back in that dress: the code
		// itself is what must match.
		code := strings.TrimSpace(shopCode)
		if len(code) > 5 && strings.EqualFold(code[:5], "SHOP-") {
			code = code[5:]
		}
		for i := range candidates {
			if strings.EqualFold(candidates[i].Product.Name, strings.TrimSpace(name)) &&
				(code == "" || strings.EqualFold(candidates[i].Shop.ShopCode, code)) {
				return &candidates[i]
			}
		}
		return nil
	}
	pickC := find(d.Pick, d.PickShop)
	if pickC == nil {
		return nil, nil, "", false
	}
	var altC *salesCandidate
	if strings.TrimSpace(d.Alternative) != "" {
		altC = find(d.Alternative, d.AltShop)
		if altC == nil || altC.Product.ID == pickC.Product.ID {
			return nil, nil, "", false
		}
	}
	reply = strings.TrimSpace(d.Reply)
	allowed := []float64{pickC.Product.ListPrice}
	if altC != nil {
		allowed = append(allowed, altC.Product.ListPrice)
	}
	if budget > 0 {
		allowed = append(allowed, budget)
	}
	if reply == "" || !voiceClean(reply) || !figuresClean(reply, allowed...) {
		return nil, nil, "", false
	}
	return pickC, altC, reply, true
}

// codeSalesPick is the factual floor under a recommendation
// when no draft can be verified: price alone decides. Under
// a budget, the strongest reach is the dearest candidate
// that still fits it; with nothing under it, the cheapest
// candidate there is. The alternative is the next distinct
// candidate the same rule leaves.
func codeSalesPick(candidates []salesCandidate, budget float64) (pick, alt *salesCandidate) {
	if len(candidates) == 0 {
		return nil, nil
	}
	sorted := append([]salesCandidate{}, candidates...)
	// Dearest first, so the first fit under a budget is the
	// strongest reach and the last is the cheapest overall.
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Product.ListPrice > sorted[i].Product.ListPrice {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	pickIdx := len(sorted) - 1 // cheapest overall
	if budget > 0 {
		for i := range sorted {
			if sorted[i].Product.ListPrice <= budget {
				pickIdx = i // dearest under the budget
				break
			}
		}
	}
	p := sorted[pickIdx]
	pick = &p
	// The alternative is the candidate closest in price to
	// the pick, kept inside her budget when anything in it
	// qualifies: a relevant alternative, not the far end of
	// the shelf.
	var pool []salesCandidate
	for i := range sorted {
		if i != pickIdx {
			pool = append(pool, sorted[i])
		}
	}
	if budget > 0 {
		var under []salesCandidate
		for _, c := range pool {
			if c.Product.ListPrice <= budget {
				under = append(under, c)
			}
		}
		if len(under) > 0 {
			pool = under
		}
	}
	bestDiff := -1.0
	for i := range pool {
		diff := pool[i].Product.ListPrice - pick.Product.ListPrice
		if diff < 0 {
			diff = -diff
		}
		if bestDiff < 0 || diff < bestDiff {
			bestDiff = diff
			a := pool[i]
			alt = &a
		}
	}
	return pick, alt
}

// dearestFit is the objection floor: she wanted the thing
// she balked at, just cheaper, so the floor offers the
// dearest candidate that still fits her budget, or the
// dearest there is when no budget binds or none fit it.
func dearestFit(candidates []salesCandidate, budget float64) *salesCandidate {
	var best *salesCandidate
	for i := range candidates {
		c := &candidates[i]
		if budget > 0 && c.Product.ListPrice > budget {
			continue
		}
		if best == nil || c.Product.ListPrice > best.Product.ListPrice {
			best = c
		}
	}
	if best == nil && budget > 0 {
		// Nothing fits the budget: the dearest candidate is
		// still the closest to what she wanted.
		for i := range candidates {
			c := &candidates[i]
			if best == nil || c.Product.ListPrice > best.Product.ListPrice {
				best = c
			}
		}
	}
	return best
}

// factualRecommend composes the floor recommendation from
// facts alone: the pick, its price, the alternative, and,
// when a budget is in force and nothing fits it, that said
// plainly.
func factualRecommend(pick, alt *salesCandidate, budget float64) string {
	var parts []string
	if budget > 0 && pick.Product.ListPrice > budget {
		parts = append(parts, "Nothing here lands inside "+money.GHS(budget)+". The closest is the "+pick.Product.Name+" at "+money.GHS(pick.Product.ListPrice)+".")
	} else {
		parts = append(parts, "The strongest fit here is the "+pick.Product.Name+" at "+money.GHS(pick.Product.ListPrice)+".")
	}
	if alt != nil {
		parts = append(parts, "There is also the "+alt.Product.Name+" at "+money.GHS(alt.Product.ListPrice)+".")
	}
	return strings.Join(parts, " ")
}

// recommendInShop serves a judged ask inside one shop: the
// shelf is the candidate set, the strongest fit is put in
// front of her with its photo when it has one, and it
// becomes the thing she is looking at.
func recommendInShop(from string, s *Session, merchant *catalogue.Merchant, shopID, ask string) bool {
	products := catalogue.ListProducts(shopID)
	if len(products) == 0 {
		return false
	}
	candidates := make([]salesCandidate, 0, len(products))
	for _, p := range products {
		pp := p
		candidates = append(candidates, salesCandidate{Shop: merchant, Product: &pp})
	}
	budget := budgetOf(from, s)
	pick, alt, reply, ok := draftSalesPick(recommendSystem, ask, budget, candidates)
	if !ok {
		pick, alt = codeSalesPick(candidates, budget)
		if pick == nil {
			return false
		}
		reply = factualRecommend(pick, alt, budget)
	}
	sendProductPhotos(from, pick.Product)
	whatsapp.SendText(from, reply)
	s.LastViewed = pick.Product
	sessions[sessionKey(shopID, from)] = s
	return true
}

// offerCheaperAlternative serves a balk at a price: a
// cheaper suitable product is put in front of her first,
// and only when the shelf holds nothing cheaper does the
// haggle invitation stand alone.
func offerCheaperAlternative(from string, s *Session, merchant *catalogue.Merchant, shopID string, current *catalogue.Product, ask string) bool {
	if current == nil {
		return false
	}
	var candidates []salesCandidate
	for _, p := range catalogue.ListProducts(shopID) {
		if p.ID == current.ID || p.ListPrice >= current.ListPrice {
			continue
		}
		pp := p
		candidates = append(candidates, salesCandidate{Shop: merchant, Product: &pp})
	}
	if len(candidates) == 0 {
		whatsapp.SendText(from, "That is the lowest-priced one of its kind here. If you want the "+current.Name+", make me an offer and let us see.")
		return true
	}
	budget := budgetOf(from, s)
	pick, _, reply, ok := draftSalesPick(objectionSystem, ask, budget, candidates)
	if !ok {
		pick = dearestFit(candidates, budget)
		if pick == nil {
			return false
		}
		reply = "I hear you. A cheaper way to the same end: the " + pick.Product.Name + " at " + money.GHS(pick.Product.ListPrice) + ". Or make me an offer on the " + current.Name + " and let us see."
	}
	sendProductPhotos(from, pick.Product)
	whatsapp.SendText(from, reply)
	s.LastViewed = pick.Product
	sessions[sessionKey(shopID, from)] = s
	return true
}

var tooExpensiveRe = regexp.MustCompile(`(?i)\b(too (expensive|much|dear|high)|still expensive|that'?s (too much|a lot)|price is (too )?(high|much)|i can'?t afford|can'?t afford (it|that))\b`)
