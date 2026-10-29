// Checking an order: the buyer was given a reference when she
// bought (the shop's code and the shop's own serial, EFUA-0007),
// and the order sits in the book under that reference. When she
// comes back she quotes it, or just asks after her order, and it
// is read back to her in plain words. The check is read-only and
// reaches only her own orders in the shop she is standing in: a
// reference that belongs to another buyer, or to another shop,
// is simply not found, so nobody reads another person's purchase
// by guessing codes.
package agent

import (
	"regexp"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// orderRefRe finds a quoted order reference in a buyer's words:
// letters, a dash, and at least four figures (serials are minted
// zero-padded to four; a shop code like BELORA-2, with its single
// figure, can never be mistaken for one).
var orderRefRe = regexp.MustCompile(`\b([A-Za-z]{2,}-\d{4,})\b`)

// orderCheckRe reads a reference-less ask after an order: "check
// my order", "order status", "where is my order". "Order" as a
// verb ("I want to order the bag") matches none of these.
var orderCheckRe = regexp.MustCompile(`(?i)\bmy order\b|\border status\b|\b(check|track)\b[^.?!]*\border\b|\bwhere is my\b[^.?!]*\border\b`)

// handleOrderCheck answers a buyer's order check and reports
// whether it did. It runs after payment claims and the stage
// handling in her turn, and it touches nothing but the reply: a
// check in the middle of a haggle or a fulfilment leaves the
// stage exactly where it stood.
func handleOrderCheck(from string, merchant *catalogue.Merchant, msg, lower string) bool {
	if m := orderRefRe.FindStringSubmatch(msg); m != nil {
		ref := strings.ToUpper(m[1])
		o := orders.Default.GetOrder(ref)
		if o == nil || o.MerchantID != merchant.ID || o.From != from {
			whatsapp.SendText(from, "I cannot find an order with the reference "+ref+" for you at "+merchant.Name+". Check the reference and send it again, or say check my order and I will pull up your latest one here.")
			return true
		}
		// She names her order and asks for its link in the same
		// breath: that is a payment request, not a status check,
		// so the check steps aside and the thinking serves it.
		if o.Unpaid() && linkRe.MatchString(lower) {
			return false
		}
		sendPieces(from, orderReadback(merchant, o)...)
		return true
	}
	if orderCheckRe.MatchString(lower) {
		o := orders.Default.NewestFor(merchant.ID, from)
		if o == nil {
			whatsapp.SendText(from, "You have no order at "+merchant.Name+" yet. When you buy something here I will give you a reference for it, and you can check it with me any time.")
			return true
		}
		sendPieces(from, orderReadback(merchant, o)...)
		return true
	}
	return false
}

// orderReadback states an order the way the buyer thinks of it:
// what it is, whether it is paid, and how the goods are moving.
func orderReadback(merchant *catalogue.Merchant, o *orders.Order) []string {
	item := o.ProductName
	if o.Quantity > 0 && o.Unit != "" {
		item = formatQty(o.Quantity) + " " + unitWord(o.Unit, o.Quantity) + " of " + o.ProductName
	}
	pieces := []string{"Order " + o.Reference + ": " + item + " at " + money.GHS(o.Price) + "."}

	switch {
	case o.Status == "paid":
		pieces = append(pieces, "It is paid. Thank you.")
	case o.ClaimStatus == "open":
		pieces = append(pieces, "You told me you have paid "+merchant.Name+" directly, and I am waiting for them to confirm it.")
	default:
		pieces = append(pieces, "It is not paid yet. The total is "+money.GHS(o.Charged())+".")
	}

	switch o.Fulfilment {
	case "delivery":
		line := "The goods are coming by delivery"
		if o.Location != "" {
			line += " to " + o.Location
		}
		line += "."
		if o.DeliveryPaidTo == "carrier" && o.DeliveryFee > 0 {
			line += " The " + money.GHS(o.DeliveryFee) + " delivery is yours to pay when it reaches you."
		}
		pieces = append(pieces, line)
	case "pickup":
		if o.Collector != "" {
			pieces = append(pieces, o.Collector+" is coming to collect it.")
		} else {
			pieces = append(pieces, "You are collecting it yourself.")
		}
	case "meet":
		line := "You are meeting the seller to collect it"
		if o.Location != "" {
			line += " at " + o.Location
		}
		pieces = append(pieces, line+".")
	case "arranged":
		line := "The seller is arranging the delivery with you"
		if o.Location != "" {
			line += "; you are in " + o.Location
		}
		pieces = append(pieces, line+".")
	}

	if o.PackageName != "" {
		pieces = append(pieces, "The package carries the name "+o.PackageName+".")
	}
	return pieces
}
