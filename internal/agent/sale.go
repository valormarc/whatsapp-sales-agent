// Completing a sale: the order is recorded, the day book
// books it, the buyer gets her payment paths, and the merchant
// is told the whole story.
package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// refTailAlphabet has no look-alike characters: no 0 with O,
// no 1 with I or L, so a tail read aloud or typed from a
// receipt is the tail that was minted.
const refTailAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// refTail draws a new reference's unguessable tail, three
// characters from the crypto source.
func refTail() string {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Never expected. Uniqueness still rests on the
		// serial; only the unguessability is thinner.
		return "XXX"
	}
	out := make([]byte, 3)
	for i, v := range b {
		out[i] = refTailAlphabet[int(v)%len(refTailAlphabet)]
	}
	return string(out)
}

// completeSale finishes a sale whose fulfilment is settled: the
// order is recorded with how the goods move, the day book books
// the item price, the payment link goes out, and the merchant is
// told the whole story, including who to hand the goods to.


// liveAgreementOrder finds the order this session's sale
// already minted, by the sale identity the session carries
// (see beginFulfilment). It is the replay guard for a stale
// session restore: the snapshot cadence can hand back a
// session from just before its completion, and the buyer
// answering again must meet her order, not a twin. A new
// purchase — buy-again included — begins a new fulfilment
// and carries a new identity, so it can never collide.
func liveAgreementOrder(shopID, from string, s *Session) *orders.Order {
	if s.SaleID == "" {
		return nil
	}
	for _, o := range orders.Default.All() {
		oo := o
		if oo.SaleID == s.SaleID && oo.MerchantID == shopID && oo.From == from {
			return &oo
		}
	}
	return nil
}

// resumeAgreementOrder answers a completion that turns out
// to be a replay: the order already stands, so the buyer is
// told its reference and, while it still awaits payment,
// handed its payment button again. No second order, no
// second day-book line, no second draw on the shelf.
func resumeAgreementOrder(from string, s *Session, merchant *catalogue.Merchant, o *orders.Order) {
	shopID := s.MerchantID
	if o.Status == "paid" {
		whatsapp.SendText(from, "That order is already confirmed: "+o.Reference+". The shop has it and will take it from here.")
	} else if o.Status != "pending" && o.Status != "link_failed" {
		// The second audit found this road re-minting a
		// payment link for orders that had already ended —
		// cancelled by the merchant or failed by the sweep —
		// overwriting the ended order's session reference
		// and sending the buyer a live button for money
		// settlement would refuse. An ended order is told
		// as ended; nothing is minted on it, ever.
		whatsapp.SendText(from, "That order, "+o.Reference+", has ended, so there is nothing to pay on it. If you still want the item, tell me and we will start it fresh.")
	} else if link := checkout.CreatePaymentLink(checkout.LinkRequest{Amount: o.Charged(), Reference: o.Reference, Subaccount: subaccountFor(merchant)}); link.URL != nil {
		// The order's session attributes must always
		// describe its LATEST mint: the third audit found
		// this road updating ProviderRef alone, leaving a
		// stale DryRun flag from an earlier disabled-
		// checkout mint riding on a real session — and,
		// once the session was later struck, the stale
		// flag borrowed the dry-run settlement exemption
		// for a real order. Rebind both together, always.
		// And the rebind itself is conditional, decided
		// from the stored record inside the book's lock
		// (the fifth audit): the mint can take seconds and
		// the order can end or settle inside that window —
		// the merchant cancels it, the sweep flips it, the
		// buyer pays the original session — and this road
		// rebound the ended order to a brand-new live
		// session and sent its buyer a working button for
		// it. The re-issue road has carried this discipline
		// since the third audit; the resume road now does
		// too: rebind only a stored order still open, and
		// answer from the stored record otherwise.
		applied := false
		orders.Default.Update(o.Reference, func(x *orders.Order) {
			if x.Status != "pending" && x.Status != "link_failed" {
				return
			}
			applied = true
			x.ProviderRef = link.ProviderRef
			x.DryRun = link.DryRun
		})
		if applied {
			whatsapp.SendText(from, "Your order "+o.Reference+" is already recorded. Here is its payment button again.")
			sendPayButton(from, o.Charged(), *link.URL)
		} else if stored := orders.Default.GetOrder(o.Reference); stored != nil && stored.Status == "paid" {
			whatsapp.SendText(from, "That order is already confirmed: "+stored.Reference+". The shop has it and will take it from here.")
		} else {
			whatsapp.SendText(from, "That order, "+o.Reference+", has ended, so there is nothing to pay on it. If you still want the item, tell me and we will start it fresh.")
		}
	} else {
		whatsapp.SendText(from, "Your order "+o.Reference+" is already recorded, and the shop has it. Nothing more is needed from you just now.")
	}
	sessions.put(sessionKey(shopID, from), &Session{Stage: "browsing", MerchantID: shopID, Location: s.Location, History: s.History})
	go SnapshotSessionsNow()
}

