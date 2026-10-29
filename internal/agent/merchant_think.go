package agent

// The merchant's thinking acts: the work her questions
// actually ask for, done from the records instead of bounced
// back at her. When she asks whether a payment came through
// the link or to her number, the answer is in her book and in
// a live check of the link, not in another question. When she
// says "ask the buyer", the buyer is asked, by the agent, and
// her answer is carried back. The merchant is the owner; the
// agent works for her, and work means answers and errands,
// not clarifying questions about what she plainly meant.

import (
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/seevwebhook"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// merchantOrderPayment answers the owner's question about the
// payment on the order in focus: what the order is, whether
// it is paid, what the buyer has claimed and what that claim
// says about the route, and what the payment link itself says
// when it is checked live. A link payment the check finds has
// landed settles the order on the spot, the same settlement
// the webhook performs, because finding it is the agent's job
// however it comes to look.
func merchantOrderPayment(from string, merchant *catalogue.Merchant, shopID, msg string) bool {
	order := orderFromMessage(shopID, msg)
	if order == nil {
		order = focusOrder(shopID)
	}
	if order == nil {
		return false
	}
	pieces := []string{"Order " + order.Reference + ": the " + order.ProductName + ", " + money.GHS(order.Charged()) + ", buyer " + order.From + "."}
	switch {
	case order.Status == "paid" && order.PaymentPath == "seev":
		pieces = append(pieces, "It is paid. The money came through the payment link, and I confirmed it to the buyer.")
	case order.Status == "paid":
		pieces = append(pieces, "It is paid. The money came to your MoMo number directly, and you confirmed it yourself.")
	case order.ClaimStatus == "open":
		amt := order.Charged()
		if order.ClaimAmount > 0 {
			amt = order.ClaimAmount
		}
		claimLine := "The buyer says she paid " + money.GHS(amt) + " straight to your MoMo number."
		if order.ScreenshotRef == "" {
			claimLine += " She sent no screenshot, only her word."
		} else {
			claimLine += " She sent a screenshot with it."
		}
		pieces = append(pieces, claimLine+" It is waiting on your word: reply yes if you have received it, or no if you have not.")
	default:
		pieces = append(pieces, "It is not paid yet, and the buyer has not claimed a payment to your number.")
	}
	if order.Status != "paid" && order.ProviderRef != nil && *order.ProviderRef != "" {
		check := checkout.VerifyPayment(*order.ProviderRef)
		switch {
		case check == nil:
			pieces = append(pieces, "I tried to check the payment link just now, but the payment service did not answer.")
		case checkout.IsPaidStatus(check.Status) && (check.AmountMajor == nil || money.Round(*check.AmountMajor*100) == money.Round(order.Charged()*100)):
			seevwebhook.SettleVerified(order, "Payment received, thank you. Your "+order.ProductName+" is confirmed at "+money.GHS(order.Charged())+". The shop has your order and will take it from here.")
			pieces = append(pieces, "I have just checked the payment link: the payment has come through there. I have settled the order and told the buyer.")
		case checkout.IsPaidStatus(check.Status):
			pieces = append(pieces, "I have just checked the payment link: a payment came through, but its amount does not match this order, so I have not settled it.")
		case checkout.IsFailedStatus(check.Status):
			pieces = append(pieces, "I have just checked the payment link: that payment failed, so nothing was charged there.")
		default:
			pieces = append(pieces, "I have just checked the payment link: nothing has come through there.")
		}
	}
	sendPieces(from, pieces...)
	return true
}

// orderFromMessage resolves an order of this shop named by
// reference in the message, or nil.
func orderFromMessage(shopID, msg string) *orders.Order {
	ref := orderRefRe.FindString(strings.ToUpper(msg))
	if ref == "" {
		return nil
	}
	o := orders.Default.GetOrder(ref)
	if o == nil || o.MerchantID != shopID {
		return nil
	}
	return o
}

// merchantAskBuyer carries the owner's question to her buyer.
// The question is composed from the open matter: on a payment
// claim or an open order it is the route question, the one
// thing only the buyer can settle; on anything else it is the
// owner's drafted words, guarded like every draft. The buyer
// hears it in her own chat, her session remembers that her
// answer is owed to the merchant, and the merchant is told
// the errand is done, never asked what the errand should be.
func merchantAskBuyer(from string, merchant *catalogue.Merchant, shopID string, u *understood) bool {
	order := focusOrder(shopID)
	if order == nil {
		return false
	}
	buyer := order.From
	question := ""
	if order.ClaimStatus == "open" || order.Unpaid() {
		question = "Hello, I am helping " + merchant.Name + " with this chat. The shop is checking on your payment for the " + order.ProductName + " (order " + order.Reference + "): did you pay through the payment link that was sent to you, or straight to the shop's MoMo number?"
	} else if strings.TrimSpace(u.Reply) != "" && voiceClean(u.Reply) && figuresClean(u.Reply, order.Charged(), order.Price) {
		question = u.Reply
	}
	if question == "" {
		return false
	}
	key := sessionKey(shopID, buyer)
	bs, ok := sessions[key]
	if !ok {
		bs = &Session{Stage: "browsing", MerchantID: shopID}
		sessions[key] = bs
	}
	bs.RelayOrderRef = order.Reference
	bs.RelayShopID = shopID
	whatsapp.SendText(buyer, question)
	whatsapp.SendText(from, "I have asked your buyer about order "+order.Reference+". I will bring you her answer the moment she replies.")
	return true
}

// relayBuyerAnswer runs on a buyer turn while her merchant's
// question is open. It returns true when the turn was the
// answer's own business (a route answer, settled by checks
// and claims); for any other answer it relays her words to
// the merchant and lets her turn continue as normal.
func relayBuyerAnswer(from string, s *Session, merchant *catalogue.Merchant, msg, lower string) bool {
	if s.RelayOrderRef == "" || merchant == nil || s.RelayShopID != merchant.ID {
		return false
	}
	order := orders.Default.GetOrder(s.RelayOrderRef)
	if order == nil || order.From != from {
		s.RelayOrderRef, s.RelayShopID = "", ""
		return false
	}
	phone := merchantNotifyPhone(merchant)
	s.RelayOrderRef, s.RelayShopID = "", ""
	saysNumber := directWordsRe.MatchString(lower)
	saysLink := !saysNumber && (strings.Contains(lower, "link") || strings.Contains(lower, "seev"))
	switch {
	case saysNumber:
		if phone != "" {
			whatsapp.SendText(phone, "Your buyer answered about order "+order.Reference+": she paid straight to your MoMo number.")
		}
		handlePaidReport(from, merchant, order, claimAmountOf(msg), true)
		return true
	case saysLink:
		if order.ProviderRef == nil || *order.ProviderRef == "" {
			if phone != "" {
				whatsapp.SendText(phone, "Your buyer answered about order "+order.Reference+": she says she paid through the payment link, but that order has no live link session, so there is nothing there for me to check.")
			}
			whatsapp.SendText(from, "Thank you. I have told "+merchant.Name+" what you said.")
			return true
		}
		if phone != "" {
			whatsapp.SendText(phone, "Your buyer answered about order "+order.Reference+": she paid through the payment link. I have checked it.")
		}
		verifyLinkPaymentForRelay(from, merchant, order, phone)
		return true
	default:
		if phone != "" {
			whatsapp.SendText(phone, "Your buyer answered about order "+order.Reference+": \""+msg+"\"")
		}
		return false
	}
}

// verifyLinkPaymentForRelay checks a relayed "I paid through
// the link" against Seev and tells both sides the result:
// the buyer her confirmation or correction, the merchant the
// outcome of the check she asked for.
func verifyLinkPaymentForRelay(from string, merchant *catalogue.Merchant, order *orders.Order, merchantPhone string) {
	charged := order.Charged()
	check := checkout.VerifyPayment(*order.ProviderRef)
	tell := func(line string) {
		if merchantPhone != "" {
			whatsapp.SendText(merchantPhone, line)
		}
	}
	switch {
	case check == nil:
		whatsapp.SendText(from, "I could not reach the payment service to check just now. The moment it confirms your payment I will tell you, and "+merchant.Name+" will hear it from me at the same time.")
		tell("I could not reach the payment service to check her link payment just now. I will confirm it the moment it answers.")
	case checkout.IsPaidStatus(check.Status) && (check.AmountMajor == nil || money.Round(*check.AmountMajor*100) == money.Round(charged*100)):
		seevwebhook.SettleVerified(order, "I have checked with the payment service, and you are right: your payment for the "+order.ProductName+" has come through, confirmed at "+money.GHS(charged)+". "+merchant.Name+" has your order and will take it from here.")
	case checkout.IsPaidStatus(check.Status):
		whatsapp.SendText(from, "I have checked with the payment service. A payment came through, but the amount does not match your order, so I have told "+merchant.Name+" and we are looking at it together.")
		tell("Her link payment came through, but the amount does not match order " + order.Reference + ", so I have not settled it.")
	case checkout.IsFailedStatus(check.Status):
		sendPieces(from,
			"I have checked with the payment service, and that payment did not go through, so nothing was charged.",
			"Your order "+order.Reference+" is still open. You can try again:",
		)
		sendPayButton(from, order.Charged(), orderPaymentURL(order))
		tell("I checked: her link payment for order " + order.Reference + " failed, so nothing was charged.")
	default:
		whatsapp.SendText(from, "I have checked with the payment service, and your payment has not come through yet. Your order "+order.Reference+" for the "+order.ProductName+" is still open. If the money has only just left your phone, give it a small moment and tell me again, and I will check again.")
		tell("I checked: her payment for order " + order.Reference + " has not come through on the link yet.")
	}
}
