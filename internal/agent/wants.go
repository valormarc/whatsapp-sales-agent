// Wants: reading what a buyer needs when she names no
// product, recommending from the shelf, and keeping the want
// book for goods the shop does not carry yet.
package agent

import (
	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/wants"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
	"regexp"
	"strings"
)

// needStopWords are the filler words of a need description: they say
// how the buyer is asking, not what would answer her, so they never
// count toward a recommendation.
var needStopWords = map[string]bool{
	"need": true, "want": true, "something": true, "anything": true,
	"keep": true, "keeps": true, "with": true, "from": true,
	"that": true, "this": true, "have": true, "will": true,
	"your": true, "yours": true, "please": true, "looking": true,
	"give": true, "really": true, "just": true, "like": true,
	"what": true, "mean": true, "right": true, "understand": true,
}

var needWordSplit = regexp.MustCompile(`[^a-z0-9]+`)

// needWords tokenises text for recommendation matching: lower case,
// words of at least four letters, lightly stemmed (a trailing s
// dropped) and stripped of filler.
func needWords(text string) map[string]bool {
	out := map[string]bool{}
	for _, w := range needWordSplit.Split(strings.ToLower(text), -1) {
		if len(w) < 4 || needStopWords[w] {
			continue
		}
		out[strings.TrimSuffix(w, "s")] = true
	}
	return out
}

// recommendForNeed picks the one product on this shop's shelf whose
// name and description best answer the buyer's stated need, by word
// overlap. It returns nil when nothing on the shelf shares a single
// meaningful word with the need: the agent recommends only what it
// can ground, and never reaches into another shop.
func recommendForNeed(shopID, lower string) *catalogue.Product {
	need := needWords(lower)
	if len(need) == 0 {
		return nil
	}
	var best *catalogue.Product
	bestScore := 0
	// The candidate set is the stocked shelf only (the
	// seventh audit): a sold-out best match must yield
	// nil, so the caller's want-recording fall-through
	// carries the buyer instead of a recommendation she
	// cannot buy.
	for _, p := range stockedProducts(shopID) {
		words := needWords(p.Name + " " + p.Description)
		score := 0
		for w := range need {
			if words[w] {
				score++
			}
		}
		if score > bestScore {
			pp := p
			best = &pp
			bestScore = score
		}
	}
	return best
}

// askFillerWords say how a buyer is asking, not what she is
// asking for: articles, pronouns, question words, polite filler
// and shop talk (price, delivery, discount). A candidate made of
// nothing else names nothing: "do you have anything nice?" is a
// buyer thinking aloud, not a want to write down.
var askFillerWords = map[string]bool{
	"a": true, "an": true, "the": true, "any": true, "some": true,
	"anything": true, "something": true, "nothing": true, "everything": true,
	"it": true, "one": true, "ones": true, "this": true, "that": true,
	"me": true, "my": true, "you": true, "your": true, "please": true,
	"nice": true, "good": true, "cheap": true, "new": true, "quality": true,
	"price": true, "prices": true, "discount": true, "discounts": true,
	"delivery": true, "deliver": true, "location": true, "located": true,
	"stock": true, "much": true, "many": true,
	"how": true, "what": true, "when": true, "where": true, "why": true, "who": true,
	"do": true, "does": true, "is": true, "are": true, "can": true, "could": true,
	"have": true, "has": true, "got": true, "get": true, "sell": true,
	"selling": true, "available": true, "in": true, "on": true, "for": true,
	"of": true, "to": true, "at": true,
	"stuff": true, "thing": true, "things": true, "item": true, "items": true,
}

// itemNamed pulls the thing a buyer is asking for out of her
// words: what is left once the asking words are dropped. minLen
// is the shortest leftover word that still names a thing: inside
// an availability frame the frame itself does the signalling, so
// even "tv" counts; standing alone it takes a fuller word.
func itemNamed(candidate string, minLen int) (string, bool) {
	words := []string{}
	for _, w := range needWordSplit.Split(candidate, -1) {
		if w == "" || askFillerWords[w] {
			continue
		}
		words = append(words, w)
	}
	for _, w := range words {
		if len(w) >= minLen {
			return strings.Join(words, " "), true
		}
	}
	return "", false
}

