// The confirmation step: warm words ("I'll buy") are not an
// order, so the buyer is quoted the price and asked plainly,
// and only her clear yes closes.
package agent

import (
	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// restatesQuotedPrice reports whether the buyer's words name the
// very figure she was quoted at the confirmation step: the close
// price itself, or, where the merchant takes the delivery money,
// the close price plus the delivery fee that was quoted beside
// it. Any other figure is not a confirmation: it is re-asked
// about, the way any other unclear answer is.
func restatesQuotedPrice(msg string, s *Session, merchant *catalogue.Merchant) bool {
	offer := parseOffer(msg)
	if offer == nil {
		return false
	}
	if *offer == s.ClosePrice {
		return true
	}
	if deliveryPaidToOf(merchant) == "merchant" {
		if fee := deliveryFeeOf(merchant); fee > 0 && *offer == s.ClosePrice+fee {
			return true
		}
	}
	return false
}

// clearPendingClose ends an open confirmation question without a
// sale: the item goes back to being the one she was viewing, and no
// haggle product is left sitting on a browsing session.
func clearPendingClose(s *Session) {
	s.PendingClose = false
	s.CloseReasked = false
	if s.CloseFromView && s.Product != nil {
		// The view goes back to the item as listed: a measured
		// close borrowed a scaled copy, and the shelf must never
		// quote the amount's price as the item's afterwards. A
		// close borrowed from a live haggle keeps the haggle, and
		// its quantity, exactly as they were.
		if s.Qty > 0 {
			s.LastViewed = wholeProductOf(s.MerchantID, s.Product)
		} else {
			s.LastViewed = s.Product
		}
		s.Product = nil
		s.Qty, s.Unit = 0, ""
	}
	s.CloseFromView = false
}

// startConfirmation opens the explicit confirmation step for a buyer
// who has said the warm words without a price agreement: she is
// quoted the price (with the delivery fee named as what it is, a
// charge that only applies if the goods are sent) and asked
// plainly. No final total is promised here, because how the goods
// reach her is not settled yet; only her clear yes at this step
// moves on, into the fulfilment step, where the total is settled.
func startConfirmation(from string, s *Session, merchant *catalogue.Merchant, product *catalogue.Product, price float64, fromView bool) {
	s.Product = product
	s.PendingClose = true
	s.ClosePrice = price
	s.CloseReasked = false
	s.CloseFromView = fromView
	sessions[sessionKey(s.MerchantID, from)] = s
	// A measured close quotes the amount, not the whole item:
	// the session's quantity names it ("3 yards of ...").
	name := product.Name
	if s.Qty > 0 && product.Unit != "" {
		name = qtyLabel(product, s.Qty)
	}
	if fee := deliveryFeeOf(merchant); fee > 0 {
		if deliveryPaidToOf(merchant) == "carrier" {
			// The fee is not hers to pay here at all: it goes
			// to whoever brings the item, when it arrives.
			sendPieces(from,
				"The "+name+" is "+money.GHS(price)+". If you want it sent, delivery is "+money.GHS(fee)+", paid to "+carrierTarget(merchant.DeliveryNote)+" when it reaches you.",
				"Shall I send the payment link?")
			return
		}
		sendPieces(from,
			"The "+name+" is "+money.GHS(price)+", plus "+money.GHS(fee)+" delivery if you want it sent to you.",
			"Shall I send the payment link?")
		return
	}
	sendPieces(from,
		"The "+name+" is "+money.GHS(price)+".",
		"Shall I send the payment link?")
}

// answerDeliveryOrTotal answers a delivery question, or the demand
// for one total, for the item in play (the haggle's product at its
// standing price, else the item in view at its list price). It
// reports whether it answered; with no item in play it says nothing
// and the conversation continues elsewhere. Haggle state is never
// touched: asking about delivery does not move a price.
func answerDeliveryOrTotal(from string, s *Session, merchant *catalogue.Merchant, lower string) bool {
	p := catalogue.FindProduct(s.MerchantID, lower)
	named := p != nil
	if p == nil {
		p = s.Product
	}
	if p == nil {
		p = s.LastViewed
	}
	if p == nil {
		return false
	}
	if named {
		s.LastViewed = p
	}
	price := p.ListPrice
	if s.Product != nil && p.ID == s.Product.ID && s.Ask > 0 {
		price = s.Ask
	}
	fee := deliveryFeeOf(merchant)
	carrier := fee > 0 && deliveryPaidToOf(merchant) == "carrier"
	if totalRe.MatchString(lower) {
		switch {
		case carrier:
			// Both purses, plainly: what she pays here, what
			// she hands over on arrival, and the two together,
			// so she still gets one clear picture and never
			// has to add anything up herself.
			whatsapp.SendText(from, "With me the "+p.Name+" is "+money.GHS(price)+". Delivery is "+money.GHS(fee)+", paid to "+carrierTarget(merchant.DeliveryNote)+" when it reaches you, so "+money.GHS(price+fee)+" in all.")
		case fee > 0:
			whatsapp.SendText(from, "Your total is "+money.GHS(price+fee)+": the "+p.Name+" at "+money.GHS(price)+", plus "+money.GHS(fee)+" delivery.")
		default:
			whatsapp.SendText(from, "The "+p.Name+" is "+money.GHS(price)+". Delivery is arranged with the seller, so there is no fixed delivery fee on top.")
		}
		if parked := basketGoods(s); parked > 0 && s.Product != nil && p.ID == s.Product.ID {
			// She is pricing this item while others already sit
			// agreed in the same order: the whole picture is
			// owed too, or the total she hears is a part dressed
			// as the whole.
			line := "With the " + basketNames(s) + " already in your order, your items come to " + money.GHS(parked+price)
			if fee > 0 && !carrier {
				line += ", " + money.GHS(parked+price+fee) + " with delivery"
			}
			whatsapp.SendText(from, line+".")
		}
		return true
	}
	if fee > 0 {
		if carrier {
			sendPieces(from, deliveryOptionLine(merchant, ""),
				"The "+p.Name+" itself is "+money.GHS(price)+" with me.")
			return true
		}
		line := "Delivery is " + money.GHS(fee) + "."
		if merchant.DeliveryNote != "" {
			line = merchant.DeliveryNote + " Delivery is " + money.GHS(fee) + "."
		}
		sendPieces(from, line,
			"So the "+p.Name+" comes to "+money.GHS(price+fee)+" in total.")
	} else {
		whatsapp.SendText(from, "Delivery is arranged with the seller directly, so there is no fixed fee here.")
	}
	return true
}
