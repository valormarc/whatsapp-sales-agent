package agent

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// Discovery: a buyer who arrives on the shared number with
// no shop of her own. The old answer was the same ask for a
// shop code, repeated at everything she said, which trapped
// her. She is now understood first, the way every other
// person the agent serves is: the model reads her words
// against a directory of every shop's public shelf (names,
// goods, list prices, nothing private), matches by meaning,
// and she is helped to a shop instead of blocked for want of
// a code. Picking a shop or a product binds her to that shop,
// and from there the ordinary one-shop flow owns her, so a
// cross-shop conversation can never sell her one shop's
// goods under another's name.

// discoveryCharter is the thinking's brief for the unbound
// buyer. The format is the shape; the words stay the model's.
const discoveryCharter = `You are the thinking of a sales agent who sells for many independent shops on one shared WhatsApp number. This buyer has not chosen a shop yet. The facts list every shop, its code, and its goods with list prices; those are the only goods and prices that exist for you.

Reason in this order before you decide: first, what she is referring to (including any results she was just shown and anything she said earlier in this chat); second, what she wants to happen; third, which of the facts answer it. When the facts say she is replying to a quoted message, that message — and who sent it, her or the shop — is what she is referring to: a reply that accepts, picks or asks about the quoted product is a pick of that product's shop, and a reply correcting her own quoted words is read against those words.

Read her message and decide:
- intent "shop": she names a shop (by name or code), or picks a shop or a product from results she was shown. Give "shop" as the shop code, and when she picked a product give it in matches too.
- intent "search": she wants a kind of thing, whether or not she has a shop in mind, and whether she asks it as a question ("which shop has laptops?"), a need ("I am looking for a laptop"), or a statement. Give "item" as the thing in her own words, and "matches": every product in the facts that genuinely fits it BY MEANING, not by wording. A laptop is a notebook computer under any brand name; slides and sandals are the same family; a phone is not a laptop. Name each match by its shop code and its exact product name from the facts. Only products in the facts, never an invention. When she asks by price (the cheapest, the most affordable), she still means the thing itself; order your matches by list price, cheapest first. When her ask is a need described by occasion, purpose or purse rather than a named kind of thing (a gift for someone, something for an event, something nice within what she can spend), set "recommend" true and order the matches with the strongest fit first: the agent will present the first as its recommendation, with a reason. When she states what she can spend, in this message or as her settled budget in the facts, give that figure in "budget" exactly as stated; otherwise leave it 0.
- intent "picture": she is asking to SEE a picture of a thing: "show me picture", "send pic", and also "I am not buying without pic", which means the same thing: the picture comes before any buying. Give "item" as the thing she wants to see and "matches" for it exactly as a search. A price wish inside the picture ask ("send pic of the cheapest heel") orders those matches by list price, cheapest first, so the first match is the one she means. A pick that carries a picture ask in the same breath is this intent, not a pick: "the strappy ones, can I see a picture?" said about a shown result is her choosing that result BY asking to see it — the picture is what she wants, so answer the picture ask rather than only landing her in the shop.
- intent "cheaper": she is balking at the price of what she was just shown or recommended: too expensive, still expensive, more than she can do, without naming a figure she would pay. Give "matches": suitable products that cost LESS than the thing she is balking at, across the shops, strongest fit first; her budget in the facts is her ceiling.
- intent "order_received": she is telling you that an order she already PAID for has arrived and she has the goods now. The facts name her paid orders when she has any; give "shop" as that order's shop code. This is about an order she already made, never a new purchase.
- intent "order_missing": she is telling you that an order she already paid for has NOT arrived: it never came, it is lost, she is still waiting and it is overdue. The facts name her paid orders when she has any; give "shop" as that order's shop code. Merely asking where an order stands is a search of another kind, not this.
- intent "browse": she asks what shops or goods there are.
- intent "no_shop": she says she has no particular shop, without naming a thing she wants. If she names a thing as well, it is a search.
- intent "offtopic": her message is not about buying or shops at all.
- intent "unclear": none of the above fits.

Judge the shape of her ask as well: when she describes a situation rather than naming a kind of thing — a gift for someone, something for an occasion, a purpose she names, what she can spend — she is asking the shops to choose for her, and a bare list is not an answer to that. For a search in that shape, set "recommend" true and put the strongest fit first among the matches.

"reply" is your answer to her in your own words for no_shop, browse, offtopic and unclear: short, warm, correct English, never a title (no Sir, Madam, Ma or Boss), steering her toward naming a shop or a thing she wants. For search and shop leave reply empty; the agent lists the verified results itself. Never invent a product, a shop or a price, and never repeat a greeting she has already had. When results are standing and her message neither picks one nor starts something new — a greeting, small talk — the pick is still open: greet her back in one short line and hold the question up again, naming the shown results and asking which one she wants. A greeting never closes a question that is still open.

Reply with exactly one JSON object:
{"intent": "search", "shop": "", "item": "", "matches": [{"shop": "LUXE", "product": "HP EliteBook 1030 G8 x360"}], "reply": ""}`

// ShownProduct is one result discovery presented to an
// unbound buyer: the shop it belongs to and the product id.
type ShownProduct struct {
	ShopID    string
	ProductID string
}

type discoveryMatch struct {
	Shop    string `json:"shop"`
	Product string `json:"product"`
}

