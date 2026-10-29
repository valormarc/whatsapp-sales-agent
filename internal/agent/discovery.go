package agent

import (
	"encoding/json"
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
- intent "search": she wants a kind of thing, whether or not she has a shop in mind, and whether she asks it as a question ("which shop has laptops?"), a need ("I am looking for a laptop"), or a statement. Give "item" as the thing in her own words, and "matches": every product in the facts that genuinely fits it BY MEANING, not by wording. A laptop is a notebook computer under any brand name; slides and sandals are the same family; a phone is not a laptop. Name each match by its shop code and its exact product name from the facts. Only products in the facts, never an invention. When she asks by price (the cheapest, the most affordable), she still means the thing itself; order your matches by list price, cheapest first.
- intent "picture": she is asking to SEE a picture of a thing: "show me picture", "send pic", and also "I am not buying without pic", which means the same thing: the picture comes before any buying. Give "item" as the thing she wants to see and "matches" for it exactly as a search. A price wish inside the picture ask ("send pic of the cheapest heel") orders those matches by list price, cheapest first, so the first match is the one she means. A pick that carries a picture ask in the same breath is this intent, not a pick: "the strappy ones, can I see a picture?" said about a shown result is her choosing that result BY asking to see it — the picture is what she wants, so answer the picture ask rather than only landing her in the shop.
- intent "order_received": she is telling you that an order she already PAID for has arrived and she has the goods now. The facts name her paid orders when she has any; give "shop" as that order's shop code. This is about an order she already made, never a new purchase.
- intent "order_missing": she is telling you that an order she already paid for has NOT arrived: it never came, it is lost, she is still waiting and it is overdue. The facts name her paid orders when she has any; give "shop" as that order's shop code. Merely asking where an order stands is a search of another kind, not this.
- intent "browse": she asks what shops or goods there are.
- intent "no_shop": she says she has no particular shop, without naming a thing she wants. If she names a thing as well, it is a search.
- intent "offtopic": her message is not about buying or shops at all.
- intent "unclear": none of the above fits.

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
	Intent  string           `json:"intent"`
	Shop    string           `json:"shop"`
	Item    string           `json:"item"`
	Matches []discoveryMatch `json:"matches"`
	Reply   string           `json:"reply"`
}

