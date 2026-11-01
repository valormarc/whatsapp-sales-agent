package agent

// The basket view. A buyer who asks to see her basket is
// asking after her own standing in this order, the same
// kind of structural ask as checking an order: one
// referent, read from the session, answered on the fixed
// paper. Until this view existed the ask had no road of
// its own and fell to the model's drafted talk, which
// strung the items into a sentence with no prices at all
// ("Your basket has Thong Sandals and Double Clothes
// Rack."), or misread the ask entirely — a welcome, an
// order status — depending on the stage she stood in.
// The view is read-only: it lists what she holds and
// leaves every stage exactly where it stood.
//
// What she holds comes in two kinds and the list keeps
// them apart: items already settled in this order, at
// the prices they settled at, and items picked but not
// settled yet, at their shelf prices under their own
// heading, so a shelf price is never read as a settled
// one.

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// basketViewRe reads the ask to see the basket: a viewing
// word aimed at "my basket", a what-question over "in my
// basket", the bare words on their own, or a membership
// question carrying "in my basket". "Add to my basket"
// names no viewing word and is not this ask. Every other
// phrasing of the ask ("check basket") is the model's to
// read: the buyer board names it and this view prints it
// (see understand.go), because what counts as asking
// after her basket is a judgement, not a word list.
var basketViewRe = regexp.MustCompile(`\b(show|see|list|view|check)\b[^.?!]*\bmy basket\b|\bwhat\b[^.?!]*\bin my basket\b|^\s*my basket\s*[.!?]?\s*$|\bin my basket\b[^.?!]*\?`)

// basketFactLine states her basket's shape for a reading:
// how much stands settled and how much is only picked. The
// names stay out of it; the reading only needs to know the
// basket is a live topic in this conversation.
func basketFactLine(s *Session) string {
	settled := len(s.Basket)
	if s.Stage == "fulfilment" || s.Stage == "agreed" || s.AgreedPrice > 0 {
		if s.Product != nil {
			settled++
		}
	}
	picked := 0
	if s.ShelfSettling && s.Product != nil {
		picked++
	}
	picked += len(s.ShelfPicks)
	if settled == 0 && picked == 0 {
		return ""
	}
	return "In her basket so far: " + countPhrase(settled, "settled item") + ", " + countPhrase(picked, "picked item") + "."
}