type discoveryUnderstanding struct {
	Intent    string           `json:"intent"`
	Shop      string           `json:"shop"`
	Item      string           `json:"item"`
	Matches   []discoveryMatch `json:"matches"`
	Reply     string           `json:"reply"`
	Budget    float64          `json:"budget"`
	Recommend bool             `json:"recommend"`
}

// stockedProducts is a shop's shelf as discovery may
// speak of it: only what can actually be bought (the
// sixth audit). A sold-out product offered as available
// bound the buyer to its shop and let the sale run until
// stock-taking refused it; the availability rule lives
// here, at the sources, so no presenter can resurrect
// what the shelf does not have.
func stockedProducts(shopID string) []catalogue.Product {
	var out []catalogue.Product
	for _, p := range catalogue.ListProducts(shopID) {
		if p.AvailableUnits() > 0 {
			out = append(out, p)
		}
	}
	return out
}

// discoveryDirectory renders the public facts: every shop,
// its code, and its goods at list price.
func discoveryDirectory() string {
	var b strings.Builder
	for _, m := range catalogue.ListMerchants() {
		products := stockedProducts(m.ID)
		if len(products) == 0 {
			continue
		}
		b.WriteString("SHOP-" + m.ShopCode + " (" + m.Name + "): ")
		parts := make([]string, 0, len(products))
		for _, p := range products {
			parts = append(parts, p.Name+" "+money.GHS(p.ListPrice))
		}
		b.WriteString(strings.Join(parts, "; ") + "\n")
	}
	return b.String()
}

// The buyer whose discovery turn is being understood is
// the function's own from parameter, and her shown
// results are looked up under it directly. A package
// variable carried this between turns once, on a claim
// that turns are serialised; they are not — different
// people's turns run in parallel by design — and one
// buyer's shown results could enter another's context
// (the fifth audit).

func understandDiscovery(from, text string) *discoveryUnderstanding {
	sys := discoveryCharter
	facts := "The shops and their goods:\n" + discoveryDirectory()
	if pre, ok := sessions.lookup(sessionKey("", from)); ok && len(pre.DiscoveryShown) > 0 {
		var shown []string
		for _, sp := range pre.DiscoveryShown {
			if p := catalogue.FindByID(sp.ShopID, sp.ProductID); p != nil {
				if shop := catalogue.GetMerchant(sp.ShopID); shop != nil {
					shown = append(shown, shop.ShopCode+": "+p.Name)
				}
			}
		}
		if len(shown) > 0 {
			facts += "\nYou just showed her these results: " + strings.Join(shown, "; ") + ". If she now names one of them, or says the first or the second, that is a pick: intent shop. If only one result was shown and she points at it without naming it — \"this\", \"that one\", \"I will take it\" — that is a pick of that one result: intent shop, with it in matches. If she asks to see a picture, in any words, that is intent picture, with the shown result she means in matches. When she does both in one message — names or describes one of them AND asks for its picture, like \"the strappy ones, can I see a picture?\" — that is intent picture with that result in matches."
		}
	}
	if pre, ok := sessions.lookup(sessionKey("", from)); ok {
		if w := recentSpeakerWords(pre, 4); w != "" {
			facts += "\nWhat she said earlier in this chat, oldest first: " + w + "."
		}
		if pre.Budget > 0 {
			facts += "\nHer budget for this conversation: " + money.GHS(pre.Budget) + "."
		}
	}
	if qf := quoteFactLine(from); qf != "" {
		facts += "\n" + qf
	}
	// Her paid orders are facts too: a buyer whose session is
	// gone (the agent restarted, days passed) still owns her
	// orders, and her word about one of them (it arrived, it
	// never came) is served from the book, not treated as a
	// new arrival looking for a shop.
	for _, o := range orders.Default.All() {
		if o.From != from || o.Status != "paid" {
			continue
		}
		shop := catalogue.GetMerchant(o.MerchantID)
		if shop == nil {
			continue
		}
		state := "not yet marked sent"
		switch o.DeliveryState {
		case "sent":
			state = "marked sent by the seller"
		case "received":
			state = "already confirmed received by her"
		case "missing":
			state = "already reported by her as not arrived"
		}
		facts += "\nShe has a paid order at " + shop.Name + " (shop code " + shop.ShopCode + "): " + o.Reference + ", the " + o.ProductName + ", " + state + "."
	}
	reply := model.Chat([]model.Message{
		{Role: "user", Content: facts + "\nHer message: \"" + text + "\""},
	}, model.Options{System: &sys, Turn: turnTokenFor(from)})
	if reply == nil {
		return nil
	}
	s := *reply
	open := strings.Index(s, "{")
	closeIdx := strings.LastIndex(s, "}")
	if open == -1 || closeIdx <= open {
		return nil
	}
	var u discoveryUnderstanding
	if err := json.Unmarshal([]byte(s[open:closeIdx+1]), &u); err != nil {
		return nil
	}
	return &u
}

// resolveDiscoveryShop turns the thinking's shop reference
// (a code or a name) into a real shop, or nil: the binding
// is never a guess the code has not verified.
func resolveDiscoveryShop(ref string) *catalogue.Merchant {
	ref = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(ref), "SHOP-"))
	if ref == "" {
		return nil
	}
	if shop := catalogue.MerchantByShopCode(strings.ToLower(ref)); shop != nil {
		return shop
	}
	// A name resolves only when it names ONE shop. The
	// merchant door learned this in the second audit; the
	// third found discovery still returning the first of
	// two same-named shops in catalogue order, binding the
	// buyer to a shop she did not mean. An ambiguous name
	// resolves to nothing here, and the caller that binds
	// on it says so and asks for the shop's code.
	var found *catalogue.Merchant
	for _, m := range catalogue.ListMerchants() {
		if strings.EqualFold(m.Name, ref) || strings.Contains(strings.ToLower(m.Name), strings.ToLower(ref)) {
			if found != nil {
				return nil
			}
			shop := m
			found = &shop
		}
	}
	return found
}

