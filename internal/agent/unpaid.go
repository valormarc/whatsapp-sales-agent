package agent

// The unpaid watch. A buyer who takes a payment link and never
// pays leaves no trace anywhere: Seev reports the attempts it
// sees, not an abandoned page, so without a watch the order sits
// pending in silence and neither side ever hears of it. The
// watch walks the order book on a slow tick. Before it surfaces
// an order it checks the payment itself: a payment that landed
// without its news reaching the agent (a webhook that never
// arrived) is settled on the spot, the way any verified payment
// settles, and no reminder is sent for money already received.
// A payment the service says failed closes the order with both
// sides told. Only a payment that truly never came through is
// surfaced, once, to the buyer as a reminder and to the
// merchant as an alert, and marked so it never nags twice.

import (
	"sync"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/seevwebhook"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

const (
	// unpaidAfter is the grace a buyer gets to complete a
	// payment before either side is told she has not.
	unpaidAfter = 30 * time.Minute
	unpaidTick  = 5 * time.Minute
)

var unpaidWatchOnce sync.Once

// StartUnpaidWatch starts the watch's slow tick. It is called
// once by the server as it starts; tests drive the pass
// directly and never start the tick.
func StartUnpaidWatch() {
	unpaidWatchOnce.Do(func() {
		go func() {
			for {
				CheckUnpaidOrders(time.Now().UTC())
				time.Sleep(unpaidTick)
			}
		}()
	})
}

// CheckUnpaidOrders runs one pass of the watch over the whole
// order book at the given moment.
func CheckUnpaidOrders(now time.Time) {
	// The watch is not a conversation, but a settlement it
	// triggers may draft; each sweep opens a fresh budget.
	model.BeginTurn()
	for _, o := range orders.Default.All() {
		if o.Status != "pending" || o.ProviderRef == nil || *o.ProviderRef == "" || o.UnpaidAlerted {
			continue
		}
		created, err := time.Parse(time.RFC3339, o.CreatedAt)
		if err != nil {
			// An order placed before orders carried a time:
			// stamp it now and let the grace run from here, so
			// a legacy order is never alerted on first sight.
			ref := o.Reference
			orders.Default.Update(ref, func(x *orders.Order) {
				if x.CreatedAt == "" {
					x.CreatedAt = now.Format(time.RFC3339)
				}
			})
			continue
		}
		if now.Sub(created) < unpaidAfter {
			continue
		}
		// Before anyone is told the money never came, the
		// payment service is asked: its own record settles the
		// question, and a landed payment whose webhook never
		// arrived is healed here instead of reminded about.
		if checkout.Enabled() {
			if check := checkout.VerifyPayment(*o.ProviderRef); check != nil {
				switch {
				case checkout.IsPaidStatus(check.Status) && (check.AmountMajor == nil || money.Round(*check.AmountMajor*100) == money.Round(o.Charged()*100)):
					oo := o
					seevwebhook.SettleVerified(&oo, "Payment received, thank you. Your "+o.ProductName+" is confirmed at "+money.GHS(o.Charged())+". The shop has your order and will take it from here.")
					continue
				case checkout.IsPaidStatus(check.Status):
					if phone := merchantNotifyPhone(catalogue.GetMerchant(o.MerchantID)); phone != "" {
						whatsapp.SendText(phone, "Payment amount mismatch on order "+o.Reference+": the order is "+money.GHS(o.Charged())+" but Seev reports "+money.GHS(*check.AmountMajor)+". I have not marked it paid. Please check the Seev record with me.")
					}
					ref := o.Reference
					orders.Default.Update(ref, func(x *orders.Order) { x.UnpaidAlerted = true })
					continue
				case checkout.IsFailedStatus(check.Status):
					orders.Default.SetStatus(o.Reference, "failed")
					whatsapp.SendText(o.From, "Your payment for the "+o.ProductName+" did not go through, so nothing has been charged. The shop has been told, and you can try the link again or talk to the shop here.")
					if phone := merchantNotifyPhone(catalogue.GetMerchant(o.MerchantID)); phone != "" {
						whatsapp.SendText(phone, "Payment failed\n\n*"+o.ProductName+"*\nThe buyer's payment of "+money.GHS(o.Charged())+" did not go through. Nothing was charged.\n\nRef: "+o.Reference+"\nBuyer: "+o.From)
					}
					continue
				}
			}
		}
		whatsapp.SendText(o.From, "Your payment for the "+o.ProductName+" has not come through yet. Your order "+o.Reference+" for "+money.GHS(o.Charged())+" is still open. You can pay with the link I sent you earlier, or talk to the shop here if something went wrong.")
		if phone := merchantNotifyPhone(catalogue.GetMerchant(o.MerchantID)); phone != "" {
			whatsapp.SendText(phone, "Payment not received yet\n\n*"+o.ProductName+"*\nOrder "+o.Reference+" for "+money.GHS(o.Charged())+" has been open for over 30 minutes without payment.\n\nBuyer: "+o.From+"\nThe buyer has been reminded. The order is still open.")
		}
		ref := o.Reference
		orders.Default.Update(ref, func(x *orders.Order) { x.UnpaidAlerted = true })
	}
}
