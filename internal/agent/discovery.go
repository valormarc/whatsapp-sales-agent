package agent

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
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

Read her message and decide:
- intent "shop": she names a shop (by name or code), or picks a shop or a product from results she was shown. Give "shop" as the shop code, and when she picked a product give it in matches too.
- intent "search": she wants a kind of thing, whether or not she has a shop in mind, and whether she asks it as a question ("which shop has laptops?"), a need ("I am looking for a laptop"), or a statement. Give "item" as the thing in her own words, and "matches": every product in the facts that genuinely fits it BY MEANING, not by wording. A laptop is a notebook computer under any brand name; slides and sandals are the same family; a phone is not a laptop. Name each match by its shop code and its exact product name from the facts. Only products in the facts, never an invention.
- intent "picture": she is asking to SEE a picture of a thing: "show me picture", "send pic", and also "I am not buying without pic", which means the same thing: the picture comes before any buying. Give "item" as the thing she wants to see and "matches" for it exactly as a search.
- intent "browse": she asks what shops or goods there are.
- intent "no_shop": she says she has no particular shop, without naming a thing she wants. If she names a thing as well, it is a search.
- intent "offtopic": her message is not about buying or shops at all.
- intent "unclear": none of the above fits.

"reply" is your answer to her in your own words for no_shop, browse, offtopic and unclear: short, warm, correct English, never a title (no Sir, Madam, Ma or Boss), steering her toward naming a shop or a thing she wants. For search and shop leave reply empty; the agent lists the verified results itself. Never invent a product, a shop or a price, and never repeat a greeting she has already had.

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
			facts += "\nYou just showed her these results: " + strings.Join(shown, "; ") + ". If she now names one of them, or says the first or the second, that is a pick: intent shop."
		}
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
	sendPieces(from, "Welcome to "+shop.Name+".", productListText(shop.ID))
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
		whatsapp.SendText(from, focus.Name+": "+focus.Description+" It goes for "+money.GHS(focus.ListPrice)+"."+offerInvite(focus, " Want to make an offer?"))
	}
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
	if pickFromShown(from, text) {
		return
	}
	if serveDiscoveryPictureAsk(from, text) {
		return
	}
	if u := understandDiscovery(from, text); u != nil && actOnDiscovery(from, u) {
		return
	}
	discoveryNet(from, text)
}

// serveDiscoveryPictureAsk is the intent gate for the
// unbound buyer: a picture ask over results she has just
// been shown is served as a picture ask. She is put inside
// that product's shop and shown the picture (or told
// plainly, with the owner asked for it); nothing is sold on
// this turn, and no stage of any sale sees her words.
func serveDiscoveryPictureAsk(from, text string) bool {
	if !picRe.MatchString(strings.ToLower(text)) {
		return false
	}
	pre, ok := sessions[sessionKey("", from)]
	if !ok || pre == nil || len(pre.DiscoveryShown) == 0 {
		return false
	}
	lower := strings.ToLower(text)
	target := pre.DiscoveryShown[0]
	for _, sh := range pre.DiscoveryShown {
		pp := catalogue.FindByID(sh.ShopID, sh.ProductID)
		if pp == nil {
			continue
		}
		named := false
		for _, w := range strings.FieldsFunc(strings.ToLower(pp.Name), func(r rune) bool {
			return (r < 'a' || r > 'z') && (r < '0' || r > '9')
		}) {
			if len(w) > 2 && strings.Contains(lower, w) {
				named = true
			}
		}
		if named {
			target = sh
			break
		}
	}
	shop := catalogue.GetMerchant(target.ShopID)
	pp := catalogue.FindByID(target.ShopID, target.ProductID)
	if shop == nil || pp == nil {
		return false
	}
	bindings[from] = shop.ID
	delete(sessions, sessionKey("", from))
	sessions[sessionKey(shop.ID, from)] = &Session{Stage: "browsing", MerchantID: shop.ID, LastViewed: pp}
	answerPictureAsk(from, shop, pp)
	return true
}

func actOnDiscovery(from string, u *discoveryUnderstanding) bool {
	switch u.Intent {
	case "picture":
		// The picture comes first: she is put inside the
		// matched product's shop and served the picture
		// ask. Nothing is sold on this turn.
		matches := verifiedMatches(u)
		if len(matches) == 0 {
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
			if taste := discoveryTaste(); taste != "" {
				whatsapp.SendText(from, taste)
			}
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
// a buyer whose want is not stocked hears what there is.
func discoveryTaste() string {
	var bits []string
	for _, m := range catalogue.ListMerchants() {
		products := catalogue.ListProducts(m.ID)
		if len(products) == 0 {
			continue
		}
		bits = append(bits, products[0].Name+" at "+m.Name)
		if len(bits) >= 3 {
			break
		}
	}
	if len(bits) == 0 {
		return ""
	}
	return "Here is some of what the shops have: " + strings.Join(bits, "; ") + ". What else are you looking for?"
}

// discoveryNet is the no-model net for the unbound buyer: a
// literal word search over names and descriptions, and a
// short search offer that never repeats the welcome wall.
// discoveryStop is the filler of a search sentence, never
// the goods: wanting, looking and having say nothing about
// what a product is.
var discoveryStop = map[string]bool{
	"looking": true, "look": true, "want": true, "wants": true,
	"need": true, "needs": true, "buy": true, "sell": true,
	"sells": true, "have": true, "has": true, "the": true,
	"for": true, "and": true, "you": true, "your": true,
	"any": true, "that": true, "this": true, "shop": true,
	"shops": true, "please": true, "get": true, "got": true,
}

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
			hay := strings.ToLower(p.Name + " " + p.Description)
			for _, w := range strings.FieldsFunc(lower, func(r rune) bool {
				return (r < 'a' || r > 'z') && (r < '0' || r > '9')
			}) {
				// The filler of the asking ("looking for",
				// "I want", "do you have") is not the goods:
				// it matches every description ever written
				// and drowns the word that names the thing.
				if len(w) > 2 && !discoveryStop[w] && strings.Contains(hay, w) {
					score++
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
		for _, w := range splitWords(p.Name) {
			if len(w) > 2 && strings.Contains(lower, w) {
				score++
				namesProduct = true
			}
		}
		for _, w := range splitWords(shop.Name + " " + shop.ShopCode) {
			if len(w) > 2 && strings.Contains(lower, w) {
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