// discoveryNameAmbiguous reports whether a shop reference
// matches more than one shop by name — the case the
// binding caller answers with a request for the code.
func discoveryNameAmbiguous(ref string) bool {
	ref = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(ref), "SHOP-"))
	if ref == "" || catalogue.MerchantByShopCode(strings.ToLower(ref)) != nil {
		return false
	}
	n := 0
	for _, m := range catalogue.ListMerchants() {
		if strings.EqualFold(m.Name, ref) || strings.Contains(strings.ToLower(m.Name), strings.ToLower(ref)) {
			n++
		}
	}
	return n > 1
}

// verifiedMatches keeps only the thinking's matches that
// truly exist, at the catalogue's own prices.
func verifiedMatches(u *discoveryUnderstanding) []struct {
	Shop    catalogue.Merchant
	Product catalogue.Product
} {
	var out []struct {
		Shop    catalogue.Merchant
		Product catalogue.Product
	}
	seen := map[string]bool{}
	for _, match := range u.Matches {
		shop := resolveDiscoveryShop(match.Shop)
		if shop == nil {
			continue
		}
		p := catalogue.FindByName(shop.ID, match.Product)
		if p == nil || p.AvailableUnits() <= 0 || seen[shop.ID+"|"+p.ID] {
			continue
		}
		seen[shop.ID+"|"+p.ID] = true
		out = append(out, struct {
			Shop    catalogue.Merchant
			Product catalogue.Product
		}{Shop: *shop, Product: *p})
	}
	return out
}

// bindDiscoveryShop puts the buyer inside the shop she
// picked, with the ordinary entry greeting, and focuses the
// product she picked when there is exactly one in that shop.
func bindDiscoveryShop(from string, shop *catalogue.Merchant, focus *catalogue.Product) {
	bindings.put(from, shop.ID)
	sendPieces(from, welcomeFor(shop, from), productListText(shop.ID))
	if focus != nil {
		key := sessionKey(shop.ID, from)
		s, ok := sessions.lookup(key)
		if !ok {
			s = &Session{Stage: "browsing", MerchantID: shop.ID}
			sessions.put(key, s)
		}
		if pre, ok := sessions.lookup(sessionKey("", from)); ok && pre.Budget > 0 {
			s.Budget = pre.Budget
		}
		s.Product = focus
		s.Ask = focus.ListPrice
		s.LastViewed = focus
		sendProductView(from, focus, " Want to make an offer?")
	}
}

// presentSingleDiscoveryMatch answers a search that found
// exactly one product in any shop: there is no choice to
// offer her, so asking "which one" would be a question with
// one possible answer. The product itself is put in front of
// her instead: which shop has it, what it is, its photo when
// it has one. She stays unbound, exactly as with a list of
// results: the shown result is remembered, and her pick of
// it, by name or by an offer that names it, is what takes
// her into the shop. Nothing is sold before that pick, as
// discovery has always held.
func presentSingleDiscoveryMatch(from string, shop *catalogue.Merchant, p *catalogue.Product) {
	whatsapp.SendText(from, shop.ShopCode+" has it:")
	sendProductPhotos(from, shop.ID, p)
	sendProductView(from, p, " Want to make an offer?")
	pre, ok := sessions.lookup(sessionKey("", from))
	if !ok {
		pre = &Session{Stage: "unbound"}
		sessions.put(sessionKey("", from), pre)
	}
	pre.DiscoveryShown = []ShownProduct{{ShopID: shop.ID, ProductID: p.ID}}
}

// presentDiscoveryRecommendation answers a judged ask
// across shops: the strongest verified match is put in
// front of her as a recommendation, with its photo when it
// has one and one alternative named. She stays unbound,
// exactly as with a list of results: every match is
// recorded as shown, and her pick of one is what takes her
// into its shop.
func presentDiscoveryRecommendation(from, ask string, matches []struct {
	Shop    catalogue.Merchant
	Product catalogue.Product
}) {
	candidates := make([]salesCandidate, 0, len(matches))
	for i := range matches {
		shop := matches[i].Shop
		p := matches[i].Product
		candidates = append(candidates, salesCandidate{Shop: &shop, Product: &p})
	}
	pre, ok := sessions.lookup(sessionKey("", from))
	if !ok || pre == nil {
		pre = &Session{Stage: "unbound"}
		sessions.put(sessionKey("", from), pre)
	}
	budget := pre.Budget
	pick, alt, reply, drafted := draftSalesPick(from, recommendSystem, ask, budget, candidates)
	if !drafted {
		pick, alt = codeSalesPick(candidates, budget)
		if pick == nil {
			return
		}
		reply = "At " + pick.Shop.Name + ", the strongest fit is the " + pick.Product.Name + " at " + money.GHS(pick.Product.ListPrice) + "."
		if budget > 0 && pick.Product.ListPrice > budget {
			reply = "Nothing in any shop lands inside " + money.GHS(budget) + ". The closest is the " + pick.Product.Name + " at " + money.GHS(pick.Product.ListPrice) + ", at " + pick.Shop.Name + "."
		}
		if alt != nil {
			reply += " There is also the " + alt.Product.Name + " at " + money.GHS(alt.Product.ListPrice) + ", at " + alt.Shop.Name + "."
		}
	}
	sendProductPhotos(from, pick.Shop.ID, pick.Product)
	whatsapp.SendText(from, reply)
	pre.DiscoveryShown = pre.DiscoveryShown[:0]
	for _, v := range matches {
		pre.DiscoveryShown = append(pre.DiscoveryShown, ShownProduct{ShopID: v.Shop.ID, ProductID: v.Product.ID})
	}
}