// newSaleID mints a fresh sale identity.
func newSaleID() string {
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(raw[:])
}

// saleStockLines lists what a completing sale takes from
// the shelf: every basket line plus the pending item, each
// in its product's own units (one piece for a plain product,
// the taken quantity for a by-measure one, the whole piece
// when no quantity was settled).
func saleStockLines(s *Session) []orders.StockLine {
	lines := make([]orders.StockLine, 0, len(s.Basket)+1)
	add := func(p *catalogue.Product, qty float64) {
		if p == nil {
			return
		}
		units := 1.0
		unit, base := "", 0.0
		if p.Unit != "" && p.BaseQty > 0 {
			units = qty
			if units <= 0 {
				units = p.BaseQty
			}
			// The line remembers the measure it was taken
			// in (the sixth audit), so the give-back can
			// restore in that denomination.
			unit, base = p.Unit, p.BaseQty
		}
		for i := range lines {
			if lines[i].Name == p.Name {
				lines[i].Units += units
				return
			}
		}
		lines = append(lines, orders.StockLine{Name: p.Name, Units: units, Unit: unit, BaseQty: base})
	}
	for _, it := range s.Basket {
		add(it.Product, it.Qty)
	}
	add(s.Product, s.Qty)
	return lines
}

// refuseSaleStock ends a sale the shelf can no longer
// honour: the buyer hears why in plain words, the merchant
// hears the fact, and the pending sale is put down the way
// a cancellation puts it down, minus the cancellation words.
func refuseSaleStock(from string, s *Session, merchant *catalogue.Merchant, why string) {
	shopID := s.MerchantID
	p := s.Product
	view := p
	if s.Qty > 0 {
		view = wholeProductOf(shopID, p)
	}
	cancelFeeQuote(shopID, from)
	cancelInHand(shopID, from)
	whatsapp.SendText(from, why)
	if phone := merchantNotifyPhone(merchant); phone != "" {
		whatsapp.SendText(phone, "A sale to "+from+" could not go ahead: "+why)
	}
	sessions.put(sessionKey(shopID, from), &Session{Stage: "browsing", MerchantID: shopID, LastViewed: view, Location: s.Location, History: s.History})
}

// giveStockLines returns claimed shelf units to the
// catalogue. The claim itself — once only, decided inside
// the order book's lock — is orders.Book.ClaimRestock;
// this is only the giving.
func giveStockLines(merchantID string, lines []orders.StockLine) {
	for _, ln := range lines {
		catalogue.GiveStockMeasured(merchantID, ln.Name, ln.Units, ln.Unit, ln.BaseQty)
	}
}

