// Package seevwebhook handles the Seev payment webhook: the moment
// money becomes real in the flow.
//
// Discipline: a delivery is believed only after its signature checks
// out (when a webhook secret is configured, it always should be). The
// order is settled only when the event says paid, the amount matches
// the agreed price to the pesewa, and, when Seev is reachable, the
// session read-back agrees. Anything else is acknowledged so Seev
// stops retrying, and the merchant hears about mismatches from us
// rather than discovering them at settlement.
package seevwebhook

import (
	"encoding/json"
	"os"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// notifyMerchant tells the owner of the shop an order belongs to.
// The shop is resolved from the order itself, so a settlement notice
// for one shop can never land on another shop's owner. The env
// number is only the fallback for a shop that records no owner.
func notifyMerchant(order *orders.Order, text string) {
	phone := ""
	if order != nil && order.MerchantID != "" {
		if shop := catalogue.GetMerchant(order.MerchantID); shop != nil && shop.OwnerWhatsApp != "" {
			phone = shop.OwnerWhatsApp
		}
	}
	if phone == "" {
		phone = os.Getenv("MERCHANT_WHATSAPP")
	}
	if phone != "" {
		whatsapp.SendText(phone, text)
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Handle handles one delivery. It returns the HTTP status to answer
// with: 401 for a bad signature, 400 for an unparseable body, 200 for
// everything else, including events we deliberately do not act on.
func Handle(rawBody, timestamp, signature string) int {
	if secret := os.Getenv("SEEV_WEBHOOK_SECRET"); secret != "" {
		if !checkout.VerifyWebhookSignatureNow(rawBody, timestamp, signature, secret) {
			return 401
		}
	}

	var body json.RawMessage
	if err := json.Unmarshal([]byte(rawBody), &body); err != nil {
		return 400
	}
	// The reference implementation parses the body as JSON and reads
	// fields from the resulting value; a body that is valid JSON but
	// not an object simply yields no fields.
	event := checkout.ParseWebhookEvent([]byte(rawBody))

	var order *orders.Order
	if event.OrderReference != nil {
		order = orders.Default.GetOrder(*event.OrderReference)
	}
	if order == nil && event.Reference != nil {
		order = orders.Default.FindByProviderRef(*event.Reference)
	}
	if order == nil || order.Status == "paid" {
		return 200
	}

	status := deref(event.Status)

	if checkout.IsFailedStatus(status) {
		orders.Default.SetStatus(order.Reference, "failed")
		whatsapp.SendText(
			order.From,
			"Your payment for the "+order.ProductName+" did not go through, so nothing has been charged. The shop has been told, and you can try the link again or talk to the shop here.",
		)
		notifyMerchant(order,
			"Payment failed\n\n*"+order.ProductName+"*\nThe buyer's payment of "+money.GHS(order.Price)+" did not go through. Nothing was charged.\n\nRef: "+order.Reference+"\nBuyer: "+order.From,
		)
		return 200
	}

	if !checkout.IsPaidStatus(status) {
		return 200
	}

	// Amount must match what the buyer was charged exactly: the
	// item price plus the shop's delivery fee when one was charged.
	// The order stores cedis. The webhook event reports its amount
	// in major units; the session read-back reports pesewas, which
	// VerifyPayment converts, so both comparisons below are in
	// cedis against the charged figure.
	charged := order.Charged()
	amountMismatch := func(major *float64) bool {
		return major != nil && money.Round(*major*100) != money.Round(charged*100)
	}

	// The session read-back is the authority when Seev answers:
	// it is Seev's own record of this exact session. If it says
	// not paid, it wins over the event. If it says paid for the
	// charged amount, the order settles on its word even if the
	// event's own amount reading is ever in a different unit.
	// Only when Seev cannot be reached does the signature-
	// verified event stand alone, with its amount checked.
	readbackAgrees := false
	if checkout.Enabled() && order.ProviderRef != nil && *order.ProviderRef != "" {
		check := checkout.VerifyPayment(*order.ProviderRef)
		if check != nil {
			if !checkout.IsPaidStatus(check.Status) {
				return 200
			}
			if amountMismatch(check.AmountMajor) {
				notifyMerchant(order,
					"Payment amount mismatch on ref "+order.Reference+": the order is "+money.GHS(charged)+" but Seev reports "+money.GHS(*check.AmountMajor)+". I have not marked it paid. Please check the Seev dashboard.",
				)
				return 200
			}
			readbackAgrees = true
		}
	}
	if amountMismatch(event.AmountMajor) && !readbackAgrees {
		notifyMerchant(order,
			"Payment amount mismatch on ref "+order.Reference+": the order is "+money.GHS(charged)+" but Seev reports "+money.GHS(*event.AmountMajor)+". I have not marked it paid. Please check the Seev dashboard.",
		)
		return 200
	}

	SettleVerified(order,
		"Payment received, thank you. Your "+order.ProductName+" is confirmed at "+money.GHS(charged)+". The shop has your order and will take it from here.",
	)
	return 200
}

// SettleVerified settles an order whose payment Seev itself
// has verified, by webhook or by the agent's own read-back
// when a buyer reports she has paid, and tells both sides:
// the buyer her confirmation, the merchant the payment
// received notice with the fee and what settles to her MoMo.
// The merchant is never asked to verify a Seev payment: she
// has no window into Seev, so verification is the agent's
// work, and hers is only the alert that it is done.
func SettleVerified(order *orders.Order, buyerText string) {
	charged := order.Charged()
	orders.Default.Update(order.Reference, func(o *orders.Order) {
		o.Status = "paid"
		o.PaymentPath = "seev"
		// A claim the buyer also filed (she said she had paid,
		// and she had, through the link) is answered by this
		// settlement: it closes as confirmed, so it never
		// lingers as work waiting on the merchant's word.
		if o.ClaimStatus == "open" {
			o.ClaimStatus = "confirmed"
		}
	})
	s := checkout.SettlementFor(charged)
	whatsapp.SendText(order.From, buyerText)
	notifyMerchant(order,
		"Payment received\n\n*"+order.ProductName+"*\n"+money.GHS(charged)+" paid in full by the buyer.\nSeev fee "+money.GHS(s.Fee)+", so "+money.GHS(s.Net)+" settles to your MoMo tomorrow.\n\nRef: "+order.Reference+"\nBuyer: "+order.From,
	)
}
