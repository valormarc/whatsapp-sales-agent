package agent

// The direct payment path: the second door beside the Seev link
// (both paths built).
//
// The link path confirms itself: Seev's signed webhook settles
// the order and the merchant hears her exact figures. The direct
// path is how Ghana already buys every day: the buyer sends MoMo
// straight to the merchant's own number and sends the transfer
// screenshot into the chat as proof. But a screenshot is a CLAIM,
// never proof (the BizHub rule: a screenshot is a claim, not
// evidence). So the agent forwards the claim to the merchant of
// that shop, with the amount, the buyer and the order reference,
// plus the screenshot itself, and only the merchant's own word
// settles it. On her yes the order becomes paid, settled by her,
// with no Seev fee (she received the full amount at once). On her
// no the buyer is told the payment has not been seen, the claim
// closes as rejected, and she may claim again. The agent never
// tells a buyer a payment is confirmed before the merchant's yes
// or Seev's webhook, and a screenshot alone never changes an
// order's status.

import (
	"regexp"
	"strings"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/insights"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/seevwebhook"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// directIntentRe reads the buyer choosing the merchant's number
// over the link: "I will pay to the number", "I'll send it
// direct", "MoMo to her number", "I prefer the number". It is only
// consulted while she has an unpaid order, where paying can only
// mean paying for that order.
var directIntentRe = regexp.MustCompile(`(pay (it )?(to|into|on) (the|her|your) number|send (it )?(direct|straight)|momo (to|into|on) (the|her|your) number|to (her|your) number\b|prefer (the|that|this) number|use (the|her|that) number|pay (direct|directly)|the number (is|will be) (fine|ok)|number (is )?fine)`)

// claimRe reads a buyer saying the money has already gone: "I
// have paid", "I just sent it", "payment done", "check your
// phone". A bare "sent" is read by isClaimText as well. Questions
// are never claims: "have you sent it?" asks, it does not tell.
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

// claimAmountOf reads the amount a claim names, when it names one:
// the first figure in her words. Nothing named means she is
// claiming the order total, which the caller fills in.
func claimAmountOf(text string) *float64 {
	nums := numbersIn(text)
	if len(nums) == 0 {
		return nil
	}
	return &nums[0]
}

// noOrderClaimText is the plain answer to a payment claim with no
// unpaid order behind it: there is nothing to confirm, and no
// merchant is troubled with it.
func noOrderClaimText(merchant *catalogue.Merchant) string {
	return "I have no unpaid order for you here just now, so there is nothing to pass on for confirmation. When you buy something here, pay and send the screenshot in this chat, and I will send it to " + merchant.Name + " to confirm."
}

// handleBuyerPayment is the buyer's side of the direct path,
// consulted once a shop is bound and before the ordinary stage
// handling: choosing the number, and claiming a payment, attach
// to her own newest unpaid order in this shop and to nothing
// else. It reports whether it took the message.
func handleBuyerPayment(from string, merchant *catalogue.Merchant, msg, lower string) bool {
	claim := isClaimText(lower)
	order := orders.Default.NewestUnpaidFor(merchant.ID, from)
	if order == nil {
		if claim {
			whatsapp.SendText(from, noOrderClaimText(merchant))
			return true
		}
		return false
	}
	if claim {
		handlePaidReport(from, merchant, order, claimAmountOf(msg), directWordsRe.MatchString(lower))
		return true
	}
	if directIntentRe.MatchString(lower) {
		payout := order.PayoutPhone
		if payout == "" {
			payout = merchant.PayoutPhone
		}
		if payout == "" {
			// She asked for a number the shop never put on
			// record: the link stays her only door, and no
			// number is invented for her.
			return false
		}
		orders.Default.Update(order.Reference, func(o *orders.Order) {
			o.PaymentPath = "direct"
		})
		sendPieces(from,
			"Send "+money.GHS(order.Charged())+" to "+payout+". The name on the number is "+merchant.Name+".",
			"When you have sent it, send the screenshot here.")
		return true
	}
	return false
}

// createClaim records a direct payment claim on the buyer's order
// and puts it in front of the merchant of that shop, and only that
// shop: the claim pieces, plainly labelled when the buyer sent no
// screenshot or named a different amount, and the screenshot
// itself as an image message when one came. A second claim while
// one is open is acknowledged, never duplicated to the merchant.
func createClaim(from string, merchant *catalogue.Merchant, order *orders.Order, named *float64, screenshotRef string, hasShot bool) {
	if order.ClaimStatus == "open" {
		whatsapp.SendText(from, "Already with "+merchant.Name+" to confirm.")
		return
	}
	total := order.Charged()
	amount := total
	if named != nil {
		amount = *named
	}
	mismatch := money.Round(amount*100) != money.Round(total*100)
	orders.Default.Update(order.Reference, func(o *orders.Order) {
		o.PaymentPath = "direct"
		o.ClaimStatus = "open"
		o.ClaimAmount = amount
		o.ClaimMismatch = mismatch
		o.ClaimedAt = time.Now().UTC().Format(time.RFC3339)
		o.ClaimShot = hasShot
		o.ScreenshotRef = screenshotRef
	})

	summary := "Payment claim: " + from + " says she has paid " + money.GHS(amount) + " for " + order.ProductName + " (order " + order.Reference + ") by MoMo to your number."
	pieces := []string{summary}
	if mismatch {
		pieces = append(pieces, "Careful: she names "+money.GHS(amount)+", but the order total is "+money.GHS(total)+".")
	}
	if !hasShot {
		pieces = append(pieces, "She sent no screenshot, only her word.")
	}
	pieces = append(pieces, "Reply yes if you have received it, or no if you have not.")
	if phone := merchantNotifyPhone(merchant); phone != "" {
		sendPieces(phone, pieces...)
		if hasShot && screenshotRef != "" {
			// The screenshot travels with its claim summary as
			// the caption, through the ordinary send path (see
			// whatsapp.SendImage for what live mode still needs).
			whatsapp.SendImage(phone, screenshotRef, summary)
		}
	}
	whatsapp.SendText(from, "I have sent it to "+merchant.Name+" to confirm. I will tell you the moment she does.")
}

// claimRefRe finds an order reference inside a merchant's answer:
// "yes EFUA-0003" names the claim outright instead of settling
// the oldest open one. A reference is a shop's code, a dash and
// its serial; the shape minted before shop codes (HACK-<stamp>)
// matches the same pattern.
var claimRefRe = regexp.MustCompile(`\b([a-z]{2,}-\d+)\b`)

// isClaimYes reads the merchant's confirming word. A question is
// never a confirmation, and "not received" is a no wearing yes's
// clothes, so the no is always read first by the caller.
func isClaimYes(lower string) bool {
	if strings.HasSuffix(strings.TrimSpace(lower), "?") {
		return false
	}
	switch controlWord(lower) {
	case "yes", "yeah", "yep", "ok", "okay", "confirm", "confirmed",
		"received", "seen it", "received it",
		"i have received it", "i received it", "i have seen it", "i saw it",
		"yes received", "yes i have received it":
		return true
	}
	w := controlWord(lower)
	return len(strings.Fields(w)) <= 6 && strings.Contains(w, "received") && !strings.Contains(w, "not")
}

// isClaimNo reads the merchant's refusing word.
func isClaimNo(lower string) bool {
	if strings.HasSuffix(strings.TrimSpace(lower), "?") {
		return false
	}
	switch controlWord(lower) {
	case "no", "nope", "not received", "not seen", "not yet",
		"haven't seen it", "have not seen it", "i have not seen it",
		"i haven't seen it", "i did not receive it", "did not receive it",
		"i have not received it":
		return true
	}
	w := controlWord(lower)
	return strings.Contains(w, "not received") || strings.Contains(w, "not seen") || strings.Contains(w, "haven't seen")
}

// handleMerchantClaim is the merchant's word on an open claim, the
// only thing that can settle a direct payment. It runs inside
// merchant mode, for her own shop only (resolved by the caller
// before anything else), after her in-progress flows, which own
// her words while they are open. A yes or no with no open claim
// in her shop is not consumed here at all: it falls through to
// her ordinary handling, and it can never reach another shop's
// claim, because the lookups below are keyed to her shop alone.
// claimLeadAnswer reads a referenced answer ("yes EFUA-0003",
// "no EFUA-0003") by its leading word. isClaimYes rightly demands
// the whole message, which a reference can never satisfy, so a
// named claim is answered by the word the merchant led with.
func claimLeadAnswer(lower string) (yes, no bool) {
	fields := strings.Fields(lower)
	if len(fields) == 0 || strings.HasSuffix(strings.TrimSpace(lower), "?") {
		return false, false
	}
	switch strings.Trim(fields[0], ".,!") {
	case "yes", "yeah", "yep", "received", "confirmed", "seen":
		return true, false
	case "no", "nope", "not":
		return false, true
	}
	return false, false
}

func handleMerchantClaim(from string, merchant *catalogue.Merchant, lower string) bool {
	if m := claimRefRe.FindStringSubmatch(lower); m != nil {
		yes, no := claimLeadAnswer(lower)
		if !yes && !no {
			return false
		}
		ref := strings.ToUpper(m[1])
		o := orders.Default.GetOrder(ref)
		if o == nil || o.MerchantID != merchant.ID || o.Status != "pending" || o.ClaimStatus != "open" {
			whatsapp.SendText(from, "That reference is not an open payment claim at "+merchant.Name+" just now.")
			return true
		}
		settleClaim(from, merchant, o, yes)
		return true
	}

	no := isClaimNo(lower)
	yes := !no && isClaimYes(lower)
	if !yes && !no {
		return false
	}
	o := orders.Default.OldestOpenClaim(merchant.ID)
	if o == nil {
		return false
	}
	settleClaim(from, merchant, o, yes)
	return true
}

// settleClaim applies the merchant's word to one open claim. Yes:
// the order is paid, by the direct path, settled by her word, and
// both sides hear it, the merchant with her direct figures (the
// full amount is hers; there is no Seev fee on money that never
// went near Seev). No: the claim closes as rejected, the order
// stays unpaid, and the buyer is told where to check and that she
// may claim again when the money lands.
func settleClaim(merchantPhone string, merchant *catalogue.Merchant, o *orders.Order, confirmed bool) {
	total := o.Charged()
	if confirmed {
		orders.Default.Update(o.Reference, func(x *orders.Order) {
			x.Status = "paid"
			x.PaymentPath = "direct"
			x.SettledVia = "merchant-confirmed"
			x.ClaimStatus = "confirmed"
		})
		whatsapp.SendText(o.From, merchant.Name+" has confirmed your payment of "+money.GHS(total)+". Thank you.")
		day := daybook.Default.GetDayState(merchant.ID, daybook.Options{Allowance: merchant.DailyGiveawayAllowance})
		whatsapp.SendText(merchantPhone, "Confirmed. "+o.ProductName+" paid in full by MoMo to your number: "+money.GHS(total)+" is yours, no Seev fee on this one. Day book: "+insights.DayReportText(day))
		return
	}
	orders.Default.Update(o.Reference, func(x *orders.Order) {
		x.ClaimStatus = "rejected"
	})
	payout := o.PayoutPhone
	if payout == "" {
		payout = merchant.PayoutPhone
	}
	whatsapp.SendText(o.From, merchant.Name+" says she has not seen the payment yet. Check the number you sent to: "+payout+". When it lands, send the screenshot again.")
	whatsapp.SendText(merchantPhone, "Told "+o.From+" you have not seen it yet, and to check the number she sent to. The order stays unpaid; she can claim again when it lands.")
}

// directWordsRe reads a buyer saying the money went to the
// shop's own number rather than through the link: she names
// the number, the sending to it, or the direct route itself.
// It decides one thing only: who can verify her report. A
// payment to the merchant's number is verified by the merchant,
// who can see her own MoMo; a payment through the Seev link
// is verified by the agent with Seev, because the merchant has
// no window into Seev at all and must never be asked to
// confirm a payment she cannot see.
var directWordsRe = regexp.MustCompile(`(?i)(momo\s+number|your\s+number|her\s+number|the\s+shop'?s\s+number|sent\s+(it|the\s+money)\s+to|paid\s+(it\s+)?(to|into)|directly|to\s+the\s+number)`)

// handlePaidReport answers a buyer who says she has paid, and
// routes the report to whoever can actually verify it. When
// her order carries a live Seev session and she does not say
// the money went to the number, the agent checks with Seev
// itself and confirms or corrects her from what Seev says.
// Only a payment she says went to the merchant's own number,
// or an order with no Seev session at all, becomes a claim for
// the merchant to settle with her own word.
func handlePaidReport(from string, merchant *catalogue.Merchant, order *orders.Order, named *float64, direct bool) {
	if fresh := orders.Default.GetOrder(order.Reference); fresh != nil && fresh.Status == "paid" {
		whatsapp.SendText(from, "That one is already confirmed paid. Thank you.")
		return
	}
	if !direct && order.ProviderRef != nil && *order.ProviderRef != "" {
		verifyLinkPayment(from, merchant, order)
		return
	}
	createClaim(from, merchant, order, named, "", false)
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
		whatsapp.SendText(from, "I could not reach the payment service to check just now. The moment it confirms your payment I will tell you, and "+merchant.Name+" will hear it from me at the same time. If you sent the money to the shop's MoMo number instead, say so and I will pass it to the shop to confirm.")
	case checkout.IsPaidStatus(check.Status) && check.AmountMajor != nil && money.Round(*check.AmountMajor*100) != money.Round(charged*100):
		whatsapp.SendText(from, "I have checked with the payment service. A payment came through, but the amount does not match your order, so I have told "+merchant.Name+" and we are looking at it together. Your order is not settled yet.")
		if phone := merchantNotifyPhone(merchant); phone != "" {
			whatsapp.SendText(phone, "Payment amount mismatch on order "+order.Reference+": the order is "+money.GHS(charged)+" but Seev reports "+money.GHS(*check.AmountMajor)+". The buyer reported she had paid; I have not marked it paid. Please check the Seev record with me.")
		}
	case checkout.IsPaidStatus(check.Status):
		seevwebhook.SettleVerified(order, "I have checked with the payment service, and you are right: your payment for the "+order.ProductName+" has come through, confirmed at "+money.GHS(charged)+". "+merchant.Name+" has your order and will take it from here.")
	case checkout.IsFailedStatus(check.Status):
		sendPieces(from,
			"I have checked with the payment service, and that payment did not go through, so nothing was charged.",
			"Your order "+order.Reference+" is still open. You can try again with the same link: "+orderPaymentURL(order),
		)
	default:
		whatsapp.SendText(from, "I have checked with the payment service, and your payment has not come through yet. Your order "+order.Reference+" for the "+order.ProductName+" is still open. If the money has only just left your phone, give it a small moment and tell me again, and I will check again. If you sent it to the shop's MoMo number instead, say so and I will pass it to the shop to confirm.")
	}
}
