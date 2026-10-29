package agent

// The delivery loop. A sale does not end when the money
// lands; the goods still have to move, and silence after
// payment is what makes buyers chase a shop. So the loop is
// closed inside the same two chats: the seller says in her
// own words that the goods have gone (or are ready), the
// buyer is told; the buyer says in her own words that they
// arrived, the seller is told; and a buyer whose goods have
// not arrived is passed to the seller with her number, the
// way a payment claim is. Every move is read by meaning (the
// order_sent, order_received and order_missing acts in
// understand.go) and executed here against the book: only
// the shop's own orders can be marked, only a paid order can
// be sent, and only the buyer of record can confirm or
// complain. The states live on the order itself
// (orders.Order.DeliveryState), so a restart loses nothing.

import (
	"strings"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

func nowStamp() string { return time.Now().UTC().Format(time.RFC3339) }

// newestPaidOrderFor is the buyer's paid order at this shop
// that her words about "my order" mean once money has moved:
// the one in transit when there is one (the order that can
// arrive, or fail to), otherwise her newest paid order.
func newestPaidOrderFor(shopID, from string) *orders.Order {
	var newest, inTransit *orders.Order
	for _, o := range orders.Default.All() {
		oo := o
		if oo.MerchantID == shopID && oo.From == from && oo.Status == "paid" {
			newest = &oo
			if oo.DeliveryState == "sent" {
				inTransit = &oo
			}
		}
	}
	if inTransit != nil {
		return inTransit
	}
	return newest
}

// paidOrdersOf lists the shop's paid orders in book order.
func paidOrdersOf(shopID string) []orders.Order {
	var out []orders.Order
	for _, o := range orders.Default.All() {
		if o.MerchantID == shopID && o.Status == "paid" {
			out = append(out, o)
		}
	}
	return out
}

// merchantMarkSent executes the owner's word that the goods
// for a paid order have gone out (or are ready to collect).
// The order is the one she names by reference, else the one
// in focus, else the only one waiting; with several waiting
// and none named, she is asked which, once.
func merchantMarkSent(from string, merchant *catalogue.Merchant, shopID, msg string) bool {
	var waiting []orders.Order
	for _, o := range paidOrdersOf(shopID) {
		if o.DeliveryState == "" {
			waiting = append(waiting, o)
		}
	}
	var target *orders.Order
	if m := orderRefRe.FindStringSubmatch(msg); m != nil {
		if o := orders.Default.GetOrder(strings.ToUpper(m[1])); o != nil && o.MerchantID == shopID {
			target = o
		}
	}
	if target == nil && len(waiting) == 1 {
		w := waiting[0]
		target = &w
	}
	if target == nil {
		if len(waiting) == 0 {
			whatsapp.SendText(from, "There is no paid order waiting to be sent just now.")
			return true
		}
		var lines []string
		for _, o := range waiting {
			lines = append(lines, o.Reference+" ("+o.ProductName+")")
		}
		whatsapp.SendText(from, "Which order has gone? "+strings.Join(lines, ", ")+". Tell me the reference and I will mark it.")
		return true
	}
	if target.Status != "paid" {
		whatsapp.SendText(from, "Order "+target.Reference+" has not been paid yet, so it cannot be marked as sent.")
		return true
	}
	switch target.DeliveryState {
	case "sent":
		whatsapp.SendText(from, "Order "+target.Reference+" is already marked as sent.")
		return true
	case "received":
		whatsapp.SendText(from, "Order "+target.Reference+" is already confirmed received by the buyer.")
		return true
	}
	ref := target.Reference
	orders.Default.Update(ref, func(o *orders.Order) {
		o.DeliveryState = "sent"
		o.SentAt = nowStamp()
	})
	buyerLine := merchant.Name + " has sent your order " + ref + " (" + target.ProductName + "). It is on its way to you."
	if target.Fulfilment == "pickup" || target.Fulfilment == "meet" {
		buyerLine = "Your order " + ref + " (" + target.ProductName + ") at " + merchant.Name + " is ready. " + fulfilmentReadyLine(target)
	}
	whatsapp.SendText(target.From, buyerLine)
	whatsapp.SendText(from, "Marked as sent: "+ref+" ("+target.ProductName+"). I have told the buyer.")
	return true
}

// fulfilmentReadyLine says how a ready order is collected,
// from the order's own record.
func fulfilmentReadyLine(o *orders.Order) string {
	if o.Fulfilment == "pickup" {
		if o.Collector != "" {
			return o.Collector + " can come and collect it."
		}
		return "You can come and collect it."
	}
	if o.Location != "" {
		return "The meeting place is " + o.Location + "."
	}
	return ""
}

// buyerConfirmReceived records the buyer's word that her
// paid order reached her, and tells the seller the loop is
// closed.
func buyerConfirmReceived(from string, merchant *catalogue.Merchant, shopID string) bool {
	o := newestPaidOrderFor(shopID, from)
	if o == nil {
		return false
	}
	if o.DeliveryState == "received" {
		whatsapp.SendText(from, "You have already told me order "+o.Reference+" reached you. Thank you.")
		return true
	}
	ref := o.Reference
	wasDisputed := o.DisputeState == "open" || o.DisputeState == "refund_claimed"
	orders.Default.Update(ref, func(x *orders.Order) {
		x.DeliveryState = "received"
		x.ReceivedAt = nowStamp()
		if wasDisputed {
			x.DisputeState = "resolved_received"
		}
	})
	whatsapp.SendText(merchant.OwnerWhatsApp, "Order "+ref+": the buyer has confirmed she received the "+o.ProductName+".")
	whatsapp.SendText(from, "Thank you. I have told "+merchant.Name+" that your order "+ref+" has arrived.")
	if wasDisputed {
		// The goods arriving closes a live dispute clean:
		// no strike, and both sides hear that it closed.
		whatsapp.SendText(from, "Your dispute on order "+ref+" is closed: the goods reached you, and no strike stands.")
		whatsapp.SendText(merchant.OwnerWhatsApp, "The dispute on order "+ref+" is closed: the buyer confirms the goods arrived.")
	}
	return true
}

// buyerReportMissing passes a buyer's word that her paid
// order has not arrived to the seller, with the buyer's
// number, so the seller can chase it the way she chases a
// rider or a station herself.
func buyerReportMissing(from string, merchant *catalogue.Merchant, shopID string) bool {
	o := newestPaidOrderFor(shopID, from)
	if o == nil {
		return false
	}
	if o.DeliveryState == "received" {
		whatsapp.SendText(from, "Order "+o.Reference+" stands as received. If that is wrong, tell me and I will pass it to "+merchant.Name+".")
		return true
	}
	if o.DeliveryState == "missing" {
		whatsapp.SendText(from, "You have already told me about order "+o.Reference+", and I have passed it to "+merchant.Name+".")
		return true
	}
	ref := o.Reference
	orders.Default.Update(ref, func(x *orders.Order) {
		x.DeliveryState = "missing"
	})
	whatsapp.SendText(merchant.OwnerWhatsApp, "Order "+ref+": the buyer says the "+o.ProductName+" has not arrived yet. Her number is "+from+".")
	whatsapp.SendText(from, "I am sorry it has not reached you. I have passed it to "+merchant.Name+" with your number, so they can follow it up.")
	return true
}
