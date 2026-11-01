package agent

// The fresh payment watch. The webhook is the instant road
// for a completed payment, but a webhook that never arrives
// must not leave a paid buyer standing in silence until she
// comes back and reports the payment herself — that was the
// live behaviour: the confirmation only moved when the
// buyer spoke, because the only other road, the unpaid
// watch, does not look at an order until it is thirty
// minutes old, and by design: it is a reminder watch, not a
// confirmation watch. This pass is the confirmation watch.
// On a short tick it reads back the Seev session of every
// fresh pending order — one created in the last few
// minutes, the stretch in which a buyer is actually at the
// checkout — and the moment the service says the money has
// landed, in the exact amount the order carries, the order
// settles through the same convergence point every
// settlement road runs through, and both sides are told at
// once. Nothing else is actioned here: a payment that has
// failed, a read-back with no amount, an amount that does
// not match — those keep the roads they already have (the
// webhook, the buyer's own report, the unpaid watch), so
// this pass can only ever add the one thing that was
// missing: a confirmation that arrives on its own, within
// the minute, while the buyer is still at the counter.

import (
	"sort"
	"time"

	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/seevwebhook"
)

const (
	// freshWindow is how long after its creation an order
	// belongs to this watch: the stretch in which a buyer
	// who took the link is completing it. Past the window
	// the unpaid watch owns the order.
	freshWindow = 10 * time.Minute
	// freshTick is the watch's beat. A payment that lands
	// just after a pass is confirmed on the next one, so a
	// buyer hears within about a tick of paying.
	freshTick = 30 * time.Second

	// The bounds of one pass, in the unpaid watch's own
	// discipline: a handful of read-backs, oldest first,
	// and the pass stops early when the provider fails
	// repeatedly rather than crawling a backlog.
	maxFreshVerifyPerPass = 10
)

// CheckFreshPayments runs one pass of the fresh payment
// watch over the order book at the given moment.
func CheckFreshPayments(now time.Time) {
	if !checkout.Enabled() {
		return
	}
	// A settlement this pass triggers may draft; the pass
	// opens a fresh budget the way the unpaid sweep does.
	token := model.BeginTurn()
	defer model.EndTurn(token)
	candidates := orders.Default.All()
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].CreatedAt < candidates[j].CreatedAt })
	verifies := 0
	providerFails := 0
	for _, o := range candidates {
		if o.Status != "pending" || o.ProviderRef == nil || *o.ProviderRef == "" {
			continue
		}
		created, err := time.Parse(time.RFC3339, o.CreatedAt)
		if err != nil || now.Sub(created) > freshWindow || now.Before(created) {
			continue
		}
		if verifies >= maxFreshVerifyPerPass {
			break
		}
		verifies++
		check := checkout.VerifyPayment(*o.ProviderRef)
		if check == nil {
			providerFails++
			if providerFails >= 3 {
				break
			}
			continue
		}
		providerFails = 0
		if checkout.PaidAmountMatches(check, o.Charged()) {
			oo := o
			seevwebhook.SettleVerified(&oo, "Payment received, thank you. Your "+o.ProductName+" is confirmed at "+money.GHS(o.Charged())+". The shop has your order and will take it from here.")
		}
	}
}
