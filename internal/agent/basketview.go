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
// names no viewing word and is not this ask.
var basketViewRe = regexp.MustCompile(`\b(show|see|list|view|check)\b[^.?!]*\bmy basket\b|\bwhat\b[^.?!]*\bin my basket\b|^\s*my basket\s*[.!?]?\s*$|\bin my basket\b[^.?!]*\?`)

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
		lines = append(lines, orderItemLines(agreed)...)
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
			lines = append(lines, "Picked, not settled yet:")
		}
		lines = append(lines, orderItemLines(unsettled)...)
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