// discoveryReplyOr answers with the model's drafted words
// when they are clean and figure-free, and with the given
// factual line otherwise.
func discoveryReplyOr(u *discoveryUnderstanding, fallback string) string {
	if u != nil {
		if draft := strings.TrimSpace(u.Reply); draft != "" && voiceClean(draft) && figuresClean(draft) {
			return draft
		}
	}
	return fallback
}

// handleUnboundBuyer serves a buyer with no resolved shop.
// It never sends the welcome wall twice: the greeting and
// the door belong to her first contact, and everything after
// is understood and answered.
func handleUnboundBuyer(from, text string) {
	pre, ok := sessions.lookup(sessionKey("", from))
	if !ok || pre == nil {
		pre = &Session{Stage: "unbound"}
		sessions.put(sessionKey("", from), pre)
	}
	noteSpeakerWords(pre, text)
	if b := netBudget(strings.ToLower(text)); b > 0 {
		pre.Budget = b
	}
	// The thinking leads, here as everywhere: a pick that
	// carries a picture ask in the same breath is a picture
	// ask, and only the thinking can tell the two apart. The
	// word net below speaks only when no model can be read,
	// the standing rule for every fallback in the agent.
	if u := understandDiscovery(from, text); u != nil {
		if u.Budget > 0 && budgetFigureIn(text, u.Budget) {
			pre.Budget = u.Budget
		}
		if actOnDiscovery(from, strings.ToLower(text), u) {
			return
		}
	}
	if quotedPick(from, text) {
		return
	}
	if pickFromShown(from, text) {
		return
	}
	// A balk at a shown price, on plain words: the cheapest
	// thing she was shown is offered as the alternative.
	// The lead of the shown set (the pick she is balking
	// at) is never offered back to her as its own cheaper
	// alternative, and nothing dearer than it is either.
	if tooExpensiveRe.MatchString(strings.ToLower(text)) && len(pre.DiscoveryShown) > 0 {
		var cheapest *catalogue.Product
		var cheapestShop *catalogue.Merchant
		leadPrice := -1.0
		for i, sp := range pre.DiscoveryShown {
			p := catalogue.FindByID(sp.ShopID, sp.ProductID)
			if p == nil || (i > 0 && p.AvailableUnits() <= 0) {
				// A sold-out shown product is never the
				// cheaper alternative (the seventh audit);
				// the lead (i == 0) still anchors the
				// price comparison.
				continue
			}
			if i == 0 {
				leadPrice = p.ListPrice
				continue
			}
			if leadPrice >= 0 && p.ListPrice >= leadPrice {
				continue
			}
			if cheapest == nil || p.ListPrice < cheapest.ListPrice {
				cheapest, cheapestShop = p, catalogue.GetMerchant(sp.ShopID)
			}
		}
		if cheapest != nil && cheapestShop != nil {
			sendProductPhotos(from, cheapestShop.ID, cheapest)
			whatsapp.SendText(from, "A cheaper one: the "+cheapest.Name+" at "+money.GHS(cheapest.ListPrice)+", at "+cheapestShop.Name+".")
			return
		}
	}
	discoveryNet(from, text)
}

// quotedPick is the no-model net for a quoted reply from an
// unbound buyer: when her message quotes a product's bubble
// and her words pick it, accept it, offer on it or ask for
// its picture — the shapes a pick takes — she is taken to
// that product's shop, the way a pick from shown results
// takes her. A quote she merely mentions, in a question or
// a sentence of her own, is not a pick and falls through.
func quotedPick(from, text string) bool {
	q := turnQuoteOf(from)
	if q == nil || !q.Known || q.Product == nil || q.ProductShop == nil {
		return false
	}
	p := catalogue.FindByID(q.ProductShop.ID, q.Product.ID)
	if p == nil {
		return false
	}
	if p.AvailableUnits() <= 0 {
		// The quoted bubble is a stale reference too (the
		// seventh audit): it is not bound, priced or
		// pictured once the shelf no longer holds it.
		whatsapp.SendText(from, "The "+p.Name+" has sold out since that message. Tell me what else you are looking for and I will search again.")
		return true
	}
	lower := strings.ToLower(text)
	shop := q.ProductShop
	switch {
	case picRe.MatchString(lower):
		bindings.put(from, shop.ID)
		sessions.del(sessionKey("", from))
		sessions.put(sessionKey(shop.ID, from), &Session{Stage: "browsing", MerchantID: shop.ID, LastViewed: p})
		answerPictureAsk(from, shop, p)
		return true
	case isBareAck(lower) || parseOffer(lower) != nil || buyIntentRe.MatchString(lower):
		bindDiscoveryShop(from, shop, p)
		return true
	}
	return false
}