func countPhrase(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// serveBasketView answers the basket ask from the session
// and reports whether it did. The ask is matched by the
// caller ahead of the thinking and the guided flows, the
// way the order check is, because at every stage the
// answer is the same list.
func serveBasketView(from string, s *Session, shopID string) bool {
	var agreed []orders.OrderItem
	for _, it := range s.Basket {
		if it.Product == nil {
			continue
		}
		name := it.Product.Name
		if it.Qty > 0 && it.Product.Unit != "" {
			name = qtyLabel(it.Product, it.Qty)
		}
		agreed = append(agreed, orders.OrderItem{Name: name, Price: it.AgreedPrice, Size: it.Size})
	}
	// The item in front of her stands agreed when the sale
	// is in its fulfilment or agreed stage: it joins the
	// settled lines at its agreed price.
	if s.Product != nil && s.AgreedPrice > 0 && len(s.TotalHand) == 0 &&
		(s.Stage == "fulfilment" || s.Stage == "agreed") {
		agreed = append(agreed, orders.OrderItem{Name: s.itemLabel(), Price: s.AgreedPrice, Size: s.PendingFulfil.Size})
	}

	var unsettled []orders.OrderItem
	if len(s.TotalHand) > 0 {
		// A hand being haggled as one lot: its items are
		// picked, none of them settled yet.
		for _, p := range s.TotalHand {
			unsettled = append(unsettled, orders.OrderItem{Name: p.Name, Price: p.ListPrice})
		}
	} else {
		seen := map[string]bool{}
		if s.ShelfSettling && s.Product != nil {
			unsettled = append(unsettled, orders.OrderItem{Name: s.Product.Name, Price: s.Product.ListPrice})
			seen[s.Product.ID] = true
		}
		byID := map[string]catalogue.Product{}
		for _, p := range catalogue.ListProducts(shopID) {
			byID[p.ID] = p
		}
		for _, id := range s.ShelfPicks {
			if seen[id] {
				continue
			}
			if p, ok := byID[id]; ok {
				unsettled = append(unsettled, orders.OrderItem{Name: p.Name, Price: p.ListPrice})
				seen[id] = true
			}
		}
	}

	if len(agreed) == 0 && len(unsettled) == 0 {
		whatsapp.SendText(from, "Your basket is empty at the moment. When you pick something or settle an item here, it will stand in your basket.")
		return true
	}
	lines := []string{"Here is what you have so far:"}
	if len(agreed) > 0 {
		lines = append(lines, orderItemBlock(agreed)...)
		settled := 0.0
		for _, it := range agreed {
			settled += it.Price
		}
		if len(unsettled) == 0 {
			lines = append(lines, "That is "+money.GHS(settled)+" in all.")
		} else {
			lines = append(lines, "That is "+money.GHS(settled)+" settled so far.")
		}
	}
	if len(unsettled) > 0 {
		if len(agreed) > 0 {
			lines = append(lines, "", "Picked, not settled yet:")
		}
		lines = append(lines, orderItemBlock(unsettled)...)
		if len(agreed) == 0 {
			sum := 0.0
			for _, it := range unsettled {
				sum += it.Price
			}
			tail := "That comes to " + money.GHS(sum) + " at shelf prices."
			switch {
			case len(s.TotalHand) > 0:
				tail += " We are settling one price for all of them together."
			case s.ShelfSettling:
				tail += " We are settling them one by one."
			default:
				tail += " Tap Done when you are ready and we will settle them one by one."
			}
			lines = append(lines, tail)
		}
	}
	whatsapp.SendText(from, strings.Join(lines, "\n"))
	return true
}

// basketClearRe reads the command to empty the basket in
// its canonical shapes: a clearing word aimed at the
// basket, in either order. Every other phrasing of the
// command ("remove everything I picked", "start over")
// is the model's to read: the buyer board names it as
// its own act (see understand.go), because what counts
// as telling the shop to drop the basket is a judgement,
// not a word list.
var basketClearRe = regexp.MustCompile(`\b(clear|empty)\b[^.?!]*\bbasket\b|\bbasket\b[^.?!]*\b(clear|empty)\b`)

// basketClearAsked reports whether this turn is the
// clearing command: the canonical shape, and not a
// question about the basket — "did we clear the
// basket?" asks after it, and questions belong to the
// view and the model's reading, never to this net.
func basketClearAsked(lower string) bool {
	if strings.HasSuffix(strings.TrimSpace(lower), "?") {
		return false
	}
	return basketClearRe.MatchString(lower)
}

// basketStands reports whether anything stands in this
// order for her to clear: settled items, picks waiting,
// a hand on the table, or a sale of one item in motion.
func basketStands(s *Session) bool {
	if len(s.Basket) > 0 || len(s.ShelfPicks) > 0 || len(s.TotalHand) > 0 {
		return true
	}
	if s.ShelfSettling && s.Product != nil {
		return true
	}
	return s.PendingClose || s.Stage == "haggling" || s.Stage == "fulfilment" || s.Stage == "agreed"
}

// serveBasketClear empties her basket: everything she
// has picked or settled in this order so far goes, and
// any sale in motion with it, the way a dropped sale
// ends — the session is replaced whole, so nothing of
// the old order can survive inside the new one. An
// order already made is not a basket and is never
// touched here; cancelling that is its own road.
func serveBasketClear(from string, s *Session, shopID string) bool {
	if !basketStands(s) {
		whatsapp.SendText(from, "Your basket is empty at the moment. When you pick something or settle an item here, it will stand in your basket.")
		return true
	}
	var view *catalogue.Product
	if s.Product != nil {
		view = s.Product
		if s.Qty > 0 {
			view = wholeProductOf(shopID, s.Product)
		}
	} else {
		view = s.LastViewed
	}
	cancelFeeQuote(shopID, from)
	cancelInHand(shopID, from)
	sessions.put(sessionKey(shopID, from), &Session{Stage: "browsing", MerchantID: shopID, LastViewed: view, Location: s.Location, History: s.History})
	whatsapp.SendText(from, "Cleared. Your basket is empty now.")
	return true
}
