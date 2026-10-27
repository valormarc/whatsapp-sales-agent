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
			"Payment failed: "+order.ProductName+" at "+money.GHS(order.Price)+" for buyer "+order.From+". Ref: "+order.Reference+". Nothing was charged.",
		)
		return 200
	}

	if !checkout.IsPaidStatus(status) {
		return 200
	}

	// Amount must match what the buyer was charged exactly: the
	// item price plus the shop's delivery fee when one was charged.
	// Seev reports major units on webhooks and read-backs; the order
	// stores cedis.
	charged := order.Charged()
	amountMismatch := func(major *float64) bool {
		return major != nil && money.Round(*major*100) != money.Round(charged*100)
	}
	if amountMismatch(event.AmountMajor) {
		notifyMerchant(order,
			"Payment amount mismatch on ref "+order.Reference+": the order is "+money.GHS(charged)+" but Seev reports "+money.GHS(*event.AmountMajor)+". I have not marked it paid. Please check the Seev dashboard.",
		)
		return 200
	}

	// Belt and braces: read the session back when we can. If Seev
	// answers and disagrees with paid, Seev's read-back wins. If Seev
	// cannot be reached, the signature-verified event stands.
	if checkout.Enabled() && order.ProviderRef != nil && *order.ProviderRef != "" {
		check := checkout.VerifyPayment(*order.ProviderRef)
		if check != nil && !checkout.IsPaidStatus(check.Status) {
			return 200
		}
		if check != nil && amountMismatch(check.AmountMajor) {
			notifyMerchant(order,
				"Payment amount mismatch on ref "+order.Reference+": the order is "+money.GHS(charged)+" but Seev reports "+money.GHS(*check.AmountMajor)+". I have not marked it paid. Please check the Seev dashboard.",
			)
			return 200
		}
	}

	orders.Default.Update(order.Reference, func(o *orders.Order) {
		o.Status = "paid"
		o.PaymentPath = "seev"
	})
	s := checkout.SettlementFor(charged)
	whatsapp.SendText(
		order.From,
		"Payment received, thank you. Your "+order.ProductName+" is confirmed at "+money.GHS(charged)+". The shop has your order and will take it from here.",
	)
	notifyMerchant(order,
		"Payment received: "+order.ProductName+" at "+money.GHS(charged)+". Seev fee "+money.GHS(s.Fee)+", so "+money.GHS(s.Net)+" settles to your MoMo tomorrow. Buyer: "+order.From+". Ref: "+order.Reference+".",
	)
	return 200
}
