package agent

// Disputes: a buyer who says she was cheated can freeze
// her order inside Stall, in the chat, with her own words.
//
// What a freeze honestly is, and is not. Money paid through
// the payment button is released by Seev to the shop's MoMo
// the next day, by the rail itself; Stall has no escrow
// lever and cannot pull back money already released, and
// the buyer is told exactly that, never a pretend hold.
// What Stall does control, it freezes completely: the
// order is marked disputed on the shop's record, the
// seller is told at once with the buyer's own words, and
// the clock starts. The goods arriving closes it clean. A
// refund the seller claims is confirmed with the buyer
// herself before it counts. Three days unresolved and the
// dispute becomes a strike against the seller's number
// (see trust.go); three strikes flag the number, and a
// flagged seller's sales are gated and warned about. The
// record is the punishment the platform can truly give,
// and it follows the number across every shop it runs.

import (
	"regexp"
	"strings"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// disputeLimit is how long a dispute may sit unresolved
// before it becomes a strike.
const disputeLimit = 72 * time.Hour

// disputeRe is the no-model net for a dispute: the words of
// being cheated, with a paid order behind them. Questions
// are not accusations. The thinking reads the rest (see
// the order_dispute act in understand.go).
var disputeRe = regexp.MustCompile(`(?i)(dispute|scam|cheated|fraud|took my money|never sent (it|the|my|them)|did not send|didn't send|didnt send)`)

func isDisputeText(lower string) bool {
	if strings.HasSuffix(strings.TrimSpace(lower), "?") {
		return false
	}
	return disputeRe.MatchString(lower)
}

// openDispute freezes the order on Stall's side and tells
// both sides the whole position, the rail's limit included.
func openDispute(from string, merchant *catalogue.Merchant, order *orders.Order, words string) {
	if order.DisputeState == "open" || order.DisputeState == "refund_claimed" {
		whatsapp.SendText(from, "Your dispute on order "+order.Reference+" is already open, and the order is frozen on Stall's side. If the goods reach you, tell me they have arrived and I will close it.")
		return
	}
	orders.Default.Update(order.Reference, func(o *orders.Order) {
		o.DisputeState = "open"
		o.DisputeBy = from
		o.DisputeAt = nowStamp()
	})
	sendPieces(from,
		"I have opened a dispute on order "+order.Reference+" and frozen it on Stall's side. I have told "+merchant.Name+" exactly what you said.",
		"The honest position: money paid through the payment button is released by the payment service to the shop's MoMo the next day, and Stall cannot pull back money that has already been released. What Stall controls is frozen: the order is marked disputed on the shop's record, and if it is still unresolved in 3 days it becomes a strike against the shop.",
		"If the goods reach you, tell me they have arrived and I will close the dispute at once. If the shop refunds you, tell me here.")
	if phone := merchantNotifyPhone(merchant); phone != "" {
		whatsapp.SendText(phone, "Dispute opened\n\nOrder "+order.Reference+": the "+order.ProductName+", "+money.GHS(order.Charged())+", buyer "+from+".\nShe says: \""+words+"\"\nThe order is frozen on Stall's side and the dispute is on your record. If the goods are on their way, say so here and I will tell her. If you refund her in full, tell me: I have refunded order "+order.Reference+". If it is still unresolved in 3 days, it becomes a strike.")
	}
}

// disputeTarget is the order a dispute from this buyer in
// this shop is about: the one she quotes, else her newest
// paid order that has not been confirmed received. An order
// the shop cancelled after she had paid still counts: the
// money moved, so the dispute road the cancellation message
// promised her has to exist.
func disputeTarget(shopID, from string) *orders.Order {
	if qo := quotedOrderFor(shopID, from); qo != nil && (qo.Status == "paid" || (qo.Status == "cancelled" && qo.PaymentPath != "")) {
		return qo
	}
	if o := newestPaidOrderFor(shopID, from); o != nil {
		return o
	}
	var newest *orders.Order
	for _, o := range orders.Default.All() {
		oo := o
		if oo.MerchantID == shopID && oo.From == from && oo.Status == "cancelled" && oo.PaymentPath != "" {
			newest = &oo
		}
	}
	return newest
}

// handleDisputeConfirm runs while a buyer owes an answer on
// a claimed refund: yes closes the dispute clean, no closes
// it with a strike. It reports whether it took the message.
func handleDisputeConfirm(from string, s *Session, merchant *catalogue.Merchant, msg, lower string) bool {
	ref := s.DisputeConfirmRef
	if ref == "" {
		return false
	}
	order := orders.Default.GetOrder(ref)
	if order == nil || order.DisputeState != "refund_claimed" {
		s.DisputeConfirmRef = ""
		sessions[sessionKey(s.MerchantID, from)] = s
		return false
	}
	sellerPhone := ""
	if m := catalogue.GetMerchant(order.MerchantID); m != nil {
		sellerPhone = merchantNotifyPhone(m)
	}
	switch {
	case isClearYes(lower):
		s.DisputeConfirmRef = ""
		sessions[sessionKey(s.MerchantID, from)] = s
		orders.Default.Update(ref, func(o *orders.Order) { o.DisputeState = "resolved_refund" })
		whatsapp.SendText(from, "Thank you. I have closed the dispute on order "+ref+": the shop's refund reached you, and no strike stands.")
		if sellerPhone != "" {
			whatsapp.SendText(sellerPhone, "The buyer confirms your refund for order "+ref+". The dispute is closed, with no strike.")
		}
		return true
	case isClearNo(lower):
		s.DisputeConfirmRef = ""
		sessions[sessionKey(s.MerchantID, from)] = s
		orders.Default.Update(ref, func(o *orders.Order) { o.DisputeState = "struck" })
		owner := ""
		if m := catalogue.GetMerchant(order.MerchantID); m != nil {
			owner = m.OwnerWhatsApp
		}
		strikes := 0
		if owner != "" {
			strikes = addStrike(owner)
		}
		whatsapp.SendText(from, "I have recorded that the refund did not reach you. The dispute on order "+ref+" is closed with a strike on the shop's record.")
		if sellerPhone != "" {
			whatsapp.SendText(sellerPhone, "The buyer says your refund for order "+ref+" did not reach her. The dispute is closed with a strike on your record ("+strikeCountWords(strikes)+"). Strikes follow your number across every shop you run here; three strikes flag it, and a flagged shop's buyers are warned and its sales are confirmed in hand before payment.")
		}
		return true
	default:
		whatsapp.SendText(from, "Please say yes or no: have you received the refund for order "+ref+"?")
		return true
	}
}

func strikeCountWords(n int) string {
	switch n {
	case 1:
		return "strike 1 of 3"
	case 2:
		return "strike 2 of 3"
	default:
		return "3 strikes: the number is now flagged"
	}
}

var refundClaimRe = regexp.MustCompile(`(?i)(i have refunded|i refunded|refunded (her|him|the buyer|them))`)

// handleMerchantRefundClaim is the seller's word that she
// refunded a disputed order. It is never taken on her word
// alone: the buyer is asked to confirm, and only her yes
// closes the dispute clean (see handleDisputeConfirm).
func handleMerchantRefundClaim(from string, merchant *catalogue.Merchant, msg, lower string) bool {
	if !refundClaimRe.MatchString(lower) {
		return false
	}
	order := orderFromMessage(merchant.ID, msg)
	if order == nil {
		order = focusOrder(merchant.ID)
	}
	if order == nil || order.DisputeState != "open" {
		return false
	}
	orders.Default.Update(order.Reference, func(o *orders.Order) { o.DisputeState = "refund_claimed" })
	buyer := order.From
	key := sessionKey(merchant.ID, buyer)
	sess := sessions[key]
	if sess == nil {
		sess = &Session{Stage: "browsing", MerchantID: merchant.ID}
	}
	sess.DisputeConfirmRef = order.Reference
	sessions[key] = sess
	sendYesNo(buyer, merchant.Name+" says she has refunded "+money.GHS(order.Charged())+" for order "+order.Reference+". Have you received the refund?", "Yes, received", "Not yet")
	whatsapp.SendText(from, "I have asked the buyer to confirm the refund for order "+order.Reference+". Her word closes the dispute.")
	return true
}

// CheckDisputes is the dispute clock, run by the watch's
// sweep: a dispute still open past the limit becomes a
// strike against the seller's number, and both sides hear
// it happen.
func CheckDisputes(now time.Time) {
	for _, o := range orders.Default.All() {
		if o.DisputeState != "open" || o.DisputeAt == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, o.DisputeAt)
		if err != nil || now.Sub(at) < disputeLimit {
			continue
		}
		ref := o.Reference
		orders.Default.Update(ref, func(x *orders.Order) { x.DisputeState = "struck" })
		merchant := catalogue.GetMerchant(o.MerchantID)
		strikes := 0
		if merchant != nil && merchant.OwnerWhatsApp != "" {
			strikes = addStrike(merchant.OwnerWhatsApp)
		}
		whatsapp.SendText(o.From, "Your dispute on order "+ref+" was never resolved, so it has become a strike on the shop's record. I am sorry the goods and the refund both failed to reach you.")
		if merchant != nil {
			if phone := merchantNotifyPhone(merchant); phone != "" {
				whatsapp.SendText(phone, "The dispute on order "+ref+" was never resolved, so it has become a strike on your record ("+strikeCountWords(strikes)+"). Strikes follow your number across every shop you run here; three strikes flag it.")
			}
		}
	}
}
