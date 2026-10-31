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
// verifyPaymentFn is the session read-back, seamed so tests
// can stand Seev in without a network.
var verifyPaymentFn = checkout.VerifyPayment

func notifyMerchant(order *orders.Order, text string) {
	phone := ""
	if order != nil && order.MerchantID != "" {
		if shop := catalogue.GetMerchant(order.MerchantID); shop != nil && shop.OwnerWhatsApp != "" {
			phone = shop.OwnerWhatsApp
		}
	}
	if phone == "" && (order == nil || order.MerchantID == "" || order.MerchantID == "demo") {
		// The env number is the demo shop's owner. The
		// second audit found it standing in for ANY
		// ownerless shop, so one shop's settlement
		// notices — buyer numbers included — landed on
		// the demo owner while the true owner heard
		// nothing. The fallback serves the shop it
		// belongs to, and no other.
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
	secret := os.Getenv("SEEV_WEBHOOK_SECRET")
	if secret == "" {
		// Fail closed, exactly as the WhatsApp webhook does:
		// an unset secret never means "no signature needed"
		// on the endpoint that marks orders paid.
		return 401
	}
	if !checkout.VerifyWebhookSignatureNow(rawBody, timestamp, signature, secret) {
		return 401
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
		// A failed event acts only on the order whose
		// session it names. An order can hold a superseded
		// session — a first link mint retried under a fresh
		// key — and the old session's later failed or
		// expired event is a normal Seev delivery that must
		// not flip an order the buyer may already have paid
		// through its live session. And an order holding
		// NO session can be bound to no event at all: the
		// third audit found the conditional form of this
		// guard passing vacuously for sessionless orders,
		// so a signed failed event flipped open orders
		// whose session had been struck (the delete-my-data
		// lane) and restocked them. Mirror the paid road:
		// no recorded session, no action.
		if order.ProviderRef == nil || *order.ProviderRef == "" {
			return 200
		}
		if event.Reference == nil || *event.Reference != *order.ProviderRef {
			return 200
		}
		// A failed event closes only an order that is
		// still open. The second audit found this branch
		// running with no status precondition at all:
		// for a sessionless order (no binding to check)
		// it flipped even a cancelled order to failed —
		// terminal for that order's payment — and tried
		// to send the failure notice to the literal
		// address "deleted" for an anonymised buyer.
		if order.Status != "pending" && order.Status != "link_failed" {
			return 200
		}
		// The flip itself is decided inside the book's
		// lock, from the stored record — every binding
		// above was read from this caller's pass-start
		// snapshot, and the fourth audit found the
		// snapshot deciding while the stored record
		// ratified: between the two, a re-mint can
		// rebind the session or a cancellation can
		// land, and the unconditional flip then closed
		// an order the event was never bound to. The
		// sweep's failed branch already works this
		// way; the webhook now matches it, and the
		// restock and notices speak from the stored
		// record the flip actually closed.
		flipped := false
		orders.Default.Update(order.Reference, func(o *orders.Order) {
			if o.ProviderRef == nil || *o.ProviderRef == "" {
				return
			}
			if event.Reference == nil || *event.Reference != *o.ProviderRef {
				return
			}
			if o.Status != "pending" && o.Status != "link_failed" {
				return
			}
			o.Status = "failed"
			flipped = true
		})
		if !flipped {
			return 200
		}
		stored := orders.Default.GetOrder(order.Reference)
		if stored == nil {
			return 200
		}
		// An order that dies unpaid gives its shelf units
		// back, once only, claimed inside the order
		// book's lock: another road ending this order at
		// the same moment claims first or not at all.
		if lines, ok := orders.Default.ClaimRestock(stored.Reference); ok {
			for _, ln := range lines {
				catalogue.GiveStockMeasured(stored.MerchantID, ln.Name, ln.Units, ln.Unit, ln.BaseQty)
			}
		}
		if stored.From != "deleted" {
			whatsapp.SendText(
				stored.From,
				"Your payment for the "+stored.ProductName+" did not go through, so nothing has been charged. The shop has been told, and you can try the link again or talk to the shop here.",
			)
		}
		notifyMerchant(stored,
			"Payment failed\n\n*"+stored.ProductName+"*\nThe buyer's payment of "+money.GHS(stored.Price)+" did not go through. Nothing was charged.\n\nRef: "+stored.Reference+"\nBuyer: "+stored.From,
		)
		return 200
	}

	if !checkout.IsPaidStatus(status) {
		return 200
	}

	if !checkout.Enabled() {
		// This deployment mints no payment links, so a paid
		// event has nothing legitimate to settle here. Hold
		// the order and tell the merchant, rather than
		// marking goods paid on an event alone.
		notifyMerchant(order,
			"A payment event arrived for ref "+order.Reference+", but checkout is not set up on this deployment, so I have not marked the order paid. Please check the Seev dashboard.",
		)
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

	// The session read-back is the authority: it is Seev's
	// own record of this exact session. If it says paid for
	// the charged amount, the order settles on its word
	// even if the event's own amount reading is ever in a
	// different unit. If Seev cannot be reached to confirm
	// its own event, the order is HELD and the merchant
	// told: an outage must never be what marks goods paid.
	//
	// One more session can carry the money: the event may
	// name a SUPERSEDED session — the old link stays
	// payable at Seev after a re-issue (nothing revokes
	// it), so a buyer who had already started paying it
	// completes there, and the recorded session's
	// read-back says "not paid" about a payment that did
	// land. The third audit found those events silently
	// dropped, the money settled to the shop with the
	// order standing unpaid and neither side told. So when
	// the recorded read-back does not agree, the session
	// the event names is read back on the same terms.
	// Dry-run orders are exempt from that second look:
	// their sessions exist by design not at all, and the
	// mint roads rebind DryRun with the reference at every
	// re-mint, so a real order never carries the flag.
	readbackAgrees := false
	recordedClosed := false // the recorded read-back already decided: held with a note, or not paid
	if order.ProviderRef != nil && *order.ProviderRef != "" {
		check := verifyPaymentFn(*order.ProviderRef)
		switch {
		case check == nil:
			notifyMerchant(order,
				"A payment event arrived for ref "+order.Reference+", but I could not confirm it with Seev just now, so I have not marked the order paid. Please check the Seev dashboard.",
			)
			recordedClosed = true
		case !checkout.IsPaidStatus(check.Status):
			recordedClosed = true
		case check.AmountMajor == nil:
			// Paid, but the read-back states no amount:
			// status alone settles nothing (a reused
			// reference can point at a former era's
			// session at a different figure). Hold and
			// tell the merchant, as with a mismatch.
			notifyMerchant(order,
				"Payment read-back without an amount on ref "+order.Reference+": Seev reports the session paid but states no amount, so I have not marked the order of "+money.GHS(charged)+" paid. Please check the Seev dashboard.",
			)
			recordedClosed = true
		case amountMismatch(check.AmountMajor):
			notifyMerchant(order,
				"Payment amount mismatch on ref "+order.Reference+": the order is "+money.GHS(charged)+" but Seev reports "+money.GHS(*check.AmountMajor)+". I have not marked it paid. Please check the Seev dashboard.",
			)
			recordedClosed = true
		default:
			readbackAgrees = true
		}
	}
	if !readbackAgrees && !order.DryRun &&
		event.Reference != nil && *event.Reference != "" &&
		(order.ProviderRef == nil || *event.Reference != *order.ProviderRef) {
		if checkout.PaidAmountMatches(verifyPaymentFn(*event.Reference), charged) {
			SettleVerified(order,
				"Payment received, thank you. Your "+order.ProductName+" is confirmed at "+money.GHS(charged)+". The shop has your order and will take it from here.",
			)
			return 200
		}
	}
	if recordedClosed {
		return 200
	}
	if amountMismatch(event.AmountMajor) && !readbackAgrees {
		notifyMerchant(order,
			"Payment amount mismatch on ref "+order.Reference+": the order is "+money.GHS(charged)+" but Seev reports "+money.GHS(*event.AmountMajor)+". I have not marked it paid. Please check the Seev dashboard.",
		)
		return 200
	}

	if !readbackAgrees && !order.DryRun {
		// No session read-back stands behind this event:
		// the order carries no provider session at all.
		// That population is real — a link that failed to
		// mint records no session, and a buyer who deletes
		// her data has the session struck from her orders
		// with their status untouched — and the second
		// audit found such orders settling on the signed
		// event alone, with no session verification and,
		// when the event omitted it, no amount check
		// either. A signed event proves an event arrived;
		// it does not prove the money belongs to this
		// order. Hold it, tell the merchant once, and let
		// the dashboard decide.
		//
		// The one road exempt from the hold is an order
		// the order book itself marks dry-run: its link
		// was minted by the disabled-checkout factory,
		// so no session exists by design and no real
		// money moves. The exemption is a property of
		// the order, recorded at its mint — an order
		// minted against a configured provider always
		// carries its session, and can never borrow the
		// dry-run road after the fact.
		// The once-only claim is decided inside the
		// book's lock, from the stored record: the
		// fourth audit found the decision read from
		// this caller's snapshot outside the lock, so
		// two concurrent deliveries of the same event
		// both saw the alert unsent and both sent it.
		alert := false
		orders.Default.Update(order.Reference, func(o *orders.Order) {
			if !o.HeldAlerted {
				o.HeldAlerted = true
				alert = true
			}
		})
		if alert {
			notifyMerchant(order,
				"A payment event arrived for ref "+order.Reference+", but I hold no payment session for this order to verify it against, so I have not marked the order of "+money.GHS(charged)+" paid. Please check the Seev dashboard.",
			)
		}
		return 200
	}

	SettleVerified(order,
		"Payment received, thank you. Your "+order.ProductName+" is confirmed at "+money.GHS(charged)+". The shop has your order and will take it from here.",
	)
	return 200
}

// BuyerText composes the buyer's settlement confirmation when
// a composer is registered. The agent registers one that
// prints the payment confirmed record from the order's own
// facts (see the agent's records). Without a registered
// composer the caller's plain factual text is used as given.
var BuyerText = func(o *orders.Order, fallback string) string { return fallback }

// MerchantNoticeText composes the merchant's settlement
// notice when a composer is registered: the agent registers
// one that prints the order notice record, settled, from
// the order's facts and the settlement's fee and net.
// Without one, SettleVerified falls back to its plain text.
var MerchantNoticeText = func(o *orders.Order, fee, net float64) string { return "" }

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
	// The transition policy lives here, at the convergence
	// point every settlement road runs through, so no caller
	// has to invent its own: settle only from an open state,
	// no-op when already paid (a repeated event or a second
	// road must not tell both sides twice), and never
	// resurrect an order that already ended — a verified
	// payment for a cancelled or failed order is real money
	// in the wrong place, so the merchant is told once and
	// the order stands.
	settled, alert := false, false
	orders.Default.Update(order.Reference, func(o *orders.Order) {
		switch o.Status {
		case "paid":
		case "pending", "link_failed":
			o.Status = "paid"
			o.PaymentPath = "seev"
			settled = true
		default:
			if !o.SettleAlerted {
				o.SettleAlerted = true
				alert = true
			}
		}
	})
	// Everything past the transition speaks from the
	// stored record, never the caller's copy: callers hold
	// copies across long windows (the webhook fetches the
	// order, then makes a read-back of up to thirty
	// seconds; the sweep settles a pass-start snapshot),
	// and the second audit found a buyer who deleted her
	// data inside that window still being messaged from
	// the stale copy, her struck number and details
	// re-disclosed in the merchant notice. The fresh
	// record is the only one that knows she deleted.
	if fresh := orders.Default.GetOrder(order.Reference); fresh != nil {
		order = fresh
		charged = order.Charged()
	}
	if alert {
		notifyMerchant(order, "A payment of "+money.GHS(charged)+" was verified for order "+order.Reference+" ("+order.ProductName+"), which already stands "+order.Status+". I have not settled it or told the buyer it went through. Please check the Seev record for that payment.")
	}
	if !settled {
		return
	}
	s := checkout.SettlementFor(charged)
	if order.From != "deleted" {
		// A buyer who asked for her data to be deleted is
		// never messaged again, even by money arriving late.
		whatsapp.SendText(order.From, BuyerText(order, buyerText))
	}
	notice := MerchantNoticeText(order, s.Fee, s.Net)
	if notice == "" {
		notice = "Payment received\n\n*" + order.ProductName + "*\n" + money.GHS(charged) + " paid in full by the buyer.\nSeev fee " + money.GHS(s.Fee) + ", so " + money.GHS(s.Net) + " settles to your MoMo tomorrow.\n\nRef: " + order.Reference + "\nBuyer: " + order.From
	}
	notifyMerchant(order, notice)
}