// deliveryReportTargetAnywhere resolves the order a
// discovery-lane delivery report means, from the buyer's
// own words, across every shop — before any shop is
// pinned (the fifth audit: the pin came first, from the
// model's hint or the book's order, and her words were
// never consulted, so a cross-shop report acted on the
// wrong shop's order). A stated reference binds first:
// hers and paid, it binds its shop; stated and not hers,
// the report is refused plainly rather than re-aimed at
// a guess. A named product binds the same way the
// in-shop road binds it — one match binds, several are
// asked by reference. Only a report naming nothing
// falls through to the hint order below.
func deliveryReportTargetAnywhere(from, msg string) (target *orders.Order, handled bool) {
	if m := orderRefRe.FindStringSubmatch(msg); m != nil {
		ref := strings.ToUpper(m[1])
		if o := orders.Default.GetOrder(ref); o != nil && o.From == from && o.Status == "paid" {
			return o, true
		}
		whatsapp.SendText(from, "I could not find order "+ref+" among your paid orders, so I have not changed anything. Check the reference and tell me again.")
		return nil, true
	}
	lower := strings.ToLower(msg)
	var named []*orders.Order
	for _, o := range orders.Default.All() {
		oo := o
		if oo.From != from || oo.Status != "paid" {
			continue
		}
		if name := strings.ToLower(strings.TrimSpace(oo.ProductName)); name != "" && strings.Contains(lower, name) {
			named = append(named, &oo)
		}
	}
	if len(named) == 1 {
		return named[0], true
	}
	if len(named) > 1 {
		refs := make([]string, 0, len(named))
		for _, o := range named {
			refs = append(refs, o.Reference)
		}
		whatsapp.SendText(from, "You have more than one "+named[0].ProductName+" on its way ("+strings.Join(refs, ", ")+"). Tell me the reference of the one you mean, and I will mark that one.")
		return nil, true
	}
	return nil, false
}

// newestPaidOrderAnywhere finds the buyer's paid order
// across every shop when her words name none of them:
// the hinted shop's order when it is in transit (the one
// that can arrive, or fail to), then any order in
// transit, then the hinted shop's newest, then her
// newest paid order anywhere.
func newestPaidOrderAnywhere(from, shopHint string) *orders.Order {
	var hintShop *catalogue.Merchant
	if shopHint != "" {
		hintShop = resolveDiscoveryShop(shopHint)
	}
	var hinted, hintedSent, inTransit, any *orders.Order
	for _, o := range orders.Default.All() {
		oo := o
		if oo.From != from || oo.Status != "paid" {
			continue
		}
		any = &oo
		if oo.DeliveryState == "sent" {
			inTransit = &oo
		}
		if hintShop != nil && oo.MerchantID == hintShop.ID {
			hinted = &oo
			if oo.DeliveryState == "sent" {
				hintedSent = &oo
			}
		}
	}
	if hintedSent != nil {
		return hintedSent
	}
	if inTransit != nil {
		return inTransit
	}
	if hinted != nil {
		return hinted
	}
	return any
}

// priceWishRe reads a price wish in the buyer's own words:
// the one signal that turns a set of matches into a price
// ordering. Ordering by price is arithmetic, not judgement,
// so it is enforced here in code wherever matches are
// presented or a first match is taken: a model's listing
// order is a reading, and a reading that puts the GH₵300
// heel before the GH₵120 one she asked to see cheapest
// first is wrong however good the matches are.
var priceWishRe = regexp.MustCompile(`\bcheapest\b|\bcheaper\b|\bmost affordable\b|\blowest price\b`)

// sortByPrice orders verified matches by list price,
// cheapest first, keeping the given order inside a price.
func sortByPrice(matches []struct {
	Shop    catalogue.Merchant
	Product catalogue.Product
}) {
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Product.ListPrice < matches[j].Product.ListPrice
	})
}

