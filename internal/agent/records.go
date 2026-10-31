package agent

// Clean records. The agent's outbound messages come in two
// families. TALK (haggling, answers, questions) is alive:
// short pieces, the model's own words, exactly as it has
// always been. RECORDS (any message that states money as a
// settled fact: the price quote, the final total, payment
// confirmed, the seller's order notice) are printed on
// fixed paper: one single message, never split by the
// piece-by-piece sender, the same lines in the same order
// every time, the contents filled only from verified
// facts. This is layout, not scripted intelligence: the
// model still does all the understanding and negotiation;
// only the print shape of a record is fixed, the way a
// receipt book prints every sale on the same lines without
// deciding anything about the sale.

import (
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/insights"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// quoteRecord is the price quote, on its paper:
//
//	Ankara fabric, 6 yards
//	Price: GH₵450
//
// The item line carries the measure when the goods are
// sold by one (the listing's own base quantity and unit).
func quoteRecord(p *catalogue.Product) string {
	item := p.Name
	if p.Unit != "" && p.BaseQty > 0 {
		item += ", " + formatQty(p.BaseQty) + " " + unitWord(p.Unit, p.BaseQty)
	}
	return item + "\nPrice: " + money.GHS(p.ListPrice)
}

// sendProductView presents one product the clean way: the
// description as talk, the quote as a record on its own
// paper, and the invitation to bargain as talk. The three
// never share a message, so the price always lands on the
// same lines whatever surrounds it.
func sendProductView(from string, p *catalogue.Product, invite string) {
	if strings.TrimSpace(p.Description) != "" {
		whatsapp.SendText(from, p.Name+": "+p.Description)
	}
	whatsapp.SendText(from, quoteRecord(p))
	if tail := strings.TrimSpace(offerInvite(p, invite)); tail != "" {
		whatsapp.SendText(from, tail)
	}
}

// sendProductViewLead is sendProductView with a caller-led
// opening ("It is this: ...", "For that, the X is the one:
// ...") in place of the plain description piece.
func sendProductViewLead(from, lead string, p *catalogue.Product, invite string) {
	if strings.TrimSpace(lead) != "" {
		whatsapp.SendText(from, lead)
	}
	whatsapp.SendText(from, quoteRecord(p))
	if tail := strings.TrimSpace(offerInvite(p, invite)); tail != "" {
		whatsapp.SendText(from, tail)
	}
}

// totalRecord is the final total, on its paper:
//
//	Ankle Strap Heels: GH₵110
//	Delivery to Osu: GH₵25
//	Total: GH₵135
//
// One line per item at its agreed price, the delivery line
// when the order carries a fee, then the total the buyer
// pays here. When the carrier takes the delivery money the
// delivery line says so on its face, the total stays what
// she pays here, and a closing line gives the two together,
// so the paper never asks her to add anything up and never
// counts the carrier's money as the shop's.
func totalRecord(items []orders.OrderItem, deliveryFee, chargedFee float64, mode, location, paidTo, deliveryNote string) string {
	var lines []string
	goods := 0.0
	for _, it := range items {
		lines = append(lines, it.Name+": "+money.GHS(it.Price))
		goods += it.Price
	}
	if mode == "delivery" && deliveryFee > 0 {
		line := "Delivery"
		if location != "" {
			line += " to " + location
		}
		line += ": " + money.GHS(deliveryFee)
		if paidTo == "carrier" {
			line += " (paid to " + carrierTarget(deliveryNote) + " on arrival)"
		}
		lines = append(lines, line)
	}
	if mode == "delivery" && deliveryFee <= 0 {
		lines = append(lines, "Delivery: arranged with the seller, no fixed fee.")
	}
	lines = append(lines, "Total: "+money.GHS(goods+chargedFee))
	if mode == "delivery" && paidTo == "carrier" && deliveryFee > 0 {
		lines = append(lines, "That is "+money.GHS(goods+deliveryFee)+" in all, with the delivery.")
	}
	return strings.Join(lines, "\n")
}

// orderItemLabel names an order's goods the way the buyer
// thinks of them: a measured order by its cut, a basket by
// its joined label (already the order's ProductName), a
// sized single item with its size.
func orderItemLabel(o *orders.Order) string {
	if len(o.Items) > 1 {
		return o.ProductName
	}
	item := o.ProductName
	if o.Quantity > 0 && o.Unit != "" {
		item = formatQty(o.Quantity) + " " + unitWord(o.Unit, o.Quantity) + " of " + o.ProductName
	}
	if len(o.Items) == 1 && o.Items[0].Size != "" {
		item += ", size " + o.Items[0].Size
	} else if len(o.Items) == 0 && o.Size != "" {
		item += ", size " + o.Size
	}
	return item
}

// paymentConfirmedRecord is the buyer's payment receipt,
// on its paper: the confirmation, the goods, the exact
// amount, the delivery facts the order carries, the
// reference, and the shop's thanks. It reads the same on
// every road that confirms a payment — the webhook, the
// agent's own read-back, the unpaid watch — because the
// facts it prints are the order's own.
func paymentConfirmedRecord(o *orders.Order) string {
	lines := []string{"Payment confirmed", "Item: " + orderItemLabel(o), "Amount: " + money.GHS(o.Charged())}
	switch o.Fulfilment {
	case "delivery":
		if o.Location != "" {
			lines = append(lines, "Delivery to: "+o.Location)
		}
		if o.DeliveryFee > 0 && o.DeliveryPaidTo == "carrier" {
			lines = append(lines, "Delivery fee: "+money.GHS(o.DeliveryFee)+" to pay on arrival")
		}
	case "pickup":
		if o.Collector != "" {
			lines = append(lines, "Collection: "+o.Collector+" collects it from the shop")
		}
	case "meet":
		if o.Location != "" {
			lines = append(lines, "Meeting place: "+o.Location)
		}
	case "arranged":
		if o.Location != "" {
			lines = append(lines, "Delivery: arranged with the seller, to "+o.Location)
		}
	}
	lines = append(lines, "Order: "+o.Reference)
	if m := catalogue.GetMerchant(o.MerchantID); m != nil && m.Name != "" {
		lines = append(lines, "Thank you for buying from "+m.Name+".")
	}
	return strings.Join(lines, "\n")
}

// orderNoticeLines is the seller's order notice up to the
// money status, in the fixed order: the header with the
// reference, the buyer, one Item line per item, the agreed
// price, the delivery instruction, and the package details
// the seller works from. The caller appends the total, the
// payment status and the day's figures, which differ
// between the sale's opening notice and its settlement.
// flatLine renders buyer-supplied text inside the
// line-structured record: its line breaks become spaces,
// so the text can never start a line of its own at the
// record's indentation (the sixth audit).
func flatLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r'
	}), " ")
}