// unstockedAsk reads an availability question, or a bare item
// name with a question mark ("Rice cooker?"), and returns the
// thing the buyer named, when she clearly named one. A frame
// that matches but names nothing specific ("do you have
// anything nice?") is decided by its candidate and goes no
// further: vague questions record no want.
func unstockedAsk(lower string) (string, bool) {
	text := strings.TrimRight(strings.TrimSpace(lower), ".?!")
	for _, re := range []*regexp.Regexp{doYouHaveRe, haveYouGotRe, doYouSellRe, isAvailableRe} {
		if m := re.FindStringSubmatch(text); m != nil {
			return itemNamed(m[1], 2)
		}
	}
	// A bare item name with a question mark is the same ask with
	// the frame worn off. It must be short, the way item names
	// are, and it must be about a thing, not about the seller: a
	// question carrying "you" is a question for the shop ("where
	// are you?"), never an item.
	if strings.HasSuffix(strings.TrimSpace(lower), "?") {
		fields := strings.Fields(text)
		if len(fields) == 0 || len(fields) > 4 {
			return "", false
		}
		for _, f := range fields {
			if f == "you" || f == "your" || f == "yours" {
				return "", false
			}
		}
		return itemNamed(text, 3)
	}
	return "", false
}

// stockedByLooseName resolves an asked-for thing against the
// shelf the way the merchant's own commands do, trying the plural
// as typed and then singular: close enough to answer the item
// itself rather than write a want for goods already in the shop.
func stockedByLooseName(shopID, item string) *catalogue.Product {
	// The name promises stocked: a product the shelf no
	// longer holds does not resolve here (the seventh
	// audit found the helper never checked), so the
	// availability road treats it as unstocked and
	// records the want.
	if p := catalogue.FindByName(shopID, item); p != nil && p.AvailableUnits() > 0 {
		return p
	}
	if strings.HasSuffix(item, "s") {
		if p := catalogue.FindByName(shopID, strings.TrimSuffix(item, "s")); p != nil && p.AvailableUnits() > 0 {
			return p
		}
	}
	return nil
}

// recordWantAndOfferShelf handles a buyer asking for something this
// shop does not stock: the want is written into the shop's want book
// (she will be alerted if it ever lands; see package wants), and the
// shelf is offered back with a light question, the way a real seller
// answers. The offer wording rotates like the other repeated lines.
func recordWantAndOfferShelf(from string, s *Session, msg string) {
	wants.Default.Record(s.MerchantID, from, msg)
	shelfLines := []string{
		"Let me help you find it. Tell me which of these you are after.",
		"This is what is on the shelf just now. Which of these are you after?",
		"Have a look at these and tell me which one you want.",
	}
	prefix := shelfLines[s.Shelves%len(shelfLines)]
	s.Shelves++
	sendPieces(from,
		"I do not have that one here yet. The moment it lands, I will alert you on this number.",
		prefix+"\n"+productListText(s.MerchantID))
}

// alertWaitingBuyers tells every buyer whose recorded want a newly
// stocked product answers, once each, through the ordinary send path
// (so dry-run runs land in the outbox like every other message). The
// want book marks the wants alerted, so a second matching product
// never alerts the same want again.
func alertWaitingBuyers(merchantID string, product catalogue.Product) {
	shop := catalogue.GetMerchant(merchantID)
	if shop == nil {
		return
	}
	matched := wants.Default.MatchAndMark(merchantID, product.Name)
	told := map[string]bool{}
	for _, w := range matched {
		if told[w.Buyer] {
			continue
		}
		told[w.Buyer] = true
		whatsapp.SendText(w.Buyer, product.Name+" is back in stock at "+shop.Name+", "+money.GHS(product.ListPrice)+". Reply to buy.")
	}
}