func actOnDiscovery(from, lower string, u *discoveryUnderstanding) bool {
	switch u.Intent {
	case "cheaper":
		// A balk at a shown price: the cheapest suitable
		// thing across the shops goes in front of her, and
		// the shown set becomes the alternatives she was
		// offered, so her pick of one still binds her. A
		// "cheaper" answer must be cheaper than the thing
		// that led what she was shown (the pick she is
		// balking at): code enforces that relation, since a
		// reading can mistake the merely-mentioned
		// alternative for the thing being pressed on her.
		matches := verifiedMatches(u)
		if pre, ok := sessions.lookup(sessionKey("", from)); ok && len(pre.DiscoveryShown) > 0 {
			if lead := catalogue.FindByID(pre.DiscoveryShown[0].ShopID, pre.DiscoveryShown[0].ProductID); lead != nil {
				var under []struct {
					Shop    catalogue.Merchant
					Product catalogue.Product
				}
				for _, v := range matches {
					if v.Product.ListPrice < lead.ListPrice {
						under = append(under, v)
					}
				}
				matches = under
			}
		}
		if len(matches) == 0 {
			return false
		}
		cheapest := matches[0]
		for _, v := range matches[1:] {
			if v.Product.ListPrice < cheapest.Product.ListPrice {
				cheapest = v
			}
		}
		sendProductPhotos(from, cheapest.Shop.ID, &cheapest.Product)
		whatsapp.SendText(from, "A cheaper one: the "+cheapest.Product.Name+" at "+money.GHS(cheapest.Product.ListPrice)+", at "+cheapest.Shop.Name+".")
		if pre, ok := sessions.lookup(sessionKey("", from)); ok {
			pre.DiscoveryShown = pre.DiscoveryShown[:0]
			for _, v := range matches {
				pre.DiscoveryShown = append(pre.DiscoveryShown, ShownProduct{ShopID: v.Shop.ID, ProductID: v.Product.ID})
			}
		}
		return true
	case "order_received", "order_missing":
		// A delivery report from a buyer with no live shop
		// session: the order HER WORDS name pins the shop —
		// resolved across every shop first (the fifth
		// audit) — and only a report naming nothing falls
		// back to the hint order. The resolved reference
		// is what the act is handed, so the order acted on
		// is the order that pinned the shop, never a
		// re-resolution inside it.
		o, handled := deliveryReportTargetAnywhere(from, lower)
		if handled && o == nil {
			return true
		}
		if o == nil {
			o = newestPaidOrderAnywhere(from, u.Shop)
		}
		if o == nil {
			return false
		}
		shop := catalogue.GetMerchant(o.MerchantID)
		if shop == nil {
			return false
		}
		bindings.put(from, shop.ID)
		if u.Intent == "order_received" {
			return buyerConfirmReceived(from, shop, shop.ID, o.Reference)
		}
		return buyerReportMissing(from, shop, shop.ID, o.Reference)
	case "picture":
		// The picture comes first: she is put inside the
		// matched product's shop and served the picture
		// ask. Nothing is sold on this turn.
		matches := verifiedMatches(u)
		if priceWishRe.MatchString(lower) {
			sortByPrice(matches)
		}
		if len(matches) == 0 {
			// The thinking named the intent but no product:
			// the picture she means is the one she was just
			// shown, and the conversation itself settles
			// which that is.
			if pre, ok := sessions.lookup(sessionKey("", from)); ok && len(pre.DiscoveryShown) > 0 {
				sp := pre.DiscoveryShown[0]
				if shop := catalogue.GetMerchant(sp.ShopID); shop != nil {
					if p := catalogue.FindByID(sp.ShopID, sp.ProductID); p != nil && p.AvailableUnits() > 0 {
						bindings.put(from, shop.ID)
						sessions.del(sessionKey("", from))
						sessions.put(sessionKey(shop.ID, from), &Session{Stage: "browsing", MerchantID: shop.ID, LastViewed: p})
						answerPictureAsk(from, shop, p)
						return true
					}
				}
			}
			return false
		}
		v := matches[0]
		shop := v.Shop
		p := v.Product
		bindings.put(from, shop.ID)
		sessions.del(sessionKey("", from))
		sessions.put(sessionKey(shop.ID, from), &Session{Stage: "browsing", MerchantID: shop.ID, LastViewed: &p})
		answerPictureAsk(from, &shop, &p)
		return true
	case "shop":
		shop := resolveDiscoveryShop(u.Shop)
		if shop == nil {
			if discoveryNameAmbiguous(u.Shop) {
				whatsapp.SendText(from, "More than one shop here matches that name, and I do not want to take you to the wrong one. Start with the shop's own code instead — it is on the link or message the shop shared with you.")
				return true
			}
			return false
		}
		var focus *catalogue.Product
		for _, v := range verifiedMatches(u) {
			if v.Shop.ID == shop.ID {
				p := v.Product
				focus = &p
				break
			}
		}
		bindDiscoveryShop(from, shop, focus)
		return true
	case "search":
		matches := verifiedMatches(u)
		if priceWishRe.MatchString(lower) {
			sortByPrice(matches)
		}
		// A judged ask (an occasion, a gift, a purse) is
		// answered with a recommendation, not a catalogue
		// dump: the strongest fit first, with its reason
		// (see sales.go). The thinking flags the shape; a
		// budget in force is the same signal in the
		// platform's own facts, and either one is enough.
		budgetHeld := false
		if pre, ok := sessions.lookup(sessionKey("", from)); ok && pre.Budget > 0 {
			budgetHeld = true
		}
		if len(matches) > 0 && (u.Recommend || budgetHeld) {
			presentDiscoveryRecommendation(from, u.Item, matches)
			return true
		}
		if len(matches) == 0 {
			item := strings.TrimSpace(u.Item)
			if item == "" {
				item = "that"
			}
			whatsapp.SendText(from, "I do not have "+item+" in any shop just now.")
			if taste, shown := discoveryTaste(); taste != "" {
				whatsapp.SendText(from, taste)
				// The goods the taste names were shown to
				// her, so they are recorded as shown: her
				// next words may point at one, and a greeting
				// after them has something to hold up.
				if pre, ok := sessions.lookup(sessionKey("", from)); ok {
					pre.DiscoveryShown = shown
				} else {
					sessions.put(sessionKey("", from), &Session{Stage: "unbound", DiscoveryShown: shown})
				}
			}
			return true
		}
		if len(matches) == 1 {
			presentSingleDiscoveryMatch(from, &matches[0].Shop, &matches[0].Product)
			return true
		}
		var lines []string
		byShop := map[string][]string{}
		var order []string
		for _, v := range matches {
			if _, ok := byShop[v.Shop.ShopCode]; !ok {
				order = append(order, v.Shop.ShopCode)
			}
			byShop[v.Shop.ShopCode] = append(byShop[v.Shop.ShopCode], v.Product.Name+" - "+money.GHS(v.Product.ListPrice))
		}
		for _, code := range order {
			lines = append(lines, code+" has: "+strings.Join(byShop[code], " | "))
		}
		lines = append(lines, "Which one do you want to look at?")
		sendPieces(from, lines...)
		if pre, ok := sessions.lookup(sessionKey("", from)); ok {
			pre.DiscoveryShown = pre.DiscoveryShown[:0]
			for _, v := range matches {
				pre.DiscoveryShown = append(pre.DiscoveryShown, ShownProduct{ShopID: v.Shop.ID, ProductID: v.Product.ID})
			}
		} else {
			shown := &Session{Stage: "unbound"}
			for _, v := range matches {
				shown.DiscoveryShown = append(shown.DiscoveryShown, ShownProduct{ShopID: v.Shop.ID, ProductID: v.Product.ID})
			}
			sessions.put(sessionKey("", from), shown)
		}
		return true
	case "browse":
		var lines []string
		for _, m := range catalogue.ListMerchants() {
			// The browse presenter names only what can be
			// bought (the seventh audit): the stocked
			// source every sibling discovery presenter
			// already uses, so a shop with nothing stocked
			// is omitted whole.
			products := stockedProducts(m.ID)
			if len(products) == 0 {
				continue
			}
			names := make([]string, 0, 3)
			for i, p := range products {
				if i >= 3 {
					break
				}
				names = append(names, p.Name)
			}
			lines = append(lines, m.Name+" (SHOP-"+m.ShopCode+"): "+strings.Join(names, ", "))
		}
		if len(lines) == 0 {
			return false
		}
		lines = append(lines, discoveryReplyOr(u, "Tell me a shop, or tell me what you want to buy and I will search every shop for it."))
		sendPieces(from, lines...)
		return true
	case "no_shop":
		if strings.TrimSpace(u.Item) != "" || len(u.Matches) > 0 {
			u.Intent = "search"
			return actOnDiscovery(from, lower, u)
		}
		whatsapp.SendText(from, discoveryReplyOr(u, "No problem at all. What are you looking to buy? I will check every shop for it."))
		return true
	case "offtopic":
		whatsapp.SendText(from, discoveryReplyOr(u, "That one is outside my work here. I sell for the shops on this number: tell me a shop code if you have one, or tell me what you are looking to buy and I will search every shop."))
		return true
	case "unclear":
		whatsapp.SendText(from, discoveryReplyOr(u, "I can help you buy from the shops here. Send me a shop code if you have one, or tell me what you are looking for and I will search every shop."))
		return true
	}
	return false
}

