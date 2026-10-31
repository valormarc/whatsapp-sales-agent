// The confirmation step: warm words ("I'll buy") are not an
// order, so the buyer is quoted the price and asked plainly,
// and only her clear yes closes.
package agent

import (
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
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
	// A confirmation opened on a different product than
	// the session's standing sale starts a NEW purchase:
	// the old sale's identity is shed here (the sixth
	// audit), so beginFulfilment can never treat a
	// foreign SaleID as this purchase's own.
	if s.Product != nil && s.Product.ID != product.ID {
		s.SaleID = ""
		s.PaymentURL = ""
	}
	s.Product = product
	s.PendingClose = true
	s.ClosePrice = price
	s.CloseReasked = false
	s.CloseFromView = fromView
	sessions.put(sessionKey(s.MerchantID, from), s)
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
				"The "+name+" is "+money.GHS(price)+". If you want it sent, delivery is "+money.GHS(fee)+", paid to "+carrierTarget(merchant.DeliveryNote)+" when it reaches you.")
			askYesNo(from, s, "close", product.Name, "Shall I send the payment link?", "Yes, send it", "Not yet")
			return
		}
		sendPieces(from,
			"The "+name+" is "+money.GHS(price)+", plus "+money.GHS(fee)+" delivery if you want it sent to you.")
		askYesNo(from, s, "close", product.Name, "Shall I send the payment link?", "Yes, send it", "Not yet")
		return
	}
	sendPieces(from,
		"The "+name+" is "+money.GHS(price)+".")
	askYesNo(from, s, "close", product.Name, "Shall I send the payment link?", "Yes, send it", "Not yet")
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
	if named && p.AvailableUnits() <= 0 && (s.Product == nil || p.ID != s.Product.ID) {
		// A named product that has sold out gets no total
		// and no place in her view (the seventh audit);
		// the in-play product of an open sale is the one
		// exception, and it is not this branch.
		whatsapp.SendText(from, "The "+p.Name+" has sold out, so there is no total I can quote for it just now.")
		return true
	}
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
	// A quote-style shop's retained set fee is NOT the
	// delivery price — the platform itself tells the
	// seller it is not being used — so it is never quoted
	// to a buyer here as a definite figure or folded into
	// a definite total (the sixth audit). The per-order
	// quote path states the real fee when it comes.
	if merchant != nil && deliveryStyleOf(merchant) == "quote" {
		if totalRe.MatchString(lower) {
			var lines []string
			goods := 0.0
			if parked := basketGoods(s); parked > 0 && s.Product != nil && p.ID == s.Product.ID {
				for _, it := range s.Basket {
					lines = append(lines, it.Product.Name+": "+money.GHS(it.AgreedPrice))
					goods += it.AgreedPrice
				}
			}
			lines = append(lines, p.Name+": "+money.GHS(price))
			goods += price
			lines = append(lines, "Delivery: the seller quotes it for each order, when you order.")
			lines = append(lines, "Total: "+money.GHS(goods)+" plus the delivery quote.")
			whatsapp.SendText(from, strings.Join(lines, "\n"))
			return true
		}
		whatsapp.SendText(from, "Delivery is quoted by "+merchant.Name+" for each order, so there is no set fee I can give you here. The "+p.Name+" itself is "+money.GHS(price)+" with me.")
		return true
	}
	// The fee is the one the order for her stated place
	// will actually charge (the fifth audit: the total
	// record priced delivery at the standard fee even
	// when her place carries the shop's area fee, so the
	// quoted record and the settled order disagreed).
	fee := deliveryFeeFor(merchant, s.Location)
	carrier := fee > 0 && deliveryPaidToOf(merchant) == "carrier"
	if totalRe.MatchString(lower) {
		// The total she asks for is a record: one message on
		// the fixed paper, her parked items folded in as
		// their own lines when the item in play is the one
		// already being bought, so the figure she hears is
		// never a part dressed as the whole.
		items := []orders.OrderItem{}
		if parked := basketGoods(s); parked > 0 && s.Product != nil && p.ID == s.Product.ID {
			for _, it := range s.Basket {
				items = append(items, orders.OrderItem{Name: it.Product.Name, Price: it.AgreedPrice})
			}
		}
		items = append(items, orders.OrderItem{Name: p.Name, Price: price})
		chargedFee := fee
		if carrier {
			chargedFee = 0
		}
		mode := "delivery"
		note := ""
		if merchant != nil {
			note = merchant.DeliveryNote
		}
		whatsapp.SendText(from, totalRecord(items, fee, chargedFee, mode, s.Location, deliveryPaidToOf(merchant), note))
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
