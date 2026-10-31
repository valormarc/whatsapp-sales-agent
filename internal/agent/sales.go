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
	"strings"
	"unicode"

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

// The captures carry the optional k: "my budget is 5k"
// is a budget of GH₵5,000, and a reader that takes the
// figure at face value works every sales pick to GH₵5
// (the fifth audit). parseFigure applies the thousand.
var budgetRe = regexp.MustCompile(`(?i)\b(?:budget|around|under|below|up to|not more than|max(?:imum)?|spend)\b[^0-9]{0,14}(\d[\d,]*(?:\.\d+)?[kK]?\b)|(\d[\d,]*(?:\.\d+)?[kK]?\b)\s*(?:is|na)?\s*(?:my|the)?\s*budget\b`)

// budgetFigureIn reports whether a stated budget figure
// appears in her own words, the verification every carried
// fact must pass before the platform holds it.
func budgetFigureIn(text string, budget float64) bool {
	for _, tok := range figureTokenRe.FindAllString(text, -1) {
		if v, err := parseFigure(tok); err == nil && v == budget {
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
			if v, err := parseFigure(g); err == nil && v > 0 {
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
	if pre, ok := sessions.lookup(sessionKey("", from)); ok && pre != s {
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
	if pre, ok := sessions.lookup(sessionKey("", from)); ok {
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
// containsNameBounded reports whether name appears in
// text as itself — bounded on both sides by a non-word
// character or the string's edge — not as a substring of
// a longer word or a longer name.
func containsNameBounded(text, name string) bool {
	if name == "" {
		return false
	}
	isWord := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	}
	for start := 0; ; {
		i := strings.Index(text[start:], name)
		if i < 0 {
			return false
		}
		i += start
		beforeOK := i == 0 || !isWord(rune(text[i-1]))
		end := i + len(name)
		afterOK := end >= len(text) || !isWord(rune(text[end]))
		if beforeOK && afterOK {
			return true
		}
		start = i + 1
		if start >= len(text) {
			return false
		}
	}
}

func draftSalesPick(from, system string, ask string, budget float64, candidates []salesCandidate) (pick, alt *salesCandidate, reply string, ok bool) {
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
	out := model.Chat([]model.Message{{Role: "user", Content: b.String()}}, model.Options{System: &system, Kind: model.KindDraft, Turn: turnTokenFor(from)})
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
	// The budget rule is a floor, not a prompt hope (the
	// fifth audit: it lived only in the system text, and
	// the draft was never checked against it, while both
	// code floors enforce it deterministically). When
	// any candidate fits her budget, neither the pick nor
	// the alternative may stand over it.
	if budget > 0 {
		anyFits := false
		for _, c := range candidates {
			if c.Product.ListPrice <= budget {
				anyFits = true
				break
			}
		}
		if anyFits && (pickC.Product.ListPrice > budget || (altC != nil && altC.Product.ListPrice > budget)) {
			return nil, nil, "", false
		}
	}
	reply = strings.TrimSpace(d.Reply)
	// The prose is bound to the resolution EXCLUSIVELY
	// (the fifth audit bound it by presence; the sixth
	// found presence alone lets the words name products
	// and shops outside the resolved set, misattribute
	// the alternative to the wrong shop, and pass on
	// substring accidents). The reply must name the
	// resolved pick — and the alternative, when one
	// resolved — on word boundaries; in a multi-shop set
	// it must place EACH of them at its own shop, by name
	// or by code; and it must name no other product or
	// shop the platform can enumerate.
	lowerReply := strings.ToLower(reply)
	if !containsNameBounded(lowerReply, strings.ToLower(pickC.Product.Name)) {
		return nil, nil, "", false
	}
	if altC != nil && !containsNameBounded(lowerReply, strings.ToLower(altC.Product.Name)) {
		return nil, nil, "", false
	}
	shops := map[string]bool{}
	for _, c := range candidates {
		shops[c.Shop.ShopCode] = true
	}
	if len(shops) > 1 {
		places := func(c *salesCandidate) bool {
			return containsNameBounded(lowerReply, strings.ToLower(c.Shop.Name)) ||
				strings.Contains(lowerReply, strings.ToLower("SHOP-"+c.Shop.ShopCode))
		}
		if !places(pickC) {
			return nil, nil, "", false
		}
		if altC != nil && !places(altC) {
			return nil, nil, "", false
		}
	}
	allowedProducts := map[string]bool{strings.ToLower(pickC.Product.Name): true}
	allowedShops := map[string]bool{strings.ToLower(pickC.Shop.Name): true, strings.ToLower("shop-" + pickC.Shop.ShopCode): true}
	if altC != nil {
		allowedProducts[strings.ToLower(altC.Product.Name)] = true
		allowedShops[strings.ToLower(altC.Shop.Name)] = true
		allowedShops[strings.ToLower("shop-"+altC.Shop.ShopCode)] = true
	}
	for _, m := range catalogue.ListMerchants() {
		if !allowedShops[strings.ToLower(m.Name)] && len(m.Name) >= 3 && containsNameBounded(lowerReply, strings.ToLower(m.Name)) {
			return nil, nil, "", false
		}
		if !allowedShops[strings.ToLower("shop-"+m.ShopCode)] && strings.Contains(lowerReply, strings.ToLower("SHOP-"+m.ShopCode)) {
			return nil, nil, "", false
		}
		for _, p := range catalogue.ListProducts(m.ID) {
			if !allowedProducts[strings.ToLower(p.Name)] && len(p.Name) >= 3 && containsNameBounded(lowerReply, strings.ToLower(p.Name)) {
				return nil, nil, "", false
			}
		}
	}
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
	if budget > 0 && pick.Product.ListPrice <= budget {
		// The pick fits her budget, so the alternative is
		// drawn from inside it UNCONDITIONALLY (the sixth
		// audit): the old conditional pool silently
		// reverted to the unfiltered shelf when the pick
		// was the only fit, presenting an over-budget
		// alternative at budget with no flag. An empty
		// pool simply means no alternative. When nothing
		// fits at all, the pick is the closest overall
		// and the pool stays whole — that case carries
		// its own disclaimer in the presenter.
		var under []salesCandidate
		for _, c := range pool {
			if c.Product.ListPrice <= budget {
				under = append(under, c)
			}
		}
		pool = under
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
	// The candidate set is the stocked shelf only (the
	// seventh audit): a sold-out product is never the
	// recommended fit or the named alternative.
	products := stockedProducts(shopID)
	if len(products) == 0 {
		return false
	}
	candidates := make([]salesCandidate, 0, len(products))
	for _, p := range products {
		pp := p
		candidates = append(candidates, salesCandidate{Shop: merchant, Product: &pp})
	}
	budget := budgetOf(from, s)
	pick, alt, reply, ok := draftSalesPick(from, recommendSystem, ask, budget, candidates)
	if !ok {
		pick, alt = codeSalesPick(candidates, budget)
		if pick == nil {
			return false
		}
		reply = factualRecommend(pick, alt, budget)
	}
	sendProductPhotos(from, pick.Shop.ID, pick.Product)
	whatsapp.SendText(from, reply)
	s.LastViewed = pick.Product
	sessions.put(sessionKey(shopID, from), s)
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
		if p.ID == current.ID || p.ListPrice >= current.ListPrice || p.AvailableUnits() <= 0 {
			// Sold-out goods are not a cheaper alternative
			// (the seventh audit): when only they are
			// cheaper, the road falls through to the
			// haggle invitation on the current product.
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
	pick, _, reply, ok := draftSalesPick(from, objectionSystem, ask, budget, candidates)
	if !ok {
		pick = dearestFit(candidates, budget)
		if pick == nil {
			return false
		}
		reply = "I hear you. A cheaper way to the same end: the " + pick.Product.Name + " at " + money.GHS(pick.Product.ListPrice) + ". Or make me an offer on the " + current.Name + " and let us see."
	}
	sendProductPhotos(from, pick.Shop.ID, pick.Product)
	whatsapp.SendText(from, reply)
	s.LastViewed = pick.Product
	sessions.put(sessionKey(shopID, from), s)
	return true
}

var tooExpensiveRe = regexp.MustCompile(`(?i)\b(too (expensive|much|dear|high)|still expensive|that'?s (too much|a lot)|price is (too )?(high|much)|i can'?t afford|can'?t afford (it|that))\b`)