// discoveryTaste names a few real goods across the shops, so
// a buyer whose want is not stocked hears what there is. It
// also returns the goods it named, in the shown shape, so
// the caller can record them as results she was shown.
func discoveryTaste() (string, []ShownProduct) {
	var bits []string
	var shown []ShownProduct
	for _, m := range catalogue.ListMerchants() {
		products := stockedProducts(m.ID)
		if len(products) == 0 {
			continue
		}
		bits = append(bits, products[0].Name+" at "+m.Name)
		shown = append(shown, ShownProduct{ShopID: m.ID, ProductID: products[0].ID})
		if len(bits) >= 3 {
			break
		}
	}
	if len(bits) == 0 {
		return "", nil
	}
	return "Here is some of what the shops have: " + strings.Join(bits, "; ") + ". What else are you looking for?", shown
}

// discoveryNet is the no-model net for the unbound buyer: a
// literal word search over names and descriptions, and a
// short search offer that never repeats the welcome wall.
func discoveryNet(from, text string) {
	lower := strings.ToLower(text)
	type hit struct {
		shop    catalogue.Merchant
		product catalogue.Product
		score   int
	}
	var hits []hit
	for _, m := range catalogue.ListMerchants() {
		for _, p := range stockedProducts(m.ID) {
			score := 0
			nameHay := strings.ToLower(p.Name)
			descHay := strings.ToLower(p.Description)
			for _, w := range strings.FieldsFunc(lower, func(r rune) bool {
				return (r < 'a' || r > 'z') && (r < '0' || r > '9')
			}) {
				// A word in the product's own name is the
				// thing itself; a word that only drifts
				// through a description ("for skin and
				// hair") is a weaker signal, and the net
				// weighs them so, instead of keeping a
				// list of words to ignore.
				if len(w) > 2 {
					if strings.Contains(nameHay, w) {
						score += 3
					}
					if strings.Contains(descHay, w) {
						score++
					}
				}
			}
			if score > 0 {
				hits = append(hits, hit{m, p, score})
			}
		}
	}
	// The best match leads the list: an ordinal pick ("the
	// first one") must land on the closest thing she asked
	// for, not on whichever shop sorts first.
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	// A price wish in her words outranks the score order:
	// cheapest first is arithmetic, and the net enforces it
	// rather than hoping a shelf happens to be priced in
	// order.
	if priceWishRe.MatchString(lower) {
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].product.ListPrice < hits[j].product.ListPrice })
	}
	// A picture ask is answered with the picture of the
	// leading hit, exactly as the thinking's picture road
	// answers it: she is put inside its shop and shown the
	// goods. Under a price wish the leader is the cheapest,
	// so "cheapest pic" shows her the cheapest one's photo
	// instead of a list of both.
	if picRe.MatchString(lower) && len(hits) > 0 {
		h := hits[0]
		bindings.put(from, h.shop.ID)
		sessions.del(sessionKey("", from))
		p := h.product
		sessions.put(sessionKey(h.shop.ID, from), &Session{Stage: "browsing", MerchantID: h.shop.ID, LastViewed: &p})
		answerPictureAsk(from, &h.shop, &p)
		return
	}
	// With a budget in force, even the net's answer is a
	// recommendation: she stated what she can spend, and a
	// bare list that ignores it is a worse answer than the
	// platform's own floor pick under her budget.
	if len(hits) > 0 {
		if pre, ok := sessions.lookup(sessionKey("", from)); ok && pre.Budget > 0 {
			var matches []struct {
				Shop    catalogue.Merchant
				Product catalogue.Product
			}
			seenHit := map[string]bool{}
			for _, h := range hits {
				if seenHit[h.shop.ID+"|"+h.product.ID] {
					continue
				}
				seenHit[h.shop.ID+"|"+h.product.ID] = true
				matches = append(matches, struct {
					Shop    catalogue.Merchant
					Product catalogue.Product
				}{Shop: h.shop, Product: h.product})
			}
			presentDiscoveryRecommendation(from, text, matches)
			return
		}
	}
	if len(hits) == 1 {
		presentSingleDiscoveryMatch(from, &hits[0].shop, &hits[0].product)
		return
	}
	if len(hits) > 0 {
		var lines []string
		pre, ok := sessions.lookup(sessionKey("", from))
		if !ok {
			pre = &Session{Stage: "unbound"}
			sessions.put(sessionKey("", from), pre)
		}
		pre.DiscoveryShown = pre.DiscoveryShown[:0]
		for i, h := range hits {
			if i >= 5 {
				break
			}
			lines = append(lines, h.shop.ShopCode+" has: "+h.product.Name+" - "+money.GHS(h.product.ListPrice))
			pre.DiscoveryShown = append(pre.DiscoveryShown, ShownProduct{ShopID: h.shop.ID, ProductID: h.product.ID})
		}
		lines = append(lines, "Which one do you want to look at?")
		sendPieces(from, lines...)
		return
	}
	key := sessionKey("", from)
	pre, ok := sessions.lookup(key)
	if !ok {
		pre = &Session{Stage: "unbound"}
		sessions.put(key, pre)
	}
	if !pre.DiscoveryAsked {
		pre.DiscoveryAsked = true
		whatsapp.SendText(from, "Tell me what you are looking to buy, and I will check every shop for it. If you have a shop code, you can send that too.")
		return
	}
	whatsapp.SendText(from, "I have not found that yet. Tell me the thing you want in a word or two, or a shop name, and I will look again.")
}

