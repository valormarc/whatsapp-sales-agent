package agent

// Payment has exactly one door: the payment button Stall
// issues for the order, with the Seev link under it. The
// shop's payout number is settlement-only and is never shown
// to a buyer, and there is no second route: no paying a
// number directly, and no screenshot a buyer can send that
// settles anything. A screenshot is a picture, not evidence,
// and a buyer's word is a report, not proof. When she says
// she has paid, the agent checks the link with Seev itself
// and answers from what Seev says. A payment she says went
// somewhere else — to a number she got elsewhere, in cash to
// a rider — is one Stall cannot see or confirm, and she is
// told so plainly: her order stays open until the button is
// paid, and only that payment settles it. No merchant is ever
// asked to confirm a payment; confirming is the agent's job,
// against the rail, or it does not happen.

import (
	"regexp"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/seevwebhook"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// directIntentRe reads the buyer asking for the merchant's
// number as her way to pay: "I will pay to the number", "I'll
// send it direct", "MoMo to her number", "I prefer the
// number". It is only consulted while she has an unpaid
// order, where paying can only mean paying for that order.
var directIntentRe = regexp.MustCompile(`(pay (it )?(to|into|on) (the|her|your) number|send (it )?(direct|straight)|momo (to|into|on) (the|her|your) number|to (her|your) number\b|prefer (the|that|this) number|use (the|her|that) number|pay (direct|directly)|the number (is|will be) (fine|ok)|number (is )?fine)`)

// claimRe reads a buyer saying the money has already gone: "I
// have paid", "I just sent it", "payment done", "check your
// phone". A bare "sent" is read by isClaimText as well. Questions
// are never reports: "have you sent it?" asks, it does not tell.
var claimRe = regexp.MustCompile(`(\bi have paid\b|\bi've paid\b|\bi just paid\b|\bi paid\b|\bpaid\b|\bi have sent (it|the money|payment)\b|\bi just sent it\b|\bi sent it\b|\bi have sent the money\b|\bsent the money\b|\bpayment (done|made|sent)\b|\bi have made the payment\b|\bcheck your phone\b)`)

func isClaimText(lower string) bool {
	if strings.HasSuffix(strings.TrimSpace(lower), "?") {
		return false
	}
	if controlWord(lower) == "sent" {
		return true
	}
	return claimRe.MatchString(lower)
}

// noOrderPaymentText is the plain answer to a payment report
// with no unpaid order behind it: there is nothing to check,
// and nothing is passed to any merchant.
func noOrderPaymentText(merchant *catalogue.Merchant) string {
	return "I have no unpaid order for you here just now. When you buy something here, you pay through the payment button I send you for the order, and I confirm it myself the moment it lands."
}

// handleBuyerPayment is the buyer's payment door, consulted
// once a shop is bound and before the ordinary stage
// handling: a report that she has paid, and a request for
// the shop's number, both attach to her own unpaid order in
// this shop (the one she quotes, when she quotes one) and to
// nothing else. It reports whether it took the message.
func handleBuyerPayment(from string, merchant *catalogue.Merchant, msg, lower string) bool {
	order := orders.Default.NewestUnpaidFor(merchant.ID, from)
	if qo := quotedUnpaidOrder(merchant.ID, from); qo != nil {
		order = qo
	}
	claim := isClaimText(lower)
	if order == nil {
		if claim {
			whatsapp.SendText(from, noOrderPaymentText(merchant))
			return true
		}
		return false
	}
	if claim {
		handlePaidReport(from, merchant, order, directWordsRe.MatchString(lower))
		return true
	}
	if directIntentRe.MatchString(lower) {
		// She is asking for a number Stall does not give
		// out: the button is the only way to pay, said
		// plainly, with the button itself sent again.
		sendPieces(from,
			"Payment here is only through the payment button I sent you for your order. I do not give out "+merchant.Name+"'s number for payment, and a payment made any other way cannot be confirmed here.",
			"Your order "+order.Reference+" is still open.")
		sendPayButton(from, order.Charged(), orderPaymentURL(order))
		return true
	}
	return false
}

// directWordsRe reads a buyer saying the money went somewhere
// other than the payment link: she names a number, the sending
// to it, or the direct route itself. It decides one thing only:
// what her report can mean. A payment through the link is
// verified by the agent with Seev. A payment she says went
// elsewhere is one the agent cannot see at all, and no one's
// word — hers or the merchant's — is asked to settle it here.
var directWordsRe = regexp.MustCompile(`(?i)(momo\s+number|your\s+number|her\s+number|the\s+shop'?s\s+number|sent\s+(it|the\s+money)\s+to|paid\s+(it\s+)?(to|into)|directly|to\s+the\s+number)`)

// handlePaidReport answers a buyer who says she has paid.
// A report about the link is checked against Seev itself and
// answered from the result. A report about money sent
// elsewhere is answered with the plain truth: Stall cannot
// see it or confirm it, a screenshot cannot change that, and
// her order stays open until the button is paid.
func handlePaidReport(from string, merchant *catalogue.Merchant, order *orders.Order, direct bool) {
	if fresh := orders.Default.GetOrder(order.Reference); fresh != nil && fresh.Status == "paid" {
		whatsapp.SendText(from, "That one is already confirmed paid. Thank you.")
		return
	}
	if direct {
		sendPieces(from,
			"That payment went outside the payment button I sent you, so I cannot check it or confirm it from here, and a screenshot cannot confirm it either.",
			"Your order "+order.Reference+" is still open. To complete it, pay through the button for the order, and I will confirm it myself the moment it lands.")
		sendPayButton(from, order.Charged(), orderPaymentURL(order))
		return
	}
	if order.ProviderRef != nil && *order.ProviderRef != "" {
		verifyLinkPayment(from, merchant, order)
		return
	}
	whatsapp.SendText(from, "I have no payment session to check for order "+order.Reference+" yet. Use the payment button I sent you for the order, and I will confirm it myself the moment it lands.")
}

// verifyLinkPayment checks a buyer's "I have paid" against
// Seev's own read-back of her checkout session, and answers
// both sides from the result. Paid and matching settles the
// order exactly as the webhook settles it, with the agent
// confirming to the buyer and alerting the merchant. Not yet
// paid is said plainly, with the order left open and the
// merchant left alone. Failed is said plainly too, with the
// link offered again. An unreachable Seev is admitted, never
// guessed around.
func verifyLinkPayment(from string, merchant *catalogue.Merchant, order *orders.Order) {
	charged := order.Charged()
	check := checkout.VerifyPayment(*order.ProviderRef)
	switch {
	case check == nil:
		whatsapp.SendText(from, "I could not reach the payment service to check just now. The moment it confirms your payment I will tell you, and "+merchant.Name+" will hear it from me at the same time.")
	case checkout.IsPaidStatus(check.Status) && check.AmountMajor != nil && money.Round(*check.AmountMajor*100) != money.Round(charged*100):
		whatsapp.SendText(from, "I have checked with the payment service. A payment came through, but the amount does not match your order, so I have told "+merchant.Name+" and we are looking at it together. Your order is not settled yet.")
		if phone := merchantNotifyPhone(merchant); phone != "" {
			whatsapp.SendText(phone, "Payment amount mismatch on order "+order.Reference+": the order is "+money.GHS(charged)+" but Seev reports "+money.GHS(*check.AmountMajor)+". The buyer reported she had paid; I have not marked it paid. Please check the Seev record with me.")
		}
	case checkout.IsPaidStatus(check.Status) && check.AmountMajor == nil:
		// Paid, but the read-back states no amount: that
		// proves nothing about the amount, so the order is
		// not settled on it. The buyer is told it is being
		// looked at, and the merchant is told the fact.
		whatsapp.SendText(from, "I have checked with the payment service. Your payment shows as received, but the service did not state its amount, so I have asked "+merchant.Name+" to confirm it with me before your order is settled.")
		if phone := merchantNotifyPhone(merchant); phone != "" {
			whatsapp.SendText(phone, "Payment read-back without an amount on order "+order.Reference+": Seev reports the session paid but states no amount, so I have not marked the order of "+money.GHS(charged)+" paid. Please check the Seev record with me.")
		}
	case checkout.PaidAmountMatches(check, charged):
		seevwebhook.SettleVerified(order, "I have checked with the payment service, and you are right: your payment for the "+order.ProductName+" has come through, confirmed at "+money.GHS(charged)+". "+merchant.Name+" has your order and will take it from here.")
	case checkout.IsFailedStatus(check.Status):
		sendPieces(from,
			"I have checked with the payment service, and that payment did not go through, so nothing was charged.",
			"Your order "+order.Reference+" is still open. You can try again:",
		)
		sendPayButton(from, order.Charged(), orderPaymentURL(order))
	default:
		whatsapp.SendText(from, "I have checked with the payment service, and your payment has not come through yet. Your order "+order.Reference+" for the "+order.ProductName+" is still open. If the money has only just left your phone, give it a small moment and tell me again, and I will check again.")
	}
}
