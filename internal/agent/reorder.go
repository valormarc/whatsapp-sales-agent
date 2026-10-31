package agent

// Buy again: the fastest sale there is, because everything
// about it is already known. A buyer who quotes a completed
// order, names its reference, or simply asks for the same
// thing again is not starting a purchase; she is repeating
// one. The agent reads that intent the way a person would,
// rebuilds the order from the book against today's shelf
// (same items, same quantities and sizes, same way of
// reaching her, same name on the package), names any price
// that has moved since, and holds the whole thing up once:
// same again? Her yes is the only question the lane ever
// asks, and the payment button follows it.
//
// The lane reaches only her own orders. A reference names
// an order, but the recap recites that order's delivery
// place, collector and package name, so presenting a
// reference is not enough: the number speaking must be the
// number the order was placed from. A reference belonging
// to another number gets a neutral answer and no details,
// and nothing about her binding moves. (This reverses the
// lane's first design, where the reference alone was the
// credential and any number could present it: references
// are serial, and a guessable number is not a credential.)

import (
	"regexp"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

var reorderRe = regexp.MustCompile(`\b(buy|order|get|have|take|want) (it|this|that|them|the same|same) .{0,20}\bagain\b|\bre ?order\b|\brepurchase\b|\bsame (order|thing|one) again\b|\bthe same (order|thing) again\b|\bbuy (the )?same\b|\bsame again\b|\brepeat (my |the )?(last )?order\b|\bbuy am again\b`)

var sameDetailsRe = regexp.MustCompile(`^use (the )?same\b|\bsame details\b|\bsame as (last time|before)\b|\bthe same details\b`)

// reorderSource finds the completed order a reorder ask is
// about: the one she quotes in this chat, the one whose
// reference her words carry (in this shop, then in any of
// her shops), or her own newest paid order here. Every road
// lands on an order placed from her own number.
func reorderSource(shopID, from, msg string) *orders.Order {
	if qo := quotedOrderFor(shopID, from); qo != nil {
		return qo
	}
	if o := orderFromMessage(shopID, msg); o != nil && o.From == from {
		return o
	}
	for _, o := range orders.Default.All() {
		if o.Reference != "" && o.From == from && strings.Contains(strings.ToUpper(msg), o.Reference) {
			oo := o
			return &oo
		}
	}
	return newestPaidOrderFor(shopID, from)
}

// foreignRefOrder returns the order a message's reference
// names when that order belongs to a different number, so
// the lane can answer for it plainly instead of falling
// through to handling that might recite its details.
func foreignRefOrder(from, msg string) *orders.Order {
	m := orderRefRe.FindStringSubmatch(strings.ToUpper(msg))
	if m == nil {
		return nil
	}
	o := orders.Default.GetOrder(m[1])
	if o != nil && o.From != from {
		return o
	}
	return nil
}

// reorderLine is one rebuilt line of the repeat order: the
// product as the shelf holds it today, the price it carries
// today, the quantity and size the old order recorded, and
// what she paid for it then, so a moved price can be named.
type reorderLine struct {
	product  *catalogue.Product
	scaled   *catalogue.Product
	price    float64
	wasPrice float64
	qty      float64
	unit     string
	size     string
}

// startReorder carries a reorder ask as far as the facts
// allow and reports whether it took the turn. It runs from
// the thinking board and from the word net alike; both are
// only naming the act, and everything after the naming is
// this code, from the books.
// bareItemName strips a leading quantity label ("3 yards
// of ") from a stored order line's display name, leaving
// the product's own name. The recorded quantity and unit,
// when the order carries them, must agree with the label
// for the strip to count.
func bareItemName(name string, qty float64, unit string) string {
	i := strings.Index(name, " of ")
	if i <= 0 {
		return name
	}
	q, ok := parseQuantity(name[:i])
	if !ok || q.Qty <= 0 {
		return name
	}
	if qty > 0 && q.Qty != qty {
		return name
	}
	if unit != "" {
		fold := func(u string) string {
			return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(u)), "s")
		}
		if fold(q.Unit) != fold(unit) {
			return name
		}
	}
	return strings.TrimSpace(name[i+len(" of "):])
}

