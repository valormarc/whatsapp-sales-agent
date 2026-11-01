package agent

import (
	"sort"
	"strconv"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// The dispatch watch: the first order of business Stall
// pursues on its own initiative after the money. A paid
// order that is still waiting to go out long past its
// payment is pushed to the merchant once, naming the order,
// so the goods move without her having to ask what needs
// her. Until this watch the order's waiting was pull only:
// it stood in her attention answer when she asked, and
// nowhere else — an order could sit paid and unsent for
// days in a shop whose owner simply had a busy week. The
// push is once and never a nag (DispatchNudged on the
// order, the unpaid watch's discipline); the attention
// answer keeps listing the order for as long as it waits.
//
// The candidate test is the attention composer's own: paid,
// delivery state still empty, and no live dispute — a
// frozen order is dispute work, not dispatch work, and the
// buckets never double-count. Nothing here drafts: the
// nudge is spoken from the stored record, so no model
// budget is opened.

const (
	// dispatchAfter is how long a paid order may wait to go
	// out before it is pushed: long enough that an ordinary
	// same-day dispatch never trips it, short enough that a
	// forgotten order surfaces the same day it was paid.
	dispatchAfter = 3 * time.Hour
	// dispatchTick is the watch's pulse; nudges are same-day
	// matters, not minute matters.
	dispatchTick = 5 * time.Minute
	// maxDispatchNudgesPerPass bounds one pass the way the
	// payment watches bound theirs: a backlog after an
	// outage drains oldest first across ticks instead of
	// landing on merchants all at once.
	maxDispatchNudgesPerPass = 10
)

// CheckDispatchNudges runs one pass of the dispatch watch
// over the whole order book at the given moment.
func CheckDispatchNudges(now time.Time) {
	candidates := orders.Default.All()
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].CreatedAt < candidates[j].CreatedAt })
	sent := 0
	for _, o := range candidates {
		if sent >= maxDispatchNudgesPerPass {
			break
		}
		if o.Status != "paid" || o.DeliveryState != "" || o.DispatchNudged {
			continue
		}
		if o.DisputeState == "open" || o.DisputeState == "refund_claimed" {
			continue
		}
		created, err := time.Parse(time.RFC3339, o.CreatedAt)
		if err != nil {
			// An order placed before orders carried a time:
			// stamp it now and let the wait run from here,
			// the unpaid watch's rule, so a legacy order is
			// never pushed on first sight.
			ref := o.Reference
			orders.Default.Update(ref, func(x *orders.Order) {
				if x.CreatedAt == "" {
					x.CreatedAt = now.Format(time.RFC3339)
				}
			})
			continue
		}
		if now.Sub(created) < dispatchAfter {
			continue
		}
		// Claim the nudge inside the book's lock, on the
		// stored record still standing in the same state, so
		// two roads can never push the same order twice.
		ref := o.Reference
		claimed := false
		orders.Default.Update(ref, func(x *orders.Order) {
			if x.Status == "paid" && x.DeliveryState == "" && !x.DispatchNudged {
				x.DispatchNudged = true
				claimed = true
			}
		})
		if !claimed {
			continue
		}
		stored := orders.Default.GetOrder(ref)
		if stored == nil {
			continue
		}
		phone := merchantNotifyPhone(catalogue.GetMerchant(stored.MerchantID))
		if phone == "" {
			continue
		}
		sent++
		whatsapp.SendText(phone, dispatchNudgeText(*stored, now.Sub(created)))
	}
}

// dispatchNudgeText speaks the push from the stored record:
// what is waiting, whose it is, what was paid and how long
// ago, and the one move that closes it.
func dispatchNudgeText(o orders.Order, age time.Duration) string {
	ageText := "over a day ago"
	if hours := int(age.Hours()); hours < 24 {
		ageText = strconv.Itoa(hours) + " hours ago"
	}
	text := "A paid order is still waiting to go out.\n\nOrder: " + o.Reference +
		"\nItem: " + o.ProductName +
		"\nBuyer: " + o.From
	if o.Location != "" {
		text += ", in " + o.Location
	}
	text += "\nPaid: " + money.GHS(o.Charged()) + ", " + ageText +
		"\n\nWhen it goes out, tell me here and I will let the buyer know it is on its way."
	return text
}