func orderNoticeLines(o *orders.Order, merchant *catalogue.Merchant) []string {
	lines := []string{"New order " + o.Reference, "Buyer: " + o.From}
	items := o.Items
	if len(items) == 0 {
		items = []orders.OrderItem{{Name: o.ProductName, Price: o.Price, Quantity: o.Quantity, Unit: o.Unit, Size: o.Size}}
	}
	multi := len(items) > 1
	for _, it := range items {
		label := it.Name
		if it.Quantity > 0 && it.Unit != "" {
			// A measured line's stored name already
			// carries its measure ("3 yards of Ankara
			// fabric" is the name the basket kept), and
			// the buyer's own records rely on that —
			// prefixing it again states the quantity
			// twice in the seller's one fulfilment
			// record (the fifth audit). The prefix is
			// added only when the name does not already
			// carry it.
			prefix := formatQty(it.Quantity) + " " + unitWord(it.Unit, it.Quantity) + " of "
			// Any leading measure prefix counts, not
			// only the exact one rebuilt from the
			// line's fields (the sixth audit): when
			// the name was rendered from a measure the
			// fields no longer agree with — a re-set
			// measure, a buy-again line — the exact
			// test failed and the notice stated the
			// quantity twice ("3 yards of 3 metres of
			// Ankara Fabric"). A name that already
			// opens with a quantity phrase and its
			// "of" carries a measure; print it as
			// stored.
			carries := strings.HasPrefix(it.Name, prefix)
			if !carries {
				if loc := quantityRe.FindStringIndex(it.Name); loc != nil && loc[0] == 0 && strings.HasPrefix(strings.TrimSpace(it.Name[loc[1]:]), "of ") {
					carries = true
				}
			}
			if !carries {
				label = prefix + it.Name
			}
		}
		if it.Size != "" {
			label += ", size " + it.Size
		}
		line := "Item: " + label
		if multi {
			line += ": " + money.GHS(it.Price)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "Agreed price: "+money.GHS(o.Price))
	lines = append(lines, deliveryInstruction(o, merchant))
	if o.PackageName != "" {
		lines = append(lines, "Package name: "+flatLine(o.PackageName))
	}
	if o.Landmark != "" {
		lines = append(lines, "Landmark: "+flatLine(o.Landmark))
	}
	if o.DeliveryPhone != "" {
		lines = append(lines, "Delivery phone: "+o.DeliveryPhone)
	}
	if o.Note != "" {
		// The note renders flattened and quoted (the
		// sixth audit): even a legacy stored note with
		// line breaks of its own cannot start a line of
		// the record at the record's own indentation and
		// pass as one of its lines.
		lines = append(lines, "Buyer's message: "+flatLine(o.Note))
	}
	return lines
}

// deliveryInstruction is the notice's delivery line, label
// and all: how the goods move, in the order's own facts,
// with the carrier purse spelled out when the fee is the
// carrier's, so the seller never counts it as hers.
func deliveryInstruction(o *orders.Order, merchant *catalogue.Merchant) string {
	switch o.Fulfilment {
	case "delivery":
		note := ""
		if merchant != nil {
			note = strings.TrimRight(strings.TrimSpace(merchant.DeliveryNote), ".")
		}
		var line string
		switch {
		case note != "" && o.Location != "":
			line = note + ", to " + o.Location
		case note != "":
			line = note
		case o.Location != "":
			line = "to " + o.Location
		default:
			line = "arranged with the buyer; she gave no location"
		}
		switch {
		case o.DeliveryPaidTo == "carrier" && o.DeliveryFee > 0:
			rawNote := ""
			if merchant != nil {
				rawNote = merchant.DeliveryNote
			}
			line += ". The " + money.GHS(o.DeliveryFee) + " delivery is hers to pay " + carrierTarget(rawNote) + " on arrival; your money is the " + money.GHS(o.Price) + "."
		case o.DeliveryFee > 0 && o.DeliveryPaidTo == "merchant":
			line += ", fee " + money.GHS(o.DeliveryFee) + " included in the total."
		default:
			line += "."
		}
		return "Delivery: " + line
	case "pickup":
		if o.Collector != "" {
			return "Pickup: a collector is coming for it: " + o.Collector + "."
		}
		return "Pickup: the buyer will come and pick it up herself."
	case "meet":
		return "Meet up: the buyer wants to meet at " + o.Location + " to collect it."
	default: // arranged
		if o.Location != "" {
			return "Delivery: to be arranged with the buyer. She is in " + o.Location + "."
		}
		return "Delivery: to be arranged with the buyer. No location given."
	}
}

// saleNoticeRecord is the seller's notice when a sale
// opens: the fixed lines, the total she is owed, payment
// waiting, and the day so far at the foot.
func saleNoticeRecord(o *orders.Order, merchant *catalogue.Merchant, day daybook.State) string {
	lines := orderNoticeLines(o, merchant)
	lines = append(lines, "Total: "+money.GHS(o.Charged()))
	lines = append(lines, "Payment: waiting")
	lines = append(lines, "")
	lines = append(lines, insights.DayReportLines(day)...)
	return strings.Join(lines, "\n")
}

// settlementNoticeRecord is the same notice at settlement:
// total paid, payment confirmed, what the payment service
// keeps and what settles to her MoMo, and the day so far.
func settlementNoticeRecord(o *orders.Order, merchant *catalogue.Merchant, fee, net float64, day daybook.State) string {
	lines := orderNoticeLines(o, merchant)
	lines = append(lines, "Total paid: "+money.GHS(o.Charged()))
	lines = append(lines, "Payment: confirmed")
	lines = append(lines, "Seev fee "+money.GHS(fee)+", so "+money.GHS(net)+" settles to your MoMo tomorrow.")
	if merchant != nil && sellerIsNew(merchant.OwnerWhatsApp) {
		// A new seller hears the platform's position in
		// plain words on her own receipt: her money moves on
		// the rail's schedule, not Stall's, and what Stall
		// watches is the delivery.
		lines = append(lines, "You are new on Stall: buyers are told so until your first deliveries are confirmed received. A dispute on this order goes on your record.")
	}
	lines = append(lines, "")
	lines = append(lines, insights.DayReportLines(day)...)
	return strings.Join(lines, "\n")
}
