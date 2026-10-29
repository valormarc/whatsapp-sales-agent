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
	// The order's goods are the pending item plus everything
	// parked in the basket, each settled on its own terms;
	// the delivery fee rides once, on the whole order.
	items := orderItemsOf(s, f, price)
	goods := basketGoods(s) + price
	multi := len(items) > 1
	total := goods + chargedFee
	link := checkout.CreatePaymentLink(checkout.LinkRequest{Amount: total, Reference: reference, Subaccount: merchant.SubaccountCode})
	linkAttempts := 1
	if link.URL == nil && checkout.Enabled() {
		// A dead first key must not strand a closed sale.
		// Seev remembers idempotency keys beyond our own
		// books: a reference minted again after a store
		// reset collides with the session it named in a
		// former era, at another amount (a 409), and its
		// old attempt keys are burned the same way, so
		// walking them heals nothing. The retry rides a
		// key that has never existed, and the order points
		// at the session that actually opened, so
		// settlement still finds it.
		if retry := checkout.CreatePaymentLink(checkout.LinkRequest{Amount: total, Reference: reference, IdempotencyKey: checkout.FreshKey(reference), Subaccount: merchant.SubaccountCode}); retry.URL != nil {
			link = retry
			linkAttempts = 2
		}
	}
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
	orderName := p.Name
	var orderLines []orders.OrderItem
	if multi {
		orderName = itemsLabel(items)
		orderLines = items
	}
	orders.Default.RecordOrder(reference, orders.Order{
		From:           from,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		ProductName:    orderName,
		Items:          orderLines,
		Price:          goods,
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
		Size:           f.Size,
		DeliveryPaidTo: paidTo,
		Quantity:       s.Qty,
		Unit:           s.Unit,
		LinkAttempts:   linkAttempts,
	})
	merchantPhone := merchantNotifyPhone(merchant)
	// Seev was configured but the session could not be created: say so,
	// to both sides, rather than send a link that does not work. No
	// sale is booked in the day book for a deal the customer cannot pay
	// for.
	if link.URL == nil {
		sessions[sessionKey(shopID, from)] = &Session{Stage: "browsing", MerchantID: shopID, Location: s.Location}
		whatsapp.SendText(from, "We have a deal at "+money.GHS(goods)+" for the "+orderName+", but the payment link is not coming up right now. The shop has been told and will reach out here to complete your order.")
		if merchantPhone != "" {
			whatsapp.SendText(merchantPhone, "Payment link failed for an agreed deal: "+orderName+" at "+money.GHS(goods)+", buyer "+from+". Ref: "+reference+". Please reach out to the buyer to complete it.")
		}
		return
	}
	url := *link.URL
	// The day book books the item prices alone (see the delivery
	// note above): each line at its own agreed price, giveaway
	// measured against its own list, delivery money nowhere in
	// the day's figures.
	var day daybook.State
	if multi {
		for _, it := range s.Basket {
			day = daybook.Default.RecordSale(shopID, daybook.Sale{
				ListPrice:   it.Product.ListPrice,
				AgreedPrice: it.AgreedPrice,
				Allowance:   merchant.DailyGiveawayAllowance,
			})
		}
	}
	day = daybook.Default.RecordSale(shopID, daybook.Sale{
		ListPrice:   p.ListPrice,
		AgreedPrice: price,
		Allowance:   merchant.DailyGiveawayAllowance,
	})
	sessions[sessionKey(shopID, from)] = &Session{Stage: "agreed", MerchantID: shopID, Product: p, AgreedPrice: price, PaymentURL: url, Location: s.Location}
	// The completion goes out in the two families. The total
	// is a record: one message on the fixed paper, never split
	// (see records.go). What happens with the goods is talk,
	// in pieces the way a person types it, before it; the
	// package read-backs and the link follow it. The deal line
	// itself was already sent when the price was agreed
	// (beginFulfilment).
	var talk []string
	switch f.Mode {
	case "pickup":
		if f.Collector != "" {
			talk = append(talk, "Noted. I have told the seller that "+f.Collector+" is coming for it.")
		}
	case "meet":
		// Honest wording, no guarantees beyond the sale: the
		// seller has been told the place and it is on the order;
		// whether she comes to it is hers to say.
		talk = append(talk, "I have told the seller you want to meet at "+f.Location+" to collect it. The place is on your order.")
	case "arranged":
		talk = append(talk, "Delivery will be arranged with the seller.")
	}
	sendPieces(from, talk...)
	note := ""
	if merchant != nil {
		note = merchant.DeliveryNote
	}
	whatsapp.SendText(from, totalRecord(items, deliveryFee, chargedFee, f.Mode, f.Location, paidTo, note))
	var pieces []string
	// The package details she gave ride with the order, and she
	// hears the name read back so a wrong name is caught here,
	// not at the rider's door.
	if f.Size != "" && !multi {
		pieces = append(pieces, "Size: "+f.Size+".")
	}
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
	sendPieces(from, pieces...)
	// The link itself arrives as a tap-to-pay button with
	// the total on its face, not as a raw address in the chat.
	sendPayButton(from, total, url)
	if merchantPhone != "" {
		// The notice is the seller's receipt for the sale, and
		// it is a record: one message on the fixed paper, the
		// same lines in the same order every time (see
		// records.go), filled from the order's own facts.
		if o := orders.Default.GetOrder(reference); o != nil {
			whatsapp.SendText(merchantPhone, saleNoticeRecord(o, merchant, day))
		}
	}
}
