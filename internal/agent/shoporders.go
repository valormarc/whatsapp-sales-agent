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
		case "missing":
			return "paid, reported missing"
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
	lines := []string{"*Your orders, newest first:*"}
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
		lines = append(lines, "", "1 paid order is waiting to be sent.")
	} else if waiting > 1 {
		lines = append(lines, "", strconv.Itoa(waiting)+" paid orders are waiting to be sent.")
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
	// The cancellation is decided inside the book's lock,
	// from the stored record (the fifth audit found every
	// guard read from the caller's snapshot while the
	// status write was unconditional: a protected
	// transition landing after the snapshot — the buyer
	// confirming receipt, a dispute opening — was
	// overwritten, and both sides were told the
	// snapshot's story). One Update re-checks the stored
	// state and only then cancels, capturing the facts
	// the rest of this road speaks from.
	var applied bool
	var refusal string
	var wasPaid bool
	var priorStatus string
	var paidTotal float64
	var buyerFrom, createdAt, productName string
	var agreedPrice float64
	var bookedList, orderQty float64
	var items []orders.OrderItem
	orders.Default.Update(ref, func(o *orders.Order) {
		switch {
		case o.Status == "cancelled":
			refusal = "Order " + ref + " is already cancelled."
			return
		case o.DeliveryState == "received":
			refusal = "Order " + ref + " has already been received by the buyer, so it cannot be cancelled. If something is wrong with it, that goes through a dispute."
			return
		case o.DisputeState == "open" || o.DisputeState == "refund_claimed":
			refusal = "A dispute is open on order " + ref + " just now. That has to settle first; the buyer is waiting on it."
			return
		}
		applied = true
		priorStatus = o.Status
		wasPaid = o.Status == "paid"
		paidTotal = o.Total
		if paidTotal == 0 {
			paidTotal = o.Price
		}
		buyerFrom, createdAt, productName, items = o.From, o.CreatedAt, o.ProductName, o.Items
		agreedPrice = o.Price
		bookedList, orderQty = o.BookedList, o.Quantity
		o.Status = "cancelled"
	})
	if !applied {
		whatsapp.SendText(from, refusal)
		return
	}
	// An unpaid order that dies gives its shelf units back;
	// a paid one's goods are the buyer's until a refund
	// settles, so its stock stays taken. The giving is
	// claimed inside the order book's lock: another road
	// ending the same order at the same moment (the
	// sweep, a webhook) claims first or not at all, and
	// the units move once.
	if !wasPaid {
		if lines, ok := orders.Default.ClaimRestock(ref); ok {
			giveStockLines(order.MerchantID, lines)
		}
	}

	// The day book gives the sale back on the order's own
	// day, line by line at the figures each line was
	// BOOKED with (the sixth audit): the booking recorded
	// one sale per line, so the reversal records one
	// cancellation per line — a single recomputed total
	// read from the shelf as it stands now corrupted the
	// sales count (one decrement for an N-line booking)
	// and the giveaway pool (a by-measure line's booked
	// list is its scaled list, not the bare shelf price,
	// and a renamed or repriced product recomputed to a
	// different figure entirely). Lines booked before the
	// figures were recorded fall back to the shelf's
	// scaled list for the recorded quantity, and to the
	// agreed price when the product is gone.
	bookedLineList := func(name string, recorded, agreed, qty float64) float64 {
		if recorded > 0 {
			return recorded
		}
		if p := catalogue.FindByName(merchant.ID, name); p != nil {
			if qty > 0 {
				return p.ScaledList(qty)
			}
			return p.ListPrice
		}
		return agreed
	}
	// The reversal answers a booking: an order whose link
	// never came up was recorded WITHOUT one (the sale
	// could not complete), and the third audit found its
	// cancellation reversing figures that were never
	// booked — a genuine sale's day erased by a phantom.
	// A link_failed order has nothing in the book to give
	// back.
	bookFixed := false
	if priorStatus != "link_failed" && len(createdAt) >= 10 {
		date := createdAt[:10]
		if len(items) > 0 {
			for _, it := range items {
				if daybook.Default.RecordCancellation(merchant.ID, daybook.Sale{
					ListPrice:   bookedLineList(it.Name, it.ListPrice, it.Price, it.Quantity),
					AgreedPrice: it.Price,
					Date:        date,
				}) {
					bookFixed = true
				}
			}
		} else {
			bookFixed = daybook.Default.RecordCancellation(merchant.ID, daybook.Sale{
				ListPrice:   bookedLineList(productName, bookedList, agreedPrice, orderQty),
				AgreedPrice: agreedPrice,
				Date:        date,
			})
		}
	}

	// An erased buyer is told nothing — there is no one
	// at "deleted" to tell — and the merchant is not told
	// she was told (the third audit found the notice
	// addressed to the tombstone and reported as sent).
	told := buyerFrom != "deleted"
	if told {
		if wasPaid {
			whatsapp.SendText(buyerFrom, "Order "+ref+" at "+merchant.Name+" has been cancelled by the shop. You paid "+money.GHS(paidTotal)+" for it, and the shop owes you that money back. If the refund does not reach you, tell me here and I will open a dispute on it.")
		} else {
			whatsapp.SendText(buyerFrom, "Order "+ref+" at "+merchant.Name+" has been cancelled by the shop. Nothing was paid for it, and nothing is owed.")
		}
	}
	reply := "Done. Order " + ref + " is cancelled"
	if told {
		reply += ", and the buyer has been told."
	} else {
		reply += ". The buyer's details were deleted at her request, so she could not be told here."
	}
	if wasPaid && told {
		reply += " She had already paid " + money.GHS(paidTotal) + ", so she has been told the refund is owed to her."
	}
	if wasPaid && !told {
		reply += " She had already paid " + money.GHS(paidTotal) + ", and that refund is still owed to her."
	}
	if bookFixed {
		reply += " Today's book has been corrected."
	}
	whatsapp.SendText(from, reply)
}