// discoveryDirectory renders the public facts: every shop,
// its code, and its goods at list price.
func discoveryDirectory() string {
	var b strings.Builder
	for _, m := range catalogue.ListMerchants() {
		products := catalogue.ListProducts(m.ID)
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

// currentDiscoveryFrom is the buyer whose discovery turn is
// being understood, so the thinking can be told what she was
// just shown. Turns are serialised by the session lock, so a
// plain package var is safe here.
var currentDiscoveryFrom string

func understandDiscovery(from, text string) *discoveryUnderstanding {
	currentDiscoveryFrom = from
	sys := discoveryCharter
	facts := "The shops and their goods:\n" + discoveryDirectory()
	if pre, ok := sessions[sessionKey("", currentDiscoveryFrom)]; ok && len(pre.DiscoveryShown) > 0 {
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
	if pre, ok := sessions[sessionKey("", from)]; ok {
		if w := recentSpeakerWords(pre, 4); w != "" {
			facts += "\nWhat she said earlier in this chat, oldest first: " + w + "."
		}
	}
	if qf := quoteFactLine(); qf != "" {
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
	}, model.Options{System: &sys})
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
	for _, m := range catalogue.ListMerchants() {
		if strings.EqualFold(m.Name, ref) || strings.Contains(strings.ToLower(m.Name), strings.ToLower(ref)) {
			shop := m
			return &shop
		}
	}
	return nil
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
		if p == nil || seen[shop.ID+"|"+p.ID] {
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
	bindings[from] = shop.ID
	sendPieces(from, welcomeFor(shop, from), productListText(shop.ID))
	if focus != nil {
		key := sessionKey(shop.ID, from)
		s, ok := sessions[key]
		if !ok {
			s = &Session{Stage: "browsing", MerchantID: shop.ID}
			sessions[key] = s
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
	sendProductPhotos(from, p)
	sendProductView(from, p, " Want to make an offer?")
	pre, ok := sessions[sessionKey("", from)]
	if !ok {
		pre = &Session{Stage: "unbound"}
		sessions[sessionKey("", from)] = pre
	}
	pre.DiscoveryShown = []ShownProduct{{ShopID: shop.ID, ProductID: p.ID}}
}

// discoveryReplyOr answers with the model's drafted words
// when they are clean and figure-free, and with the given
// factual line otherwise.
func discoveryReplyOr(u *discoveryUnderstanding, fallback string) string {
	if u != nil {
		if draft := strings.TrimSpace(u.Reply); draft != "" && voiceClean(draft) && !strings.Contains(draft, "₵") {
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
	pre, ok := sessions[sessionKey("", from)]
	if !ok || pre == nil {
		pre = &Session{Stage: "unbound"}
		sessions[sessionKey("", from)] = pre
	}
	noteSpeakerWords(pre, text)
	// The thinking leads, here as everywhere: a pick that
	// carries a picture ask in the same breath is a picture
	// ask, and only the thinking can tell the two apart. The
	// word net below speaks only when no model can be read,
	// the standing rule for every fallback in the agent.
	if u := understandDiscovery(from, text); u != nil && actOnDiscovery(from, u) {
		return
	}
	if quotedPick(from, text) {
		return
	}
	if pickFromShown(from, text) {
		return
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
	q := turnQuote
	if q == nil || !q.Known || q.Product == nil || q.ProductShop == nil {
		return false
	}
	p := catalogue.FindByID(q.ProductShop.ID, q.Product.ID)
	if p == nil {
		return false
	}
	lower := strings.ToLower(text)
	shop := q.ProductShop
	switch {
	case picRe.MatchString(lower):
		bindings[from] = shop.ID
		delete(sessions, sessionKey("", from))
		sessions[sessionKey(shop.ID, from)] = &Session{Stage: "browsing", MerchantID: shop.ID, LastViewed: p}
		answerPictureAsk(from, shop, p)
		return true
	case isBareAck(lower) || parseOffer(lower) != nil || buyIntentRe.MatchString(lower):
		bindDiscoveryShop(from, shop, p)
		return true
	}
	return false
}

// newestPaidOrderAnywhere finds the buyer's paid order
// across every shop that her words mean: the shop the
// thinking named first, then the order in transit (the one
// that can arrive, or fail to), then her newest paid order.
func newestPaidOrderAnywhere(from, shopHint string) *orders.Order {
	var hintShop *catalogue.Merchant
	if shopHint != "" {
		hintShop = resolveDiscoveryShop(shopHint)
	}
	var hinted, inTransit, any *orders.Order
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
				return &oo
			}
		}
	}
	if hinted != nil {
		return hinted
	}
	if inTransit != nil {
		return inTransit
	}
	return any
}

func actOnDiscovery(from string, u *discoveryUnderstanding) bool {
	switch u.Intent {
	case "order_received", "order_missing":
		// A delivery report from a buyer with no live shop
		// session: her order pins the shop, she is bound to
		// it, and the loop's own acts serve her word.
		o := newestPaidOrderAnywhere(from, u.Shop)
		if o == nil {
			return false
		}
		shop := catalogue.GetMerchant(o.MerchantID)
		if shop == nil {
			return false
		}
		bindings[from] = shop.ID
		if u.Intent == "order_received" {
			return buyerConfirmReceived(from, shop, shop.ID)
		}
		return buyerReportMissing(from, shop, shop.ID)
	case "picture":
		// The picture comes first: she is put inside the
		// matched product's shop and served the picture
		// ask. Nothing is sold on this turn.
		matches := verifiedMatches(u)
		if len(matches) == 0 {
			// The thinking named the intent but no product:
			// the picture she means is the one she was just
			// shown, and the conversation itself settles
			// which that is.
			if pre, ok := sessions[sessionKey("", from)]; ok && len(pre.DiscoveryShown) > 0 {
				sp := pre.DiscoveryShown[0]
				if shop := catalogue.GetMerchant(sp.ShopID); shop != nil {
					if p := catalogue.FindByID(sp.ShopID, sp.ProductID); p != nil {
						bindings[from] = shop.ID
						delete(sessions, sessionKey("", from))
						sessions[sessionKey(shop.ID, from)] = &Session{Stage: "browsing", MerchantID: shop.ID, LastViewed: p}
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
		bindings[from] = shop.ID
		delete(sessions, sessionKey("", from))
		sessions[sessionKey(shop.ID, from)] = &Session{Stage: "browsing", MerchantID: shop.ID, LastViewed: &p}
		answerPictureAsk(from, &shop, &p)
		return true
	case "shop":
		shop := resolveDiscoveryShop(u.Shop)
		if shop == nil {
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
				if pre, ok := sessions[sessionKey("", from)]; ok {
					pre.DiscoveryShown = shown
				} else {
					sessions[sessionKey("", from)] = &Session{Stage: "unbound", DiscoveryShown: shown}
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
		if pre, ok := sessions[sessionKey("", from)]; ok {
			pre.DiscoveryShown = pre.DiscoveryShown[:0]
			for _, v := range matches {
				pre.DiscoveryShown = append(pre.DiscoveryShown, ShownProduct{ShopID: v.Shop.ID, ProductID: v.Product.ID})
			}
		} else {
			shown := &Session{Stage: "unbound"}
			for _, v := range matches {
				shown.DiscoveryShown = append(shown.DiscoveryShown, ShownProduct{ShopID: v.Shop.ID, ProductID: v.Product.ID})
			}
			sessions[sessionKey("", from)] = shown
		}
		return true
	case "browse":
		var lines []string
		for _, m := range catalogue.ListMerchants() {
			products := catalogue.ListProducts(m.ID)
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
			return actOnDiscovery(from, u)
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
		products := catalogue.ListProducts(m.ID)
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
		for _, p := range catalogue.ListProducts(m.ID) {
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
	if len(hits) == 1 {
		presentSingleDiscoveryMatch(from, &hits[0].shop, &hits[0].product)
		return
	}
	if len(hits) > 0 {
		var lines []string
		pre, ok := sessions[sessionKey("", from)]
		if !ok {
			pre = &Session{Stage: "unbound"}
			sessions[sessionKey("", from)] = pre
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
	pre, ok := sessions[key]
	if !ok {
		pre = &Session{Stage: "unbound"}
		sessions[key] = pre
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
	pre, ok := sessions[sessionKey("", from)]
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
		if p == nil || shop == nil {
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
	bindDiscoveryShop(from, shop, focus)
	return true
}