// pickFromShown reads her message as a selection from the
// results she was just shown: naming one of those products
// or shops, or an ordinal ("the first one"), binds her to
// that shop with the product in focus. Selection from a
// presented list is mechanics, verified against the shown
// set alone; anything else falls through to the thinking.
func pickFromShown(from, text string) bool {
	pre, ok := sessions.lookup(sessionKey("", from))
	if !ok || len(pre.DiscoveryShown) == 0 {
		return false
	}
	lower := strings.ToLower(text)
	ordinals := map[string]int{"first": 0, "second": 1, "third": 2, "fourth": 3, "fifth": 4}
	for word, idx := range ordinals {
		if strings.Contains(lower, word) && idx < len(pre.DiscoveryShown) {
			return bindShownPick(from, pre.DiscoveryShown[idx])
		}
	}
	bestScore := 0
	bestNamesProduct := false
	var best *ShownProduct
	splitWords := func(s string) []string {
		return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return (r < 'a' || r > 'z') && (r < '0' || r > '9')
		})
	}
	for i := range pre.DiscoveryShown {
		sp := pre.DiscoveryShown[i]
		p := catalogue.FindByID(sp.ShopID, sp.ProductID)
		shop := catalogue.GetMerchant(sp.ShopID)
		if p == nil || shop == nil || p.AvailableUnits() <= 0 {
			continue
		}
		score := 0
		namesProduct := false
		herWords := map[string]bool{}
		for _, w := range splitWords(lower) {
			herWords[w] = true
		}
		for _, w := range splitWords(p.Name) {
			if len(w) > 2 && herWords[w] {
				score++
				namesProduct = true
			}
		}
		for _, w := range splitWords(shop.Name + " " + shop.ShopCode) {
			if len(w) > 2 && herWords[w] {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			bestNamesProduct = namesProduct
			best = &sp
		}
	}
	// One word that names the product itself is a pick ("the
	// sandals"); a looser match needs two words behind it.
	if best != nil && (bestNamesProduct || bestScore >= 2) {
		return bindShownPick(from, *best)
	}
	return false
}

func bindShownPick(from string, sp ShownProduct) bool {
	shop := catalogue.GetMerchant(sp.ShopID)
	if shop == nil {
		return false
	}
	focus := catalogue.FindByID(sp.ShopID, sp.ProductID)
	if focus != nil && focus.AvailableUnits() <= 0 {
		// A shown product is a stale reference by the time
		// she picks it (the seventh audit): re-check
		// availability at resolution, the way
		// verifiedMatches does at search time. Sold out
		// is answered honestly and binds nothing.
		whatsapp.SendText(from, "The "+focus.Name+" has sold out since I showed it to you. Tell me what else you are looking for and I will search again.")
		return true
	}
	bindDiscoveryShop(from, shop, focus)
	return true
}
