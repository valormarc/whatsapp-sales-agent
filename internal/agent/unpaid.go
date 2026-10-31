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
	"fmt"
	"os"
	"sort"
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

	// The bounds of one sweep pass (see CheckUnpaidOrders):
	// at most this many provider read-backs, inside this
	// much wall time, before the rest carries to the next
	// tick.
	maxVerifyPerPass  = 25
	sweepPassDeadline = 2 * time.Minute
	// The sibling expiry passes the same sweep runs first
	// (fee quotes, in-hand asks, disputes) expire and
	// message per item with no provider call to slow them,
	// and the third audit found them unbounded: a backlog
	// of thousands of stale items would be actioned and
	// messaged in one pass, hours of sequential sends
	// ahead of the bounded order pass they exist to feed.
	// Each pre-pass actions at most this many per sweep;
	// the rest stay open and carry to the next tick.
	maxPrePassPerSweep = 25
	// The item cap alone left the pre-passes time
	// unbounded (the fourth audit): each actioned item
	// can make a provider read-back or a paced send, and
	// 75 of them in sequence stall the serial sweep for
	// minutes while the order pass's own clock has not
	// even started. The pre-passes share this one budget
	// per sweep, checked between actions; what it cuts
	// off stays open and carries to the next tick, in
	// step with its messages.
	prePassBudget = 60 * time.Second
)

// prePassDeadline is the moment the current sweep's
// pre-pass budget runs out, set by CheckUnpaidOrders
// before the passes run. The zero value (passes invoked
// outside a sweep, as tests do) means no budget.
var prePassDeadline time.Time

func prePassSpent() bool {
	return !prePassDeadline.IsZero() && time.Now().After(prePassDeadline)
}

var unpaidWatchOnce sync.Once

