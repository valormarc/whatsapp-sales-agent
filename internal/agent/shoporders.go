package agent

// The merchant's orders desk, in chat: the list a dashboard
// would open with, and the one hard action on it, cancelling
// an order. Both print from the order book alone, on fixed
// paper: an order's state is a record, not a conversation.

import (
	"sort"
	"strconv"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// orderStateWords is an order's state as the merchant reads
// it in her list: money first, then where the goods are.
func orderStateWords(o orders.Order) string {
	switch o.Status {
	case "cancelled":
		return "cancelled"
	case "paid":
		switch o.DeliveryState {
		case "sent":
			return "paid, sent"
		case "received":
			return "paid, received"
		default:
			return "paid, not sent yet"
		}
	case "link_failed":
		return "payment link failed"
	default:
		return "waiting for payment"
	}
}

// merchantOrdersText is the orders view: her newest orders,
// newest first, each on one line with the facts she runs her
// day by, and a closing count of the paid orders still
// waiting to be sent, which is the work the list exists to
// show her.
func merchantOrdersText(merchant *catalogue.Merchant) string {
	mine := make([]orders.Order, 0)
	for _, o := range orders.Default.All() {
		if o.MerchantID == merchant.ID {
			mine = append(mine, o)
		}
	}
	if len(mine) == 0 {
		return "No orders yet. When a buyer completes a sale, it lands here: what she bought, what she paid, and where it stands."
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].CreatedAt > mine[j].CreatedAt })
	lines := []string{"Your orders, newest first:"}
	waiting := 0
	for _, o := range mine {
		if o.Status == "paid" && o.DeliveryState == "" {
			waiting++
		}
	}
	shown := mine
	if len(shown) > 10 {
		shown = shown[:10]
	}
	for _, o := range shown {
		total := o.Total
		if total == 0 {
			total = o.Price
		}
		lines = append(lines, "- "+o.Reference+" | "+o.ProductName+" | "+money.GHS(total)+" | buyer "+o.From+" | "+orderStateWords(o))
	}
	if len(mine) > len(shown) {
		lines = append(lines, "And "+strconv.Itoa(len(mine)-len(shown))+" earlier ones.")
	}
	if waiting == 1 {
		lines = append(lines, "1 paid order is waiting to be sent.")
	} else if waiting > 1 {
		lines = append(lines, strconv.Itoa(waiting)+" paid orders are waiting to be sent.")
	}
	return strings.Join(lines, "\n")
}

// cancelMerchantOrder cancels one of the merchant's own
// orders at her word, and keeps everyone honest about it:
// the buyer is told at once (with the refund she is owed
// named plainly when she had already paid), and the day
// book gives the sale back when the order was made on the
// day the book is keeping. An order already received is
// closed history and refuses; one under an open dispute
// settles there first.
func cancelMerchantOrder(from string, merchant *catalogue.Merchant, ref string) {
	order := orders.Default.GetOrder(ref)
	if order == nil || order.MerchantID != merchant.ID {
		whatsapp.SendText(from, "I cannot find order "+ref+" among your orders. Say \"my orders\" to see them with their references.")
		return
	}
	if order.Status == "cancelled" {
		whatsapp.SendText(from, "Order "+ref+" is already cancelled.")
		return
	}
	if order.DeliveryState == "received" {
		whatsapp.SendText(from, "Order "+ref+" has already been received by the buyer, so it cannot be cancelled. If something is wrong with it, that goes through a dispute.")
		return
	}
	if order.DisputeState == "open" || order.DisputeState == "refund_claimed" {
		whatsapp.SendText(from, "A dispute is open on order "+ref+" just now. That has to settle first; the buyer is waiting on it.")
		return
	}
	wasPaid := order.Status == "paid"
	paidTotal := order.Total
	if paidTotal == 0 {
		paidTotal = order.Price
	}
	orders.Default.Update(ref, func(o *orders.Order) { o.Status = "cancelled" })

	// The day book gives the sale back on the order's own
	// day: list figures are read from the shelf as it stands
	// (the prices she sells at now), which is the best record
	// of what the sale drew from the day.
	listTotal := order.Price
	if p := catalogue.FindByName(merchant.ID, order.ProductName); p != nil && len(order.Items) == 0 {
		listTotal = p.ListPrice
	} else if len(order.Items) > 0 {
		sum := 0.0
		ok := true
		for _, it := range order.Items {
			p := catalogue.FindByName(merchant.ID, it.Name)
			if p == nil {
				ok = false
				break
			}
			sum += p.ListPrice
		}
		if ok {
			listTotal = sum
		}
	}
	bookFixed := false
	if len(order.CreatedAt) >= 10 {
		bookFixed = daybook.Default.RecordCancellation(merchant.ID, daybook.Sale{
			ListPrice:   listTotal,
			AgreedPrice: order.Price,
			Date:        order.CreatedAt[:10],
		})
	}

	if wasPaid {
		whatsapp.SendText(order.From, "Order "+ref+" at "+merchant.Name+" has been cancelled by the shop. You paid "+money.GHS(paidTotal)+" for it, and the shop owes you that money back. If the refund does not reach you, tell me here and I will open a dispute on it.")
	} else {
		whatsapp.SendText(order.From, "Order "+ref+" at "+merchant.Name+" has been cancelled by the shop. Nothing was paid for it, and nothing is owed.")
	}
	reply := "Done. Order " + ref + " is cancelled, and the buyer has been told."
	if wasPaid {
		reply += " She had already paid " + money.GHS(paidTotal) + ", so she has been told the refund is owed to her."
	}
	if bookFixed {
		reply += " Today's book has been corrected."
	}
	whatsapp.SendText(from, reply)
}
