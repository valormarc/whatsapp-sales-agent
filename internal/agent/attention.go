package agent

// The merchant's attention list: what needs her, answered
// from the books alone, in the order a person would work
// through it — a buyer with a problem first, then goods
// that are paid for and waiting to move, then her own
// unfinished work, then what is merely waiting on buyers,
// and the day's figures to close. Like the orders desk, it
// prints on fixed paper: these are records, not a
// conversation, and no figure in it is composed by a draft.

import (
	"sort"
	"strconv"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/insights"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// merchantAttentionText composes the attention list for
// one shop. The session, when there is one, contributes her
// work in progress in this chat: an unsaved listing, a kept
// batch still waiting for its prices, kept photos not yet
// listed. Everything else comes from the durable books.
func merchantAttentionText(merchant *catalogue.Merchant, s *Session) string {
	mine := make([]orders.Order, 0)
	for _, o := range orders.Default.All() {
		if o.MerchantID == merchant.ID {
			mine = append(mine, o)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].CreatedAt > mine[j].CreatedAt })

	var disputes, toSend, waiting []orders.Order
	for _, o := range mine {
		switch {
		case o.DisputeState == "open" || o.DisputeState == "refund_claimed":
			disputes = append(disputes, o)
		case o.Status == "paid" && o.DeliveryState == "":
			toSend = append(toSend, o)
		case o.Unpaid():
			waiting = append(waiting, o)
		}
	}

	work := attentionWorkLines(s)
	needs := len(disputes) + len(toSend) + len(work)

	var lines []string
	if needs == 0 && len(waiting) == 0 {
		lines = append(lines, "Nothing needs you just now.")
	} else {
		lines = append(lines, "Here is what needs your attention:")
	}
	orderLine := func(o orders.Order, tail string) string {
		total := o.Total
		if total == 0 {
			total = o.Price
		}
		return "- " + o.Reference + " | " + o.ProductName + " | " + money.GHS(total) + " | buyer " + o.From + " | " + tail
	}
	for i, o := range disputes {
		if i == 5 {
			lines = append(lines, "And "+strconv.Itoa(len(disputes)-5)+" more with a dispute open.")
			break
		}
		tail := "a dispute is open on it"
		if o.DisputeState == "refund_claimed" {
			tail = "the buyer has claimed a refund on it"
		}
		lines = append(lines, orderLine(o, tail))
	}
	for i, o := range toSend {
		if i == 5 {
			lines = append(lines, "And "+strconv.Itoa(len(toSend)-5)+" more paid orders waiting to be sent.")
			break
		}
		lines = append(lines, orderLine(o, "paid, waiting to be sent"))
	}
	lines = append(lines, work...)
	if len(waiting) > 0 {
		owed := 0.0
		for _, o := range waiting {
			total := o.Total
			if total == 0 {
				total = o.Price
			}
			owed += total
		}
		noun := "orders are"
		if len(waiting) == 1 {
			noun = "order is"
		}
		lines = append(lines, strconv.Itoa(len(waiting))+" "+noun+" waiting for buyers to pay, "+money.GHS(owed)+" in all.")
	}
	day := daybook.Default.GetDayState(merchant.ID, daybook.Options{Allowance: merchant.DailyGiveawayAllowance})
	lines = append(lines, "Today so far: "+insights.DayReportText(day))
	return strings.Join(lines, "\n")
}

// attentionWorkLines names the merchant's own unfinished
// work in this conversation, from the session alone: the
// listing she is mid-way through, the batch kept from her
// photos, the photos held behind it.
func attentionWorkLines(s *Session) []string {
	if s == nil {
		return nil
	}
	var lines []string
	if f := s.Onboarding; f != nil {
		line := "- A listing is in progress and not saved yet"
		if f.Name != "" {
			line += ": " + f.Name
			if f.ListPrice > 0 {
				line += " at " + money.GHS(f.ListPrice)
			}
		}
		lines = append(lines, line+".")
	}
	if len(s.BurstBatch) > 0 {
		noun := "items"
		if len(s.BurstBatch) == 1 {
			noun = "item"
		}
		lines = append(lines, "- "+strconv.Itoa(len(s.BurstBatch))+" kept "+noun+" from your photos are waiting for their last prices.")
	}
	if len(s.BurstHeld) > 0 {
		if len(s.BurstHeld) == 1 {
			lines = append(lines, "- 1 kept photo is waiting to be listed.")
		} else {
			lines = append(lines, "- "+strconv.Itoa(len(s.BurstHeld))+" kept photos are waiting to be listed.")
		}
	}
	return lines
}