func startReorder(from string, s *Session, merchant *catalogue.Merchant, shopID, msg string) bool {
	if foreignRefOrder(from, msg) != nil {
		whatsapp.SendText(from, "That order was placed from a different number, so I cannot pull it up from here. If it is yours, talk to me from the number you ordered with and I will find it at once.")
		return true
	}
	source := reorderSource(shopID, from, msg)
	if source == nil {
		return false
	}
	if source.Status == "cancelled" {
		whatsapp.SendText(from, "That order, "+source.Reference+", was cancelled, so there is nothing standing to repeat. Tell me what you would like instead and I will sort it out.")
		return true
	}
	if source.Status != "paid" {
		whatsapp.SendText(from, orderReadback(merchant, source))
		return true
	}
	targetShop := source.MerchantID
	if targetShop == "" {
		targetShop = shopID
	}
	targetMerchant := catalogue.GetMerchant(targetShop)
	if targetMerchant == nil {
		return false
	}
	if targetShop != shopID {
		// The order she presented belongs to another shop on
		// this number: she is that shop's customer for this
		// order, and the repeat is built there, on a fresh
		// session that cannot leak this shop's state into it.
		bindings.put(from, targetShop)
		s = &Session{Stage: "browsing", MerchantID: targetShop, Location: s.Location, History: s.History}
		merchant = targetMerchant
		shopID = targetShop
	}

	// Rebuild the lines from the book against today's shelf.
	type rawLine struct {
		name string
		was  float64
		qty  float64
		unit string
		size string
	}
	var raw []rawLine
	if len(source.Items) > 0 {
		for _, it := range source.Items {
			raw = append(raw, rawLine{name: it.Name, was: it.Price, qty: it.Quantity, unit: it.Unit, size: it.Size})
		}
	} else {
		raw = append(raw, rawLine{name: source.ProductName, was: source.Price, qty: source.Quantity, unit: source.Unit, size: source.Size})
	}
	var lines []reorderLine
	var missing []string
	for _, r := range raw {
		p := catalogue.FindByName(shopID, r.name)
		if p == nil {
			// A by-measure line is stored under its
			// quantity label ("3 yards of Ankara
			// Fabric"), which is a display name, not a
			// catalogue key (the sixth audit): strip the
			// label and resolve the product behind it
			// before the line is called missing.
			if bare := bareItemName(r.name, r.qty, r.unit); bare != r.name {
				p = catalogue.FindByName(shopID, bare)
			}
		}
		if p == nil {
			missing = append(missing, r.name)
			continue
		}
		if p.AvailableUnits() <= 0 || (r.qty > 0 && p.ByMeasure() && p.AvailableUnits() < r.qty) {
			// Resolved but unavailable is missing in every
			// way that matters to her (the seventh audit):
			// the line joins the missing list and is named
			// as no longer available, exactly like a
			// delisted line, instead of being rebuilt into
			// a fulfilment run for goods that are gone.
			missing = append(missing, r.name)
			continue
		}
		line := reorderLine{product: p, wasPrice: r.was, qty: r.qty, unit: r.unit, size: r.size}
		if r.qty > 0 && r.unit != "" && p.ByMeasure() {
			scaled := scaledCopy(p, r.qty)
			line.scaled = &scaled
			line.price = scaled.ListPrice
		} else {
			line.price = p.ListPrice
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		whatsapp.SendText(from, merchant.Name+" does not have "+strings.Join(missing, ", ")+" on the shelf any more, so that order cannot be repeated as it stands. Here is what they have now:\n"+productListText(shopID))
		return true
	}

	// Assemble the pending sale exactly as a settled haggle
	// leaves it: the first line is the sale's own product,
	// the rest ride in the basket.
	key := sessionKey(shopID, from)
	first := lines[0]
	s.Stage = "fulfilment"
	// A reorder is a sale like any other and carries a
	// sale identity from its first moment: the second
	// audit found this lane minting none, so a restored
	// reorder session could never be matched to the order
	// it had already completed.
	s.SaleID = newSaleID()
	s.MerchantID = shopID
	s.PendingAdd = nil
	s.Basket = nil
	s.ShelfPicks = nil
	s.ShelfSettling = false
	if first.scaled != nil {
		s.Product = first.scaled
		s.Qty, s.Unit = first.qty, first.unit
	} else {
		s.Product = first.product
		s.Qty, s.Unit = 0, ""
	}
	s.AgreedPrice = first.price
	for _, line := range lines[1:] {
		item := BasketItem{Product: line.product, AgreedPrice: line.price, Qty: line.qty, Unit: line.unit, Size: line.size}
		if line.scaled != nil {
			item.Product = line.scaled
		}
		s.Basket = append(s.Basket, item)
	}

	f := fulfilment{
		Mode:        source.Fulfilment,
		Location:    source.Location,
		Collector:   source.Collector,
		PackageName: source.PackageName,
		Note:        source.Note,
		Size:        first.size,
	}
	if f.Mode == "" {
		f.Mode = "arranged"
	}
	var opening []string
	opening = append(opening, "Same again: "+source.ProductName+" from "+merchant.Name+", order "+source.Reference+".")
	for _, line := range lines {
		if line.price != line.wasPrice {
			opening = append(opening, "One change since then: "+line.product.Name+" was "+money.GHS(line.wasPrice)+", and it is "+money.GHS(line.price)+" now.")
		}
	}
	if len(missing) > 0 {
		opening = append(opening, "The shop no longer stocks "+strings.Join(missing, ", ")+", so those are not in this repeat.")
	}
	sendPieces(from, opening...)

	if f.Mode == "delivery" && deliveryStyleOf(merchant) == "quote" {
		// Her fee was quoted for that order, in that day's
		// words; a repeat asks for a fresh one, with the
		// goods and the name already settled around it.
		s.PendingFulfil = f
		s.FeePark = &f
		s.PendingFlow, s.FlowKind = "", "" // the chat adoption spends the form token (the sixth audit)
		sessions.put(key, s)
		beginFeeQuote(from, s, merchant, f.Location)
		return true
	}
	if f.Mode == "delivery" {
		f.Fee = deliveryFeeFor(merchant, f.Location)
	}
	s.PendingFulfil = f
	s.FulfilStep = "confirm"
	if f.PackageName != "" {
		packageNames.put(key, f.PackageName)
	}
	sessions.put(key, s)
	sendOrderRecap(from, s, f)
	return true
}

// adoptSameDetails answers "use the same details as last
// time" inside the details steps: everything the book
// already holds about how her goods reached her and what
// name went on the package is applied at once, the owed
// sizes are settled from her last orders where they can
// be, and the sale goes straight to its recap. It reports
// whether it took the turn; a first-time buyer has nothing
// to adopt, and the ordinary steps keep her.
func adoptSameDetails(from string, s *Session, merchant *catalogue.Merchant) bool {
	if s.FulfilStep == "confirm" || s.FulfilStep == "" {
		return false
	}
	remembered, ok := rememberedFulfilment(s.MerchantID, from, merchant)
	if !ok {
		return false
	}
	key := sessionKey(s.MerchantID, from)
	f := s.PendingFulfil
	// At the choice steps nothing is settled yet, so the
	// remembered way is adopted whole. Inside the details
	// the way is already settled for this sale, in her own
	// words this time, and it stands: what "same details"
	// supplies there is everything still open, the name
	// and the sizes, from the book.
	if f.Mode == "" {
		f.Mode, f.Location, f.Collector = remembered.Mode, remembered.Location, remembered.Collector
		if f.Mode == "delivery" {
			f.Fee = remembered.Fee
		}
	}
	if name := rememberedPackageName(s.MerchantID, from); name != "" {
		f.PackageName = name
	}
	// Settle any sizes still owed from her last orders; a
	// size the book cannot answer is still asked, alone.
	var still []int
	for _, idx := range s.SizeQueue {
		var p *catalogue.Product
		var set func(string)
		if idx >= 0 && idx < len(s.Basket) {
			p = s.Basket[idx].Product
			set = func(sz string) { s.Basket[idx].Size = sz }
		} else if s.Product != nil {
			p = s.Product
			set = func(sz string) { f.Size = sz }
		}
		if p != nil {
			if sz := lastSizeFor(s.MerchantID, from, p.Name); sz != "" && matchListedSize(p, sz) != "" {
				set(sz)
				continue
			}
		}
		still = append(still, idx)
	}
	s.SizeQueue = still
	s.PendingFulfil = f
	if len(still) > 0 {
		s.FulfilStep = "size"
		s.PendingFlow, s.FlowKind = "", "" // the chat adoption spends the form token (the sixth audit)
		sessions.put(key, s)
		whatsapp.SendText(from, "Using your details from last time. One thing the book does not hold: "+sizeQuestionText(s))
		return true
	}
	if f.Mode == "delivery" && deliveryStyleOf(merchant) == "quote" {
		// A quoted fee is never remembered into a new sale:
		// the details ride parked across the fresh quote,
		// and the recap follows the fee.
		s.FeePark = &f
		sessions.put(key, s)
		beginFeeQuote(from, s, merchant, f.Location)
		return true
	}
	s.FulfilStep = "confirm"
	s.PendingFlow, s.FlowKind = "", "" // the chat adoption spends the form token (the sixth audit)
	sessions.put(key, s)
	sendOrderRecap(from, s, f)
	return true
}