// StartUnpaidWatch starts the watch's slow tick. It is called
// once by the server as it starts; tests drive the pass
// directly and never start the tick.
func StartUnpaidWatch() {
	unpaidWatchOnce.Do(func() {
		go func() {
			for {
				// A panic in one pass must never take the
				// whole process down with it: the watch
				// recovers, says so, and ticks again. (The
				// second audit's crash-loop began as a
				// single unguarded dereference in this
				// goroutine, which had no recover at all.)
				func() {
					defer func() {
						if r := recover(); r != nil {
							fmt.Fprintf(os.Stderr, "agent: unpaid watch pass panicked and was recovered: %v\n", r)
						}
					}()
					CheckUnpaidOrders(time.Now().UTC())
				}()
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
	sweepToken := model.BeginTurn()
	defer model.EndTurn(sweepToken)
	prePassDeadline = time.Now().Add(prePassBudget)
	defer func() { prePassDeadline = time.Time{} }()
	// The same sweep carries the delivery-fee timeout: a
	// seller who has not quoted in time leaves her buyer
	// offered the other ways (see deliveryfees.go).
	CheckFeeQuotes(now)
	// And the in-hand timeout (see inhand.go), and the
	// dispute clock (see disputes.go).
	CheckInHand(now)
	CheckDisputes(now)
	// The order pass is bounded: read-backs run oldest
	// first, at most maxVerifyPerPass of them, inside a
	// pass deadline, and the pass stops early when the
	// provider fails repeatedly — a backlog after an outage
	// must never turn one pass into an hours-long sequential
	// crawl that starves the clocks above (the 2026-10-30
	// audit's arithmetic: stale count times a 30 second
	// client timeout, each). What does not fit carries to
	// the next tick, oldest still first.
	passStart := time.Now()
	verifies := 0
	providerFails := 0
	candidates := orders.Default.All()
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].CreatedAt < candidates[j].CreatedAt })
	for _, o := range candidates {
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
		if verifies >= maxVerifyPerPass || time.Since(passStart) > sweepPassDeadline {
			break
		}
		if checkout.Enabled() {
			verifies++
			// The session this pass is about to verify:
			// the failed-flip below binds itself to it
			// (the fifth audit found the flip re-checking
			// the stored status but not the stored
			// session, so a read-back of a superseded
			// session flipped an order whose live session
			// was never the one verified).
			verifiedRef := *o.ProviderRef
			check := checkout.VerifyPayment(verifiedRef)
			if check == nil {
				providerFails++
				if providerFails >= 3 {
					break
				}
			} else {
				providerFails = 0
			}
			if check != nil {
				switch {
				case checkout.PaidAmountMatches(check, o.Charged()):
					oo := o
					seevwebhook.SettleVerified(&oo, "Payment received, thank you. Your "+o.ProductName+" is confirmed at "+money.GHS(o.Charged())+". The shop has your order and will take it from here.")
					continue
				case checkout.IsPaidStatus(check.Status) && check.AmountMajor == nil:
					// Paid, but the read-back states no
					// amount: a contractually expected
					// shape (the checkout package documents
					// the amount as nil when it was missing
					// or not a number), and the second
					// audit found this branch dereferencing
					// it — a panic in the watch goroutine
					// that crash-looped the whole process
					// on the same order after every
					// restart. Status alone settles
					// nothing: hold, tell the merchant in
					// the amount-absent words, mark the
					// order surfaced, and move on.
					if phone := merchantNotifyPhone(catalogue.GetMerchant(o.MerchantID)); phone != "" {
						whatsapp.SendText(phone, "Payment read-back without an amount on order "+o.Reference+": Seev reports the session paid but states no amount, so I have not marked the order of "+money.GHS(o.Charged())+" paid. Please check the Seev record with me.")
					}
					ref := o.Reference
					orders.Default.Update(ref, func(x *orders.Order) { x.UnpaidAlerted = true })
					continue
				case checkout.IsPaidStatus(check.Status):
					if phone := merchantNotifyPhone(catalogue.GetMerchant(o.MerchantID)); phone != "" {
						whatsapp.SendText(phone, "Payment amount mismatch on order "+o.Reference+": the order is "+money.GHS(o.Charged())+" but Seev reports "+money.GHS(*check.AmountMajor)+". I have not marked it paid. Please check the Seev record with me.")
					}
					ref := o.Reference
					orders.Default.Update(ref, func(x *orders.Order) { x.UnpaidAlerted = true })
					continue
				case checkout.IsFailedStatus(check.Status):
					// The flip is conditional on the STORED
					// order still standing open, and both
					// messages speak from the stored record:
					// the third audit found this branch
					// flipping and messaging from the
					// pass-start snapshot, overwriting a
					// cancellation (or a settlement) that
					// landed while the pass was verifying,
					// and telling both sides a failure story
					// about an order that had already ended
					// another way. SettleVerified keeps this
					// same discipline at its convergence
					// point; the sweep keeps it here.
					flipped := false
					orders.Default.Update(o.Reference, func(x *orders.Order) {
						if (x.Status == "pending" || x.Status == "link_failed") &&
							x.ProviderRef != nil && *x.ProviderRef == verifiedRef {
							x.Status = "failed"
							flipped = true
						}
					})
					if !flipped {
						continue
					}
					stored := orders.Default.GetOrder(o.Reference)
					if stored == nil {
						continue
					}
					if lines, ok := orders.Default.ClaimRestock(stored.Reference); ok {
						giveStockLines(stored.MerchantID, lines)
					}
					if stored.From != "deleted" {
						whatsapp.SendText(stored.From, "Your payment for the "+stored.ProductName+" did not go through, so nothing has been charged. The shop has been told, and you can try the link again or talk to the shop here.")
					}
					if phone := merchantNotifyPhone(catalogue.GetMerchant(stored.MerchantID)); phone != "" {
						whatsapp.SendText(phone, "Payment failed\n\n*"+stored.ProductName+"*\nThe buyer's payment of "+money.GHS(stored.Charged())+" did not go through. Nothing was charged.\n\nRef: "+stored.Reference+"\nBuyer: "+stored.From)
					}
					continue
				}
			}
		}
		// The reminder is decided from the stored record,
		// re-read here (the fifth audit found it sent from
		// the pass-start snapshot: an order settled or
		// cancelled while the pass verified earlier
		// candidates was reminded about as though still
		// open, in the snapshot's figures). It proceeds
		// only while the stored order still stands unpaid
		// and unreminded, speaks from the stored record,
		// and marks the reminder on that same re-verified
		// state.
		stored := orders.Default.GetOrder(o.Reference)
		if stored == nil || !stored.Unpaid() || stored.UnpaidAlerted {
			continue
		}
		if stored.From != "deleted" {
			whatsapp.SendText(stored.From, "Your payment for the "+stored.ProductName+" has not come through yet. Your order "+stored.Reference+" for "+money.GHS(stored.Charged())+" is still open. You can pay with the link I sent you earlier, or talk to the shop here if something went wrong.")
		}
		if phone := merchantNotifyPhone(catalogue.GetMerchant(stored.MerchantID)); phone != "" {
			whatsapp.SendText(phone, "Payment not received yet\n\n*"+stored.ProductName+"*\nOrder "+stored.Reference+" for "+money.GHS(stored.Charged())+" has been open for over 30 minutes without payment.\n\nBuyer: "+stored.From+"\nThe buyer has been reminded. The order is still open.")
		}
		orders.Default.Update(stored.Reference, func(x *orders.Order) {
			if x.Unpaid() {
				x.UnpaidAlerted = true
			}
		})
	}
}
