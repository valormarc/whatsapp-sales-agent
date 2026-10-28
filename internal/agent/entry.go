// Shop entry: how a buyer's message finds its shop on the
// shared number, by the code in her link, a code she types, or
// (only before she is bound) the shop's name.
package agent

import (
	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"regexp"
	"strings"
)

var (
	// The explicit entry token in a share link's prefilled text, in
	// its SHOP-EFUA and shop:efua spellings. The word after the
	// marker must resolve to a real shop code, or nothing is routed.
	// A code can itself carry a hyphen (BELORA-2, the form a code
	// takes when its name was already in use), so the capture
	// allows one hyphenated tail.
	shopTokenRe = regexp.MustCompile(`\bshop[-:\s]+([a-z0-9]+(?:-[a-z0-9]+)?)\b`)
	// A hyphenated code typed bare, without the SHOP- marker. It is
	// read before the bare word pass below, which would split it
	// at the hyphen and match its head alone, landing the buyer in
	// the first shop of that name instead of the one she meant.
	hyphenCodeRe = regexp.MustCompile(`\b([a-z]{3,}-[0-9]+)\b`)
	// A bare shop code standing as a word of the message (what a
	// buyer types when she has the code but not the link).
	bareWordSplit = regexp.MustCompile(`[^a-z0-9]+`)
)

// askShopText is what a buyer with no resolved shop hears. It is
// deliberately all she hears: until a shop is resolved, nothing is
// sold, no catalogue is shown and no product is named, so the agent
// can never sell her one shop's goods under another shop's name.
const askShopText = "Hello, and welcome. I sell for many shops on this one number, so I need to know whose shop you have come for. Send me the shop code the seller shared with you, for example SHOP-EFUA, or tell me the shop name."

// parseShopEntry finds the shop a buyer's message enters, and what
// of the message is left once the entry marker is taken out. The
// marker is an explicit SHOP-<code> or shop:<code> token, or a bare
// code word matching a shop. When the buyer has no bound shop yet, a
// shop name in the text also resolves, as the fallback for a buyer
// who arrived with no link. An unresolvable marker resolves to no
// shop at all: routing is never a guess.
func parseShopEntry(lower string, allowName bool) (*catalogue.Merchant, string) {
	if m := shopTokenRe.FindStringSubmatchIndex(lower); m != nil {
		code := lower[m[2]:m[3]]
		if shop := catalogue.MerchantByShopCode(code); shop != nil {
			rest := strings.TrimSpace(lower[:m[0]] + " " + lower[m[1]:])
			return shop, rest
		}
		return nil, lower
	}
	if m := hyphenCodeRe.FindStringSubmatchIndex(lower); m != nil {
		code := lower[m[2]:m[3]]
		if shop := catalogue.MerchantByShopCode(code); shop != nil {
			rest := strings.TrimSpace(lower[:m[0]] + " " + lower[m[1]:])
			return shop, rest
		}
	}
	for _, w := range bareWordSplit.Split(lower, -1) {
		if len(w) < 3 {
			continue
		}
		if shop := catalogue.MerchantByShopCode(w); shop != nil {
			rest := strings.TrimSpace(bareWordSplit.ReplaceAllString(
				strings.Replace(lower, w, " ", 1), " "))
			return shop, rest
		}
	}
	if allowName {
		if shop := catalogue.MerchantByShopName(lower); shop != nil {
			return shop, lower
		}
	}
	return nil, lower
}

// recoverBuyerFromOrders rebuilds a buyer's place from the
// order book when her session is gone. Sessions live in
// memory, so an agent restart (a deploy, a crash) used to
// orphan a buyer mid-deal: her order sat in the book, paid
// for by nobody, while the agent greeted her as a stranger
// and asked whose shop she had come for. The book is the
// durable truth, so it is asked: if her number holds an open
// order in exactly one shop, that is her shop, she is bound
// to it again, and a session is restored at the agreed stage
// with the order's own link, so whatever she says next is
// answered inside the deal she is actually in. It returns the
// shop id, or nothing when the book cannot place her.
func recoverBuyerFromOrders(from string) string {
	shops := map[string]bool{}
	var newest *orders.Order
	for _, o := range orders.Default.All() {
		if o.From != from || o.Status != "pending" {
			continue
		}
		shops[o.MerchantID] = true
		oo := o
		if newest == nil || oo.Reference > newest.Reference {
			newest = &oo
		}
	}
	if len(shops) != 1 || newest == nil {
		return ""
	}
	shopID := newest.MerchantID
	bindings[from] = shopID
	key := sessionKey(shopID, from)
	if _, ok := sessions[key]; !ok {
		sessions[key] = &Session{Stage: "agreed", MerchantID: shopID, PaymentURL: orderPaymentURL(newest)}
	}
	return shopID
}
