package agent

// More than one item in an order. A buyer rarely buys the
// way a checkout form expects, one thing alone: she settles
// the heels, then remembers the bag. So an agreed item can
// be parked while she haggles over the next one, each on
// its own terms (its own haggle, its own draw on the day's
// allowance, its own last price), and the whole basket
// completes as ONE order: one fulfilment, one set of
// package details, one payment link for the combined total.
// Adding is always confirmed before anything is parked:
// the stage thinking names the wish, the code verifies the
// product against the shelf, and she is asked once, with
// the figures in front of her. Nothing joins an order on a
// guess.

import (
	"strings"
	"unicode"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// basketGoods is what the items already parked in this
// order come to, before the pending product is counted.
func basketGoods(s *Session) float64 {
	var sum float64
	for _, it := range s.Basket {
		sum += it.AgreedPrice
	}
	return sum
}

// orderItemsOf lays out the whole order's lines: the parked
// items, then the pending product at the price this sale
// was completed with.
func orderItemsOf(s *Session, f fulfilment, price float64) []orders.OrderItem {
	items := make([]orders.OrderItem, 0, len(s.Basket)+1)
	for _, it := range s.Basket {
		label := it.Product.Name
		if it.Qty > 0 && it.Product.Unit != "" {
			label = qtyLabel(it.Product, it.Qty)
		}
		items = append(items, orders.OrderItem{Name: label, Price: it.AgreedPrice, Quantity: it.Qty, Unit: it.Unit, Size: it.Size})
	}
	label := s.itemLabel()
	items = append(items, orders.OrderItem{Name: label, Price: price, Quantity: s.Qty, Unit: s.Unit, Size: f.Size})
	return items
}

// itemsLabel joins an order's lines the way a person names
// them: "Ankle Strap Heels and Leather Bag".
func itemsLabel(items []orders.OrderItem) string {
	names := make([]string, 0, len(items))
	for _, it := range items {
		names = append(names, it.Name)
	}
	if len(names) <= 2 {
		return strings.Join(names, " and ")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// basketBreakdown is the buyer's one-line account of a
// multi-item order so far: every line with its price and
// its size when she chose one.
func basketBreakdown(items []orders.OrderItem) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		part := it.Name
		if it.Size != "" {
			part += " (size " + it.Size + ")"
		}
		part += " " + money.GHS(it.Price)
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

// basketNames joins the parked items' names for a spoken
// line: "Ankle Strap Heels and Leather Bag".
func basketNames(s *Session) string {
	names := make([]string, 0, len(s.Basket))
	for _, it := range s.Basket {
		names = append(names, it.Product.Name)
	}
	if len(names) <= 2 {
		return strings.Join(names, " and ")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// inBasket reports whether a product is already part of
// this order, parked or pending.
func inBasket(s *Session, p *catalogue.Product) bool {
	if p == nil {
		return false
	}
	if s.Product != nil && s.Product.ID == p.ID {
		return true
	}
	for _, it := range s.Basket {
		if it.Product.ID == p.ID {
			return true
		}
	}
	return false
}

// namedBasketCandidate finds the shelf product a message
// names in full (every significant word of its name, in
// order), the structural read used when no thinking can be
// read. It never returns the pending product or one already
// in the order.
func namedBasketCandidate(s *Session, msg string) *catalogue.Product {
	lower := strings.ToLower(msg)
	var best *catalogue.Product
	for _, p := range catalogue.ListProducts(s.MerchantID) {
		pp := p
		if pp.AvailableUnits() <= 0 || inBasket(s, &pp) {
			continue
		}
		name := strings.ToLower(pp.Name)
		if !strings.Contains(lower, name) {
			continue
		}
		if best == nil || len(pp.Name) > len(best.Name) {
			best = &pp
		}
	}
	return best
}

// addNameTokens splits a product name or a buyer's words
// into the small set of comparable tokens used to verify
// an add's referent. A trailing plural s is folded away,
// as in the want book's matching.
func addNameTokens(text string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(w) < 3 {
			continue
		}
		out[strings.TrimSuffix(w, "s")] = true
	}
	return out
}

// verifiedStageAddCandidate verifies the product behind a
// judged add against the buyer's own words. The thinking
// names the act and proposes the item, but a live real-shop
// run showed it can substitute a different shelf product
// for the one she actually named ("add the headwrap" came
// back as the dress). A name word that belongs to exactly
// one product on this shelf is therefore decisive: when her
// words identify exactly one addable product that way, that
// product is the candidate, even when the model's item
// names another. When her words identify several, the
// model's candidate stands only if it is one of them; when
// they identify none, the model's reading is left to carry
// the referent from context, as it did before.
func verifiedStageAddCandidate(s *Session, msg, item string) *catalogue.Product {
	modelCandidate := catalogue.FindProduct(s.MerchantID, strings.ToLower(item))

	products := catalogue.ListProducts(s.MerchantID)
	tokenOwners := map[string]int{}
	for _, p := range products {
		for token := range addNameTokens(p.Name) {
			tokenOwners[token]++
		}
	}
	words := addNameTokens(msg)
	var matched []*catalogue.Product
	for i := range products {
		p := products[i]
		if inBasket(s, &p) {
			continue
		}
		for token := range addNameTokens(p.Name) {
			if tokenOwners[token] == 1 && words[token] {
				matched = append(matched, &p)
				break
			}
		}
	}
	switch {
	case len(matched) == 1:
		return matched[0]
	case len(matched) > 1:
		if modelCandidate != nil {
			for _, p := range matched {
				if p.ID == modelCandidate.ID {
					return modelCandidate
				}
			}
		}
		return nil
	default:
		return modelCandidate
	}
}

// addQuestionText is the one confirming question an add
// always gets, with the running total in front of her. It
// is asked on buttons (see askAdd): a tap cannot be misread,
// and her typed yes or no reads exactly as it always did.
func addQuestionText(s *Session, p *catalogue.Product) string {
	total := basketGoods(s) + s.AgreedPrice + p.ListPrice
	return "Add the " + p.Name + " at " + money.GHS(p.ListPrice) + " to this order too? That would make your items " + money.GHS(total) + " in all, before delivery."
}

// askAdd puts the add question on its buttons.
func askAdd(from string, s *Session, p *catalogue.Product) {
	askYesNo(from, s, "add", p.Name, addQuestionText(s, p), "Yes, add it", "No, leave it")
}

// offerAdd puts the confirming question for a verified
// candidate. A product already in the order is answered
// with that fact and the open question.
func offerAdd(from string, s *Session, merchant *catalogue.Merchant, candidate *catalogue.Product) bool {
	if candidate == nil {
		return false
	}
	if candidate.AvailableUnits() <= 0 {
		// The sink guard (the seventh audit): a candidate
		// the thinking road resolved is checked here too,
		// so no source can price a sold-out add.
		whatsapp.SendText(from, "The "+candidate.Name+" has sold out, so I cannot add it to this order.")
		reaskFulfilStep(from, s)
		return true
	}
	key := sessionKey(s.MerchantID, from)
	if inBasket(s, candidate) {
		whatsapp.SendText(from, "The "+candidate.Name+" is already in this order.")
		reaskFulfilStep(from, s)
		return true
	}
	s.PendingAdd = candidate
	sessions.put(key, s)
	askAdd(from, s, candidate)
	return true
}

// servePendingAdd reads her answer to the add question. A
// picture ask about the candidate is served first (she is
// deciding whether to add it); a call-off ends the whole
// pending sale, as everywhere in this stage; a yes parks
// the settled item and puts her in front of the new one; a
// no returns to the open question, the order untouched.
func servePendingAdd(from string, s *Session, merchant *catalogue.Merchant, msg, lower string) bool {
	key := sessionKey(s.MerchantID, from)
	candidate := s.PendingAdd
	// A bare yes or no while a different question is armed
	// in her chat is that question's answer, not this one's.
	if !s.yesNoFor("add") && (isClearYes(lower) || isClearNo(lower)) {
		return false
	}
	if reading := thinkStageIntent(from, s, merchant, msg); reading.Intent == stageIntentPicture {
		answerPictureAsk(from, merchant, candidate)
		askAdd(from, s, candidate)
		return true
	} else if reading.Intent == stageIntentCancel || fulfilCancelRe.MatchString(lower) {
		dropPendingSale(from, s)
		return true
	}
	if isClearYes(lower) {
		// Park what is settled, exactly as it was agreed.
		s.Basket = append(s.Basket, BasketItem{Product: s.Product, AgreedPrice: s.AgreedPrice, Qty: s.Qty, Unit: s.Unit})
		s.PendingAdd = nil
		s.clearYesNo("add")
		s.Product = candidate
		s.Ask = candidate.ListPrice
		s.AgreedPrice = 0
		s.Qty, s.Unit = 0, ""
		s.Round, s.Floor, s.StepScale = 0, 0, 0
		s.PendingClose = false
		s.FulfilStep = ""
		s.PendingFulfil = fulfilment{}
		s.Stage = "browsing"
		s.LastViewed = candidate
		sessions.put(key, s)
		whatsapp.SendText(from, "Added. The "+s.Basket[len(s.Basket)-1].Product.Name+" stays in your order.")
		sendProductView(from, candidate, " Make me an offer and let us see.")
		return true
	}
	if isClearNo(lower) {
		s.PendingAdd = nil
		s.clearYesNo("add")
		sessions.put(key, s)
		reaskFulfilStep(from, s)
		return true
	}
	askAdd(from, s, candidate)
	return true
}