func completeSale(from string, s *Session, price float64, f fulfilment) {
	p := s.Product
	shopID := s.MerchantID
	merchant := catalogue.GetMerchant(shopID)
	// The in-hand gate stands at the threshold of payment:
	// on the sales where a phantom item would hurt most,
	// the seller confirms the goods are in hand before any
	// order is recorded or any button sent (see inhand.go).
	// Her yes re-enters here with the gate lifted.
	if !s.InHandOK {
		if grounds := inHandGrounds(merchant, s, price); grounds != "" {
			beginInHandWait(from, s, merchant, f, grounds)
			return
		}
	}
	// A completion must never mint the same agreement
	// twice. If this session's sale was already recorded —
	// the shape of a stale snapshot restored over a
	// completion the books already hold — the standing order
	// is resumed instead of a second one being minted (see
	// liveAgreementOrder).
	if s.SaleID == "" {
		s.SaleID = newSaleID()
	}
	if existing := liveAgreementOrder(shopID, from, s); existing != nil {
		resumeAgreementOrder(from, s, merchant, existing)
		return
	}

	// The reference is the order's key in the book and the
	// buyer's receipt: the shop's own code and the shop's own
	// count, so the code alone names whose order it is (EFUA-0007
	// is Efua's seventh order) and no other shop can ever mint
	// the same one. The count is bumped past any key already in
	// the book, so a reference is never reused and an order is
	// never silently overwritten. Behind the serial rides a
	// short unguessable tail (EFUA-0007-K7Q): a reference is
	// quoted back to pull an order up, and a bare serial is
	// guessable by anyone who can count, so the serial alone
	// is never the whole credential. It is minted first now,
	// before any stock moves, because the completion journal
	// below names the sale by it.
	code := merchant.ShopCode
	if code == "" {
		code = strings.ToUpper(shopID)
	}
	serial := orders.Default.CountFor(shopID) + 1
	reference := fmt.Sprintf("%s-%04d-%s", code, serial, refTail())
	for orders.Default.GetOrder(reference) != nil {
		serial++
		reference = fmt.Sprintf("%s-%04d-%s", code, serial, refTail())
	}

	// The stock gate: every line of this sale is taken from
	// the shelf now, at the mint — not at selection time,
	// which may be many turns and other buyers ago. The
	// take is the check: each line's deduction is atomic
	// in the catalogue, so two buyers completing at once
	// cannot both pass a look at the same last unit (the
	// second audit found the old shape — a full check
	// loop, then a take loop that discarded the results —
	// overselling exactly there). A line the shelf can no
	// longer cover refuses the whole sale plainly, and
	// what this completion already took goes back before
	// the refusal is spoken, so a refused sale moves no
	// stock at all. An order that later dies unpaid gives
	// its units back (see ClaimRestock). Until this gate
	// existed stock was never consumed at all: a stock-1
	// one-off item could be sold again and again.
	stockLines := saleStockLines(s)
	// The completion is journalled before the first unit
	// moves: if the process dies between the take and the
	// order reaching the book, the journal is what the
	// next boot reconciles the shelf against, instead of
	// the units vanishing with no record anywhere (the
	// second audit's crash window). See sale_journal.go.
	recordSaleCompletion(sessionKey(shopID, from), s.SaleID, reference, p.ID, shopID, stockLines)
	taken := make([]orders.StockLine, 0, len(stockLines))
	for _, ln := range stockLines {
		if catalogue.TakeStock(shopID, ln.Name, ln.Units) {
			taken = append(taken, ln)
			// The journal learns of each take as it
			// lands, so a crash inside this loop leaves
			// a record of exactly what moved.
			recordJournalTake(reference, ln)
			continue
		}
		// The journal intent settles BEFORE the rollback
		// gives anything back (the fifth audit): while
		// the entry stood unsettled, a crash after a
		// rollback give persisted left boot
		// reconciliation treating the entry as a sale
		// that took stock and never landed — and giving
		// every taken line back a second time. Settled
		// first, a crash can lose a rollback give but
		// can never double one, the direction the
		// codebase already chose for this trade.
		settleSaleIntent(reference)
		for _, tk := range taken {
			catalogue.GiveStockMeasured(shopID, tk.Name, tk.Units, tk.Unit, tk.BaseQty)
		}
		live := catalogue.FindByName(shopID, ln.Name)
		if live == nil {
			refuseSaleStock(from, s, merchant, "I am sorry — the shop no longer has the "+ln.Name+", so this order cannot go ahead. Nothing has been charged.")
			return
		}
		why := "I am sorry — the " + ln.Name + " has just sold out, so this order cannot go ahead. Nothing has been charged."
		if live.Unit != "" && live.BaseQty > 0 {
			why = "I am sorry — there is only " + formatQty(live.AvailableUnits()) + " " + unitWord(live.Unit, live.AvailableUnits()) + " of the " + ln.Name + " left, so this order cannot go ahead. Nothing has been charged."
		}
		refuseSaleStock(from, s, merchant, why)
		return
	}
	// Delivery money has two purses, and the shop's own setting
	// (deliveryPaidToOf) says which one it falls into. When the
	// merchant takes it herself, the fee is a pass-through on top
	// of the agreed item price: the buyer pays it here so the goods
	// can travel, the haggle never negotiated it, and it is charged
	// as part of one total, the way buyers ask for it ("How much
	// in total?"). When the carrier takes it (the default), the
	// buyer pays whoever brings the item, on arrival, and the fee
	// never enters the charged total at all: the order still
	// records the quoted fee and who it goes to, so the books know,
	// but the merchant's money is the item price alone. Either way
	// the fee rides only on a delivery she chose; pickup and
	// meet-ups carry none. And the day book below books the item
	// price only in both purses: revenue and givenAway are item
	// figures, never inflated by delivery money.
	deliveryFee := f.Fee
	paidTo := ""
	chargedFee := deliveryFee
	if f.Mode == "delivery" {
		paidTo = deliveryPaidToOf(merchant)
		if paidTo == "carrier" {
			chargedFee = 0
		}
	}
	// The order's goods are the pending item plus everything
	// parked in the basket, each settled on its own terms;
	// the delivery fee rides once, on the whole order.
	items := orderItemsOf(s, f, price)
	goods := basketGoods(s) + price
	multi := len(items) > 1
	total := goods + chargedFee
	link := checkout.CreatePaymentLink(checkout.LinkRequest{Amount: total, Reference: reference, Subaccount: subaccountFor(merchant)})
	linkAttempts := 1
	if link.URL == nil && checkout.Enabled() {
		// A dead first key must not strand a closed sale.
		// Seev remembers idempotency keys beyond our own
		// books: a reference minted again after a store
		// reset collides with the session it named in a
		// former era, at another amount (a 409), and its
		// old attempt keys are burned the same way, so
		// walking them heals nothing. The retry rides a
		// key that has never existed, and the order points
		// at the session that actually opened, so
		// settlement still finds it.
		if retry := checkout.CreatePaymentLink(checkout.LinkRequest{Amount: total, Reference: reference, IdempotencyKey: checkout.FreshKey(reference), Subaccount: subaccountFor(merchant)}); retry.URL != nil {
			link = retry
			linkAttempts = 2
		}
	}
	status := "link_failed"
	if link.URL != nil {
		status = "pending"
	}
	// The order carries its shop and that shop's payout destination,
	// so settlement can never drift to another merchant.
	payout := ""
	if merchant != nil {
		payout = merchant.PayoutPhone
	}
	orderName := p.Name
	var orderLines []orders.OrderItem
	if multi {
		orderName = itemsLabel(items)
		orderLines = items
	}
	orders.Default.RecordOrder(reference, orders.Order{
		From:           from,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		ProductName:    orderName,
		Items:          orderLines,
		Price:          goods,
		DeliveryFee:    deliveryFee,
		Total:          total,
		ProviderRef:    link.ProviderRef,
		Status:         status,
		MerchantID:     shopID,
		PayoutPhone:    payout,
		Fulfilment:     f.Mode,
		Location:       f.Location,
		Collector:      f.Collector,
		PackageName:    f.PackageName,
		Note:           f.Note,
		DeliveryPhone:  f.Phone,
		Landmark:       f.Landmark,
		Size:           f.Size,
		DeliveryPaidTo: paidTo,
		Quantity:       s.Qty,
		Unit:           s.Unit,
		LinkAttempts:   linkAttempts,
		StockLines:     stockLines,
		SaleID:         s.SaleID,
		DryRun:         link.DryRun,
	})
	// The completion is durable in the books from here; the
	// session snapshot follows the moment this turn lets go
	// of the lock, so a restart restores the buyer's place
	// after the sale, never the moment just before it. The
	// completion journal (written before the stock moved)
	// covers the gap if the process dies first: it rebinds
	// this session's sale identity at restore, so her next
	// answer resumes this order instead of minting a twin.
	go SnapshotSessionsNow()
	merchantPhone := merchantNotifyPhone(merchant)
	// Seev was configured but the session could not be created: say so,
	// to both sides, rather than send a link that does not work. No
	// sale is booked in the day book for a deal the customer cannot pay
	// for.
	if link.URL == nil {
		sessions.put(sessionKey(shopID, from), &Session{Stage: "browsing", MerchantID: shopID, Location: s.Location})
		whatsapp.SendText(from, "We have a deal at "+money.GHS(goods)+" for the "+orderName+", but the payment link is not coming up right now. The shop has been told and will reach out here to complete your order.")
		if merchantPhone != "" {
			whatsapp.SendText(merchantPhone, "Payment link failed for an agreed deal: "+orderName+" at "+money.GHS(goods)+", buyer "+from+". Ref: "+reference+". Please reach out to the buyer to complete it.")
		}
		return
	}
	url := *link.URL
	// The day book books the item prices alone (see the delivery
	// note above): each line at its own agreed price, giveaway
	// measured against its own list, delivery money nowhere in
	// the day's figures.
	var day daybook.State
	bookedLists := make([]float64, 0, len(items))
	if multi {
		for _, it := range s.Basket {
			day = daybook.Default.RecordSale(shopID, daybook.Sale{
				ListPrice:   it.Product.ListPrice,
				AgreedPrice: it.AgreedPrice,
				Allowance:   merchant.DailyGiveawayAllowance,
			})
			bookedLists = append(bookedLists, it.Product.ListPrice)
		}
	}
	day = daybook.Default.RecordSale(shopID, daybook.Sale{
		ListPrice:   p.ListPrice,
		AgreedPrice: price,
		Allowance:   merchant.DailyGiveawayAllowance,
	})
	bookedLists = append(bookedLists, p.ListPrice)
	// The booked list figures are recorded on the order
	// itself (the sixth audit): a later cancellation
	// reverses this booking line by line at these
	// figures, instead of recomputing a single total from
	// the shelf as it stands then — which corrupted the
	// day book's sales count and giveaway pool for
	// multi-item and by-measure orders. bookedLists is
	// aligned with items (basket lines first, the main
	// line last), the order orderItemsOf wrote.
	orders.Default.Update(reference, func(x *orders.Order) {
		x.BookedList = p.ListPrice
		for i := range x.Items {
			if i < len(bookedLists) {
				x.Items[i].ListPrice = bookedLists[i]
			}
		}
	})
	sessions.put(sessionKey(shopID, from), &Session{Stage: "agreed", MerchantID: shopID, Product: p, AgreedPrice: price, PaymentURL: url, Location: s.Location})
	// The completion goes out in the two families. The total
	// is a record: one message on the fixed paper, never split
	// (see records.go). What happens with the goods is talk,
	// in pieces the way a person types it, before it; the
	// package read-backs and the link follow it. The deal line
	// itself was already sent when the price was agreed
	// (beginFulfilment).
	var talk []string
	switch f.Mode {
	case "pickup":
		if f.Collector != "" {
			talk = append(talk, "Noted. I have told the seller that "+f.Collector+" is coming for it.")
		}
	case "meet":
		// Honest wording, no guarantees beyond the sale: the
		// seller has been told the place and it is on the order;
		// whether she comes to it is hers to say.
		talk = append(talk, "I have told the seller you want to meet at "+f.Location+" to collect it. The place is on your order.")
	case "arranged":
		talk = append(talk, "Delivery will be arranged with the seller.")
	}
	sendPieces(from, talk...)
	note := ""
	if merchant != nil {
		note = merchant.DeliveryNote
	}
	whatsapp.SendText(from, totalRecord(items, deliveryFee, chargedFee, f.Mode, f.Location, paidTo, note))
	var pieces []string
	// The trust word, when the seller's record calls for
	// one, goes out before the payment button: a buyer about
	// to pay a stranger is told what the marketplace knows
	// about her, in plain words, while it can still change
	// her mind (see trust.go).
	if merchant != nil {
		switch {
		case flaggedNumber(merchant.OwnerWhatsApp):
			pieces = append(pieces, "Before you pay, you should know: "+merchant.Name+" has strikes on its record here for disputes that were never resolved. If you go ahead, your payment is confirmed by Stall itself the moment it lands, and if the goods do not reach you, tell me and I will open a dispute and freeze the order on Stall's side.")
		case sellerIsNew(merchant.OwnerWhatsApp):
			pieces = append(pieces, "One thing to know: "+merchant.Name+" is new on Stall and has not completed a delivery here yet. Your payment is confirmed by Stall itself the moment it lands, and if the goods do not reach you, tell me and I will open a dispute and freeze the order on Stall's side.")
		}
	}
	// The package details she gave ride with the order, and she
	// hears the name read back so a wrong name is caught here,
	// not at the rider's door.
	if f.Size != "" && !multi {
		pieces = append(pieces, "Size: "+f.Size+".")
	}
	if f.PackageName != "" {
		pieces = append(pieces, "The package will carry the name "+f.PackageName+".")
	}
	if f.Note != "" {
		pieces = append(pieces, "Your message for the seller is on the order.")
	}
	// Her receipt: the reference goes to her as its own piece, so
	// it sits in her chat the way a shop hands over a receipt
	// number. It is recorded against this order in the book, and
	// she can quote it any time to have the order pulled up.
	pieces = append(pieces, "Your order reference is "+reference+". Keep it: send it to me any time and I will pull up your order.")
	sendPieces(from, pieces...)
	// The link itself arrives as a tap-to-pay button with
	// the total on its face, not as a raw address in the chat.
	sendPayButton(from, total, url)
	if merchantPhone != "" {
		// The notice is the seller's receipt for the sale, and
		// it is a record: one message on the fixed paper, the
		// same lines in the same order every time (see
		// records.go), filled from the order's own facts.
		if o := orders.Default.GetOrder(reference); o != nil {
			whatsapp.SendText(merchantPhone, saleNoticeRecord(o, merchant, day))
		}
	}
}
