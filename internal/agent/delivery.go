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
	// A reference she named is her instruction, not a hint:
	// the second audit found a named-but-unresolvable
	// reference (a foreign shop's, a mistyped one, a
	// courier's tracking number in ref shape) silently
	// falling through to the sole waiting order, which was
	// marked sent and its buyer told the goods were on
	// their way, while the order she named stayed waiting.
	namedRef := false
	if m := orderRefRe.FindStringSubmatch(msg); m != nil {
		namedRef = true
		if o := orders.Default.GetOrder(strings.ToUpper(m[1])); o != nil && o.MerchantID == shopID {
			target = o
		}
	}
	if target == nil && !namedRef && len(waiting) == 1 {
		w := waiting[0]
		target = &w
	}
	if target == nil {
		if len(waiting) == 0 {
			if namedRef {
				whatsapp.SendText(from, "I could not find that order reference, and there is no paid order waiting to be sent just now.")
			} else {
				whatsapp.SendText(from, "There is no paid order waiting to be sent just now.")
			}
			return true
		}
		var lines []string
		for _, o := range waiting {
			lines = append(lines, o.Reference+" ("+o.ProductName+")")
		}
		if namedRef {
			whatsapp.SendText(from, "I could not find that order reference among your waiting orders: "+strings.Join(lines, ", ")+". Which of these has gone?")
			return true
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

// deliveryReportTarget resolves the order a buyer's
// delivery report is about. A reference she names binds,
// exactly as on the merchant's mark-sent road: named and
// hers at this shop, it is the target; named and not
// resolvable among her paid orders here, the report is
// answered honestly and NOTHING is substituted — the
// third audit found a named older order's report landing
// on her newest sent order, closing the wrong loop.
// Unnamed, her newest paid order (in transit first) is
// the target, as it always was. handled reports whether
// the caller is done: false only when there is no target
// and nothing was said.
func deliveryReportTarget(from string, merchant *catalogue.Merchant, shopID, msg string) (target *orders.Order, handled bool) {
	if m := orderRefRe.FindStringSubmatch(msg); m != nil {
		ref := strings.ToUpper(m[1])
		if o := orders.Default.GetOrder(ref); o != nil && o.MerchantID == shopID && o.From == from && o.Status == "paid" {
			return o, true
		}
		whatsapp.SendText(from, "I could not find order "+ref+" among your paid orders at "+merchant.Name+", so I have not changed anything. Check the reference and tell me again.")
		return nil, true
	}
	// Naming the order by its goods binds it by its goods
	// (the fourth audit): the fall-through to her newest
	// order confirmed the WRONG delivery whenever she
	// named the older of two — "the bag has arrived" with
	// shoes also on the road closed the shoes' loop and
	// left the bag's open. Her paid orders whose product
	// name her message carries are the candidates: one
	// match binds it, several are put back to her by
	// reference, none leaves the newest-order reading for
	// a message that names nothing.
	lower := strings.ToLower(msg)
	var named []*orders.Order
	for _, o := range orders.Default.All() {
		oo := o
		if oo.MerchantID != shopID || oo.From != from || oo.Status != "paid" {
			continue
		}
		if name := strings.ToLower(strings.TrimSpace(oo.ProductName)); name != "" && strings.Contains(lower, name) {
			named = append(named, &oo)
		}
	}
	if len(named) == 1 {
		return named[0], true
	}
	if len(named) > 1 {
		refs := make([]string, 0, len(named))
		for _, o := range named {
			refs = append(refs, o.Reference)
		}
		whatsapp.SendText(from, "You have more than one "+named[0].ProductName+" on its way from "+merchant.Name+" ("+strings.Join(refs, ", ")+"). Tell me the reference of the one you mean, and I will mark that one.")
		return nil, true
	}
	if o := newestPaidOrderFor(shopID, from); o != nil {
		return o, true
	}
	return nil, false
}

// buyerConfirmReceived records the buyer's word that her
// paid order reached her, and tells the seller the loop is
// closed.
func buyerConfirmReceived(from string, merchant *catalogue.Merchant, shopID, msg string) bool {
	o, handled := deliveryReportTarget(from, merchant, shopID, msg)
	if o == nil {
		return handled
	}
	if o.DeliveryState == "received" {
		// Defence in depth (the seventh audit): a dispute
		// must never stand open on an order that already
		// stands received with no closing road — if one
		// does (a reopened or raced record), the receipt
		// road closes it here, inside the lock.
		ref := o.Reference
		closedHere := false
		orders.Default.Update(ref, func(x *orders.Order) {
			if x.DeliveryState == "received" && (x.DisputeState == "open" || x.DisputeState == "refund_claimed") {
				x.DisputeState = "resolved_received"
				closedHere = true
			}
		})
		if closedHere {
			whatsapp.SendText(from, "Your dispute on order "+ref+" is closed: the goods reached you, and no strike stands.")
			whatsapp.SendText(merchant.OwnerWhatsApp, "The dispute on order "+ref+" is closed: the buyer confirms the goods arrived.")
		}
		whatsapp.SendText(from, "You have already told me order "+o.Reference+" reached you. Thank you.")
		return true
	}
	if o.DeliveryState != "sent" && o.DeliveryState != "missing" {
		// Nothing can be received before it is sent: the
		// second audit found a buyer confirming receipt on
		// a never-dispatched order, closing its dispute as
		// resolved and minting the seller proven-delivery
		// credit for a delivery that never happened. A
		// MISSING order stands on the other side of that
		// line (the sixth audit): it was dispatched — a
		// missing report can only be made on a sent order
		// — and its late arrival must be confirmable, or
		// its dispute can never close clean.
		whatsapp.SendText(from, merchant.Name+" has not marked order "+o.Reference+" as sent yet, so there is nothing to confirm receiving just now. When she sends it, you will hear it from me here.")
		return true
	}
	ref := o.Reference
	// The whole receipt is decided from the stored record
	// inside the lock (the sixth audit decided the
	// dispute flip this way; the seventh found the
	// receipt itself still unconditional, stamping
	// "received" onto an order the merchant cancelled
	// between the target read and this write). The
	// receipt records only when the stored order still
	// stands paid and in a receivable state; only a
	// still-live dispute resolves; and the notices word
	// from what the write actually found.
	applied := false
	disputeClosed := false
	orders.Default.Update(ref, func(x *orders.Order) {
		if x.Status != "paid" || (x.DeliveryState != "sent" && x.DeliveryState != "missing") {
			return
		}
		applied = true
		x.DeliveryState = "received"
		x.ReceivedAt = nowStamp()
		if x.DisputeState == "open" || x.DisputeState == "refund_claimed" {
			x.DisputeState = "resolved_received"
			disputeClosed = true
		}
	})
	if !applied {
		// The order moved under the confirmation: answer
		// from the stored record, never from the snapshot.
		if cur := orders.Default.GetOrder(ref); cur != nil && cur.Status == "cancelled" {
			whatsapp.SendText(from, merchant.Name+" cancelled order "+ref+", so there is no delivery to confirm. Nothing about a receipt has been recorded.")
			return true
		}
		if cur := orders.Default.GetOrder(ref); cur != nil && cur.DeliveryState == "received" {
			whatsapp.SendText(from, "You have already told me order "+ref+" reached you. Thank you.")
			return true
		}
		whatsapp.SendText(from, "Order "+ref+" is not in a state I can record a receipt on just now. I have left it as it stands.")
		return true
	}
	whatsapp.SendText(merchant.OwnerWhatsApp, "Order "+ref+": the buyer has confirmed she received the "+o.ProductName+".")
	whatsapp.SendText(from, "Thank you. I have told "+merchant.Name+" that your order "+ref+" has arrived.")
	if disputeClosed {
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
func buyerReportMissing(from string, merchant *catalogue.Merchant, shopID, msg string) bool {
	o, handled := deliveryReportTarget(from, merchant, shopID, msg)
	if o == nil {
		return handled
	}
	if o.DeliveryState == "received" {
		whatsapp.SendText(from, "Order "+o.Reference+" stands as received. If that is wrong, tell me and I will pass it to "+merchant.Name+".")
		return true
	}
	if o.DeliveryState == "missing" {
		whatsapp.SendText(from, "You have already told me about order "+o.Reference+", and I have passed it to "+merchant.Name+".")
		return true
	}
	// A missing report on a paid order is always taken
	// and always passed to the seller: the buyer who paid
	// and received nothing must be able to say so. What
	// the seventh audit settled, on the merits, is that
	// taking the report never required advancing the
	// order's delivery state: a "missing" stamp on an
	// order the shop has not dispatched yet silently
	// removes it from every waiting-to-send worklist,
	// and no dispute opens on a bare report for the
	// dispute lane to arbitrate. So the state advance is
	// gated on dispatch — mirroring the receipt gate
	// above — while the buyer's word is never gated: an
	// undispatched order is chased, in its true position,
	// with its state untouched.
	ref := o.Reference
	if o.DeliveryState != "sent" && o.SentAt == "" {
		whatsapp.SendText(merchant.OwnerWhatsApp, "Order "+ref+": the buyer is chasing the "+o.ProductName+" she paid for; it has not been marked as sent yet. Her number is "+from+".")
		whatsapp.SendText(from, merchant.Name+" has not marked order "+ref+" as sent yet, so it is still with the shop. I have told them you are waiting for it, with your number.")
		return true
	}
	orders.Default.Update(ref, func(x *orders.Order) {
		x.DeliveryState = "missing"
	})
	whatsapp.SendText(merchant.OwnerWhatsApp, "Order "+ref+": the buyer says the "+o.ProductName+" has not arrived yet. Her number is "+from+".")
	whatsapp.SendText(from, "I am sorry it has not reached you. I have passed it to "+merchant.Name+" with your number, so they can follow it up.")
	return true
}
