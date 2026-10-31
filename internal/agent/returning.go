package agent

// Returning-buyer recognition. A seller remembers her
// regulars: where they are, how they like to get their
// goods, what they bought last time. The agent's memory of
// a buyer is her order book at that shop, nothing invented:
// her last PAID order says how the goods actually reached
// her, and it is held up for her to confirm or change,
// never silently reused. The same book answers whether she
// has bought here before (her welcome says so) and what
// size she took last time for the same product (held for
// the recap instead of asked again, correctable there).

import (
	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// lastPaidOrderFor is a buyer's newest paid order at one
// shop: the purchase that actually happened, whose
// fulfilment and sizes describe what she did, not what she
// once started.
func lastPaidOrderFor(shopID, buyer string) *orders.Order {
	var best *orders.Order
	for _, o := range orders.Default.All() {
		oo := o
		if oo.MerchantID != shopID || oo.From != buyer || oo.Status != "paid" {
			continue
		}
		if best == nil || oo.CreatedAt > best.CreatedAt {
			best = &oo
		}
	}
	return best
}

// rememberedFulfilment reads how a returning buyer's goods
// reached her last time, as a fulfilment she can confirm:
// the mode, the place, the collector, and, for a delivery,
// today's fee under today's terms rather than the old one.
func rememberedFulfilment(shopID, buyer string, merchant *catalogue.Merchant) (fulfilment, bool) {
	o := lastPaidOrderFor(shopID, buyer)
	if o == nil || o.Fulfilment == "" {
		return fulfilment{}, false
	}
	f := fulfilment{Mode: o.Fulfilment, Location: o.Location, Collector: o.Collector}
	if f.Mode == "delivery" {
		// Her place's own fee, not the flat one: the
		// third audit found a returning buyer to Kasoa
		// re-priced at the shop's flat 40 over her area's
		// 80, because this road read the flat helper the
		// fresh-delivery and reorder roads never use.
		f.Fee = deliveryFeeFor(merchant, f.Location)
	}
	return f, true
}

// usualQuestionText asks whether last time's way is this
// time's way, in the shape of what last time actually was.
func usualQuestionText(f fulfilment) string {
	switch f.Mode {
	case "delivery":
		return "Last time you had it delivered to " + f.Location + ". Same again, or different?"
	case "meet":
		return "Last time you met the seller at " + f.Location + ". Same again, or different?"
	case "pickup":
		if f.Collector != "" {
			return "Last time " + f.Collector + " collected it for you. Same again, or different?"
		}
		return "Last time you picked it up yourself. Same again, or different?"
	default: // arranged
		return "Last time you were in " + f.Location + " and delivery was arranged with the seller. Same again, or different?"
	}
}

// lastSizeFor is the size a buyer took for one product on
// her newest paid order at this shop that carried one, so
// a repeat purchase is not asked what the book already
// knows.
func lastSizeFor(shopID, buyer, productName string) string {
	bestAt, size := "", ""
	for _, o := range orders.Default.All() {
		if o.MerchantID != shopID || o.From != buyer || o.Status != "paid" || o.CreatedAt < bestAt {
			continue
		}
		for _, it := range o.Items {
			if it.Name == productName && it.Size != "" {
				bestAt, size = o.CreatedAt, it.Size
			}
		}
		if o.ProductName == productName && o.Size != "" {
			bestAt, size = o.CreatedAt, o.Size
		}
	}
	return size
}

// welcomeFor greets a buyer at a shop's door: back, when
// the book says she has bought here before.
func welcomeFor(shop *catalogue.Merchant, from string) string {
	if shop != nil && lastPaidOrderFor(shop.ID, from) != nil {
		return "Welcome back to " + shop.Name + "."
	}
	return "Welcome to " + shop.Name + "."
}
