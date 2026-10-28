// Completing a sale: the order is recorded, the day book
// books it, the buyer gets her payment paths, and the merchant
// is told the whole story.
package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/haggle"
	"github.com/valormarc/hackathon-agent/internal/insights"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// completeSale finishes a sale whose fulfilment is settled: the
// order is recorded with how the goods move, the day book books
// the item price, the payment link goes out, and the merchant is
// told the whole story, including who to hand the goods to.
func completeSale(from string, s *Session, price float64, f fulfilment) {
	p := s.Product
	shopID := s.MerchantID
	merchant := catalogue.GetMerchant(shopID)
	// The reference is the order's key in the book and the
	// buyer's receipt: the shop's own code and the shop's own
	// count, so the code alone names whose order it is (EFUA-0007
	// is Efua's seventh order) and no other shop can ever mint
	// the same one. The count is bumped past any key already in
	// the book, so a reference is never reused and an order is
	// never silently overwritten.
	code := merchant.ShopCode
	if code == "" {
		code = strings.ToUpper(shopID)
	}
	serial := orders.Default.CountFor(shopID) + 1
	reference := fmt.Sprintf("%s-%04d", code, serial)
	for orders.Default.GetOrder(reference) != nil {
		serial++
		reference = fmt.Sprintf("%s-%04d", code, serial)
	}
	// Delivery money has two purses, and the shop's own setting
	// (deliveryPaidToOf) says which one it falls into. When the
	// merchant takes it herself, the fee is a pass-through on top
	// of the agreed item price: the buyer pays it here so the goods
	// can travel, the haggle never negotiated it, and it is charged
	// as part of one total, the way buyers ask for it ("How much
	// in total?"). When the carrier takes it (the default), the
	// buyer pays whoever brings the item, on arrival, and the fee
	// never enters the charged total at all: the order still
	// records the quoted fee and who it goes to, so the books know,
	// but the merchant's money is the item price alone. Either way
	// the fee rides only on a delivery she chose; pickup and
	// meet-ups carry none. And the day book below books the item
	// price only in both purses: revenue and givenAway are item
	// figures, never inflated by delivery money.
	deliveryFee := f.Fee
	paidTo := ""
	chargedFee := deliveryFee
	if f.Mode == "delivery" {
		paidTo = deliveryPaidToOf(merchant)
		if paidTo == "carrier" {
			chargedFee = 0
		}
	}
	total := price + chargedFee
	link := checkout.CreatePaymentLink(checkout.LinkRequest{Amount: total, Reference: reference, Subaccount: merchant.SubaccountCode})
	status := "link_failed"
	if link.URL != nil {
		status = "pending"
	}
	// The order carries its shop and that shop's payout destination,
	// so settlement can never drift to another merchant.
	payout := ""
	if merchant != nil {
		payout = merchant.PayoutPhone
	}
	orders.Default.RecordOrder(reference, orders.Order{
		From:           from,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		ProductName:    p.Name,
		Price:          price,
		DeliveryFee:    deliveryFee,
		Total:          total,
		ProviderRef:    link.ProviderRef,
		Status:         status,
		MerchantID:     shopID,
		PayoutPhone:    payout,
		Fulfilment:     f.Mode,
		Location:       f.Location,
		Collector:      f.Collector,
		PackageName:    f.PackageName,
		Note:           f.Note,
		DeliveryPaidTo: paidTo,
		Quantity:       s.Qty,
		Unit:           s.Unit,
	})
	merchantPhone := merchantNotifyPhone(merchant)
	// Seev was configured but the session could not be created: say so,
	// to both sides, rather than send a link that does not work. No
	// sale is booked in the day book for a deal the customer cannot pay
	// for.
	if link.URL == nil {
		sessions[sessionKey(shopID, from)] = &Session{Stage: "browsing", MerchantID: shopID, Location: s.Location}
		whatsapp.SendText(from, "We have a deal at "+money.GHS(price)+" for the "+p.Name+", but the payment link is not coming up right now. The shop has been told and will reach out here to complete your order.")
		if merchantPhone != "" {
			whatsapp.SendText(merchantPhone, "Payment link failed for an agreed deal: "+p.Name+" at "+money.GHS(price)+", buyer "+from+". Ref: "+reference+". Please reach out to the buyer to complete it.")
		}
		return
	}
	url := *link.URL
	// The day book books the item price alone (see the delivery note
	// above): a sale at the item price, giveaway measured against the
	// list price, delivery money nowhere in the day's figures.
	day := daybook.Default.RecordSale(shopID, daybook.Sale{
		ListPrice:   p.ListPrice,
		AgreedPrice: price,
		Allowance:   merchant.DailyGiveawayAllowance,
	})
	sessions[sessionKey(shopID, from)] = &Session{Stage: "agreed", MerchantID: shopID, Product: p, AgreedPrice: price, PaymentURL: url, Location: s.Location}
	linkPiece := "Pay here to complete your order: " + url
	// The completion goes out piece by piece, the way a person
	// types it: the total the seller worked out for her, what
	// happens with the goods, then the link. The deal line itself
	// was already sent when the price was agreed (beginFulfilment).
	var pieces []string
	switch {
	case f.Mode == "delivery" && chargedFee > 0:
		// The merchant takes the delivery money herself: one
		// total, worked out for the buyer, fee inside it.
		line := "Item " + money.GHS(price) + " + delivery " + money.GHS(deliveryFee) + " = " + money.GHS(total) + " total"
		if f.Location != "" {
			line += ", to " + f.Location
		}
		pieces = []string{line + "."}
	case f.Mode == "delivery" && deliveryFee > 0:
		// The carrier takes the delivery money: two purses,
		// named plainly, so she never mistakes the fee for part
		// of what she pays here. What she pays here is the item.
		note := ""
		if merchant != nil {
			note = merchant.DeliveryNote
		}
		feeLine := "Delivery is " + money.GHS(deliveryFee) + ", you pay that to " + carrierTarget(note) + " when it reaches you."
		if f.Location != "" {
			feeLine += " It is going to " + f.Location + "."
		}
		pieces = []string{
			"Your total with me is " + money.GHS(price) + ".",
			feeLine,
		}
	case f.Mode == "delivery":
		line := "Your total is " + money.GHS(total) + "."
		if f.Location != "" {
			line = "Your total is " + money.GHS(total) + ". It goes to " + f.Location + "."
		}
		pieces = []string{line}
	case f.Mode == "pickup" && f.Collector != "":
		pieces = []string{
			"Noted. I have told the seller that " + f.Collector + " is coming for it.",
			"No delivery fee. Your total is " + money.GHS(total) + ".",
		}
	case f.Mode == "pickup":
		pieces = []string{
			"No delivery fee, you collect it yourself. Your total is " + money.GHS(total) + ".",
		}
	case f.Mode == "meet":
		// Honest wording, no guarantees beyond the sale: the
		// seller has been told the place and it is on the order;
		// whether she comes to it is hers to say.
		pieces = []string{
			"I have told the seller you want to meet at " + f.Location + " to collect it. The place is on your order.",
			"No delivery fee. Your total is " + money.GHS(total) + ".",
		}
	default: // arranged
		pieces = []string{
			"Delivery will be arranged with the seller.",
			"Your total is " + money.GHS(total) + ".",
		}
	}
	// The package details she gave ride with the order, and she
	// hears the name read back so a wrong name is caught here,
	// not at the rider's door.
	if f.PackageName != "" {
		pieces = append(pieces, "The package will carry the name "+f.PackageName+".")
	}
	if f.Note != "" {
		pieces = append(pieces, "Your message for the seller is on the order.")
	}
	// Her receipt: the reference goes to her as its own piece, so
	// it sits in her chat the way a shop hands over a receipt
	// number. It is recorded against this order in the book, and
	// she can quote it any time to have the order pulled up.
	pieces = append(pieces, "Your order reference is "+reference+". Keep it: send it to me any time and I will pull up your order.")
	pieces = append(pieces, linkPiece)
	// The second door, offered beside the link: plenty of buyers
	// will not tap a link from a number they just met, but they
	// will send MoMo to the merchant's own number, the way they
	// already buy every day. The offer is worded from the order's
	// own payout phone, and is only made when the shop has one on
	// record: a number is never invented. Choosing it, and what a
	// screenshot can and cannot prove, lives in payment.go.
	if payout != "" {
		shopName := ""
		if merchant != nil {
			shopName = merchant.Name
		}
		pieces = append(pieces, "Or send MoMo directly to "+shopName+"'s number, "+payout+", and send me the screenshot here.")
	}
	sendPieces(from, pieces...)
	if merchantPhone != "" {
		// The notice is the seller's receipt for the sale, and it
		// is laid out the way she reads on her phone: the goods
		// and the money first, then the order's own details with
		// labels, then the day so far. One message, short lines,
		// nothing buried inside a paragraph. A measured sale is
		// named by its amount, so the merchant knows what to cut
		// ("3 yards of Ankara Fabric (6 yards)"), and the figures
		// behind it are the scaled ones the haggle ran on, list
		// included.
		var lines []string
		lines = append(lines, "New order", "")
		if s.Qty > 0 {
			lines = append(lines, "*"+qtyLabel(p, s.Qty)+"*")
		} else {
			lines = append(lines, "*"+p.Name+"*")
		}
		margin := haggle.MarginGivenAway(p.ListPrice, price)
		sold := "Sold for " + money.GHS(price) + " (list " + money.GHS(p.ListPrice) + ", "
		if margin > 0 {
			sold += money.GHS(margin) + " given away)"
		} else {
			sold += "nothing given away)"
		}
		lines = append(lines, sold)
		if chargedFee > 0 {
			lines = append(lines, "The buyer pays "+money.GHS(total)+" in all: item "+money.GHS(price)+" + delivery "+money.GHS(deliveryFee)+".")
		}
		lines = append(lines, "", "Ref: "+reference, "Buyer: "+from)
		switch f.Mode {
		case "delivery":
			line := "Delivery: "
			note := ""
			rawNote := ""
			if merchant != nil {
				rawNote = merchant.DeliveryNote
				note = strings.TrimRight(strings.TrimSpace(rawNote), ".")
			}
			switch {
			case note != "" && f.Location != "":
				line += note + ", to " + f.Location
			case note != "":
				line += note
			case f.Location != "":
				line += "to " + f.Location
			default:
				line += "arranged with the buyer; she gave no location"
			}
			switch {
			case paidTo == "carrier" && deliveryFee > 0:
				// Her money is the item alone: the fee is the
				// buyer's to hand to the carrier on arrival,
				// and the notice says so in as many words, so
				// she never counts it as hers.
				line += ". The " + money.GHS(deliveryFee) + " delivery is hers to pay " + carrierTarget(rawNote) + " on arrival; your money is the " + money.GHS(price) + "."
			case chargedFee > 0:
				line += ", fee " + money.GHS(deliveryFee) + " included in the total."
			default:
				line += "."
			}
			lines = append(lines, line)
		case "pickup":
			if f.Collector != "" {
				lines = append(lines, "Pickup: a collector is coming for it: "+f.Collector+".")
			} else {
				lines = append(lines, "Pickup: the buyer will come and pick it up herself.")
			}
		case "meet":
			lines = append(lines, "Meet up: the buyer wants to meet at "+f.Location+" to collect it.")
		case "arranged":
			if f.Location != "" {
				lines = append(lines, "Delivery: to be arranged with the buyer. She is in "+f.Location+".")
			} else {
				lines = append(lines, "Delivery: to be arranged with the buyer. No location given.")
			}
		}
		if f.PackageName != "" {
			lines = append(lines, "Package name: "+f.PackageName+".")
		}
		if f.Note != "" {
			lines = append(lines, "Buyer's message: "+f.Note+".")
		}
		lines = append(lines, "")
		lines = append(lines, insights.DayReportLines(day)...)
		whatsapp.SendText(merchantPhone, strings.Join(lines, "\n"))
	}
}
