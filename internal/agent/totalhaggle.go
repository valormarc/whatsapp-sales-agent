package agent

// An offer on the whole hand. A buyer holding several
// picks talks the way she talks at a stall: "I will give
// you 509" — for the lot, not for whichever item she
// touched last. Read as an offer on one item it closes
// that item at its own price and strands the rest, which
// is a misreading of the plainest sentence in a market.
// So a bare offer that cannot belong to any single item
// in her hand — more than the dearest pick costs, while
// she holds at least two — opens the haggle on the total
// instead: the picks wear one coat (their list prices
// added, their last prices added, the day's posture the
// same as any haggle), the talk is over the one figure,
// and an agreement shares the discount back across the
// items in proportion to what each of them can give,
// never below any item's own last price, the shares
// summing to the agreed total exactly. The order that
// completes is the ordinary basket order; only its road
// in was the total.
//
// An offer that names an item is that item's, as always,
// and an offer no bigger than the dearest pick can belong
// to one item, so those roads are untouched — unless she
// declares the scope herself: "for all", "for the lot",
// "everything together". The declared scope is read at
// any figure, the way a figure framed as her budget is
// never an offer at all; only a bare figure with no scope
// declared keeps the conservative rule.

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// donePickingRe is the typed shape of the shelf's Done
// row: a control word, read the way the buttons' yes and
// no are read, never as chat. While she holds picks the
// word can only mean she has finished picking.
var donePickingRe = regexp.MustCompile(`^(i am done|i'm done|done|finished|that'?s all|that is all)[.!]?$`)

// totalFrameRe is the wording that frames a figure as
// being for the whole hand: "for all", "for the lot",
// "everything together". A figure's scope is part of its
// shape, the same way a figure framed as her budget is
// never an offer (see haggle.go): when she declares the
// scope herself, the declaration is read at any figure,
// and only a bare figure with no scope declared keeps
// the conservative rule below.
var totalFrameRe = regexp.MustCompile(`for all\b|for the lot\b|the whole lot\b|for them all\b|for everything\b|everything together\b|all together\b`)

// handPicks resolves her pick ids to the products
// themselves, in the order she picked them. A pick the
// shelf no longer holds is simply not in the hand — and
// neither is one the shop can no longer supply: stock is
// re-read here, at the moment the hand is acted on, the
// same protection the settling walk gives a pick whose
// stock has gone since it was chosen.
func handPicks(s *Session, shopID string) []catalogue.Product {
	var hand []catalogue.Product
	for _, id := range s.ShelfPicks {
		for _, p := range catalogue.ListProducts(shopID) {
			if p.ID == id && p.Stock > 0 {
				hand = append(hand, p)
				break
			}
		}
	}
	return hand
}

// effFloor is the lowest an item may settle at inside a
// shared discount: its own last price, or — for a fixed
// price item, whose price does not move at all — its
// list price, so a fixed item's share of any discount is
// always nothing.
func effFloor(p catalogue.Product) float64 {
	if p.Fixed {
		return p.ListPrice
	}
	return p.FloorPrice
}

// handLabel joins the hand's names the way an order names
// its lines: "Rack, Dress and Bag".
func handLabel(hand []catalogue.Product) string {
	names := make([]string, 0, len(hand))
	for _, p := range hand {
		names = append(names, p.Name)
	}
	if len(names) <= 2 {
		return strings.Join(names, " and ")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// openTotalHaggle takes a buyer's offer as an offer on
// her whole hand when the figure leaves no other reading,
// or when she declares that scope herself, and reports
// whether it did. The haggle that follows is the ordinary
// one, over a product built from the hand; the hand itself
// rides on the session so the agreement can be shared back
// across the real items.
func openTotalHaggle(from string, s *Session, merchant *catalogue.Merchant, offer float64, msg string) bool {
	shopID := s.MerchantID
	if catalogue.FindProduct(shopID, strings.ToLower(msg)) != nil {
		return false // she named an item: the offer is that item's
	}
	hand := handPicks(s, shopID)
	if len(hand) < 2 {
		return false
	}
	var dearest float64
	for _, p := range hand {
		if p.ListPrice > dearest {
			dearest = p.ListPrice
		}
	}
	if offer <= dearest && !totalFrameRe.MatchString(strings.ToLower(msg)) {
		return false // this figure can belong to one item, and she
		// did not declare it for the lot; it is read there
	}
	startTotalHaggle(from, s, merchant, hand, offer, msg)
	return true
}

// startTotalHaggle opens the ordinary haggle over a
// hand wearing one coat and puts the buyer's figure to
// it. The hand rides on the new session so the agreement
// can be shared back across the real items.
func startTotalHaggle(from string, s *Session, merchant *catalogue.Merchant, hand []catalogue.Product, offer float64, msg string) {
	shopID := s.MerchantID
	var totalList, totalFloor float64
	allFixed := true
	for _, p := range hand {
		totalList += p.ListPrice
		totalFloor += effFloor(p)
		if !p.Fixed {
			allFixed = false
		}
	}
	synth := &catalogue.Product{
		Name:       handLabel(hand),
		ListPrice:  totalList,
		FloorPrice: totalFloor,
		Fixed:      allFixed,
	}
	ns := openHaggle(shopID, merchant, synth)
	ns.Location, ns.History, ns.Basket = s.Location, s.History, s.Basket
	ns.ShelfPicks = handIDs(hand)
	ns.LastViewed = s.LastViewed
	ns.TotalHand = hand
	sessions.put(sessionKey(shopID, from), ns)
	whatsapp.SendText(from, "For all of them together, then. "+handLabel(hand)+" come to "+money.GHS(totalList)+" in all.")
	processOffer(from, ns, merchant, offer, msg)
}

// handIDs is a hand as pick ids, in the order she
// picked them.
func handIDs(hand []catalogue.Product) []string {
	ids := make([]string, 0, len(hand))
	for _, p := range hand {
		ids = append(ids, p.ID)
	}
	return ids
}

// apportionTotal shares an agreed total back across the
// hand: every item gives in proportion to its headroom
// (the distance between its list and its last price), no
// item settles below its last price, and the shares sum
// to the total exactly, with any rounding residue laid
// on the item that can carry it.
//
// The arithmetic is integer pesewas throughout. The
// second audit found the earlier float shape rounding
// every share, room and residue to WHOLE cedis while the
// floor clamp used the exact fractional floor, so a
// pesewa-priced hand (list 10.50, floor 10.20, twice,
// total 20.50) apportioned to 20.40 — ten pesewas the
// buyer was charged never reached a share — and a share
// could round above its own list price. In pesewas the
// sum is exact by construction.
func apportionTotal(hand []catalogue.Product, total float64) []float64 {
	n := len(hand)
	toP := func(v float64) int64 { return int64(math.Round(v * 100)) }
	listP := make([]int64, n)
	floorP := make([]int64, n)
	headP := make([]int64, n)
	var listSum, headSum int64
	for i, p := range hand {
		listP[i] = toP(p.ListPrice)
		floorP[i] = toP(effFloor(p))
		headP[i] = listP[i] - floorP[i]
		if headP[i] < 0 {
			headP[i] = 0
		}
		listSum += listP[i]
		headSum += headP[i]
	}
	totalP := toP(total)
	discount := listSum - totalP
	prices := make([]int64, n)
	for i := range hand {
		var share int64
		if headSum > 0 && discount > 0 {
			share = discount * headP[i] / headSum
		}
		price := listP[i] - share
		if price < floorP[i] {
			price = floorP[i]
		}
		if price > listP[i] {
			price = listP[i]
		}
		prices[i] = price
	}
	var sum int64
	for _, v := range prices {
		sum += v
	}
	if residue := totalP - sum; residue != 0 {
		order := make([]int, n)
		for i := range order {
			order[i] = i
		}
		sort.Slice(order, func(a, b int) bool { return headP[order[a]] > headP[order[b]] })
		// The residue belongs to the hand, not to one item:
		// spread it across every item that still has room,
		// in headroom order, until it is placed. An earlier
		// shape offered the whole residue to a single item
		// and silently dropped what no one item could
		// absorb, so the shares summed to a few cedis more
		// or less than the total the buyer agreed (the
		// 2026-10-30 audit found 34 violating totals in an
		// ordinary sweep).
		for residue != 0 {
			placed := false
			for _, i := range order {
				if residue == 0 {
					break
				}
				var room int64
				if residue > 0 {
					room = listP[i] - prices[i]
				} else {
					room = prices[i] - floorP[i]
				}
				if room <= 0 {
					continue
				}
				step := residue
				if step > room {
					step = room
				}
				if step < -room {
					step = -room
				}
				prices[i] += step
				residue -= step
				placed = true
			}
			if !placed {
				break
			}
		}
	}
	out := make([]float64, n)
	for i := range prices {
		out[i] = float64(prices[i]) / 100
	}
	return out
}

// unsettledHand is the part of her hand still on the
// table while the settling runs: the pick in front of
// her first, then the picks still waiting, each re-read
// from the shelf the way handPicks reads them, so a pick
// the shop can no longer supply is not in the hand.
func unsettledHand(s *Session, shopID string) []catalogue.Product {
	var hand []catalogue.Product
	seen := map[string]bool{}
	if s.ShelfSettling && s.Product != nil {
		for _, p := range catalogue.ListProducts(shopID) {
			if p.ID == s.Product.ID && p.Stock > 0 {
				hand = append(hand, p)
				seen[p.ID] = true
				break
			}
		}
	}
	for _, p := range handPicks(s, shopID) {
		if !seen[p.ID] {
			hand = append(hand, p)
			seen[p.ID] = true
		}
	}
	return hand
}

// serveSettlingHandOffer reads an offer made while the
// one-by-one settling is under way. An offer shaped for
// the whole hand must never close the item in front of
// her at its own price: that misreading dealt a buyer
// both her items at their full prices when she offered
// 330 for a 350 hand, her figure never heard at all.
// When nothing has settled yet the hand is still whole,
// and the total haggle opens over it exactly as it would
// have before Done. When some items already stand at
// agreed prices, no road guesses what the figure covers:
// one question asks, and the figure waits on the session
// (see servePendingHandOffer).
func serveSettlingHandOffer(from string, s *Session, merchant *catalogue.Merchant, offer float64, msg string) bool {
	if !s.ShelfSettling || len(s.TotalHand) > 0 {
		return false
	}
	shopID := s.MerchantID
	lower := strings.ToLower(msg)
	if catalogue.FindProduct(shopID, lower) != nil {
		return false // she named an item: the offer is that item's
	}
	hand := unsettledHand(s, shopID)
	if len(hand) == 0 {
		return false
	}
	if len(s.Basket) == 0 && len(hand) < 2 {
		return false // nothing settled and only the one item
		// stands: the offer is that item's, as always
	}
	var dearest float64
	for _, p := range hand {
		if p.ListPrice > dearest {
			dearest = p.ListPrice
		}
	}
	if offer <= dearest && !totalFrameRe.MatchString(lower) {
		return false // this figure can belong to the item in front of her
	}
	if len(s.Basket) == 0 {
		// Nothing settled yet: the hand is whole. The pick
		// in front of her goes back with the others and the
		// total haggle opens over all of them.
		s.ShelfPicks = handIDs(hand)
		s.ShelfSettling = false
		startTotalHaggle(from, s, merchant, hand, offer, msg)
		return true
	}
	// Some items already stand at agreed prices. Ask the
	// one question and hold the figure meanwhile.
	s.PendingHandOffer = offer
	sessions.put(sessionKey(shopID, from), s)
	settled := basketGoods(s)
	parked := make([]catalogue.Product, 0, len(s.Basket))
	for _, b := range s.Basket {
		if b.Product != nil {
			parked = append(parked, *b.Product)
		}
	}
	line := "The " + handLabel(parked) + " is already settled at " + money.GHS(settled) + "."
	if len(parked) > 1 {
		line = "The " + handLabel(parked) + " are already settled at " + money.GHS(settled) + " in all."
	}
	whatsapp.SendText(from, line+" Is the "+money.GHS(offer)+" for everything, those included, or for the rest of them?")
	return true
}

// handAllRe and handRestRe are the two shapes of her
// answer to the scope question: the figure covers
// everything, settled items included, or it covers only
// what is still on the table.
var handAllRe = regexp.MustCompile(`^(yes|all|everything|all of them|for all|for everything|the lot)[.!]?$`)
var handRestRe = regexp.MustCompile(`\b(the rest|the remaining|remaining ones|the others)\b`)

// servePendingHandOffer reads her answer to the scope
// question serveSettlingHandOffer asked. A fresh figure
// is not an answer but a new offer: the waiting figure
// is dropped and the new words take the ordinary roads.
// Words that declare neither scope drop it too, and are
// read fresh. Only a declared scope spends the figure:
// "everything" reads it as covering the settled items
// too, so what remains for the hand still on the table
// is the figure less what already stands; "the rest"
// reads the whole figure on that hand. Either way the
// read goes through the ordinary haggle roads, so every
// price in it is verified the same way as any offer.
func servePendingHandOffer(from string, s *Session, merchant *catalogue.Merchant, msg string) bool {
	if s.PendingHandOffer <= 0 {
		return false
	}
	shopID := s.MerchantID
	lower := strings.ToLower(msg)
	offer := s.PendingHandOffer
	if parseOffer(lower) != nil {
		s.PendingHandOffer = 0
		sessions.put(sessionKey(shopID, from), s)
		return false
	}
	everything := totalFrameRe.MatchString(lower) || handAllRe.MatchString(strings.TrimSpace(lower))
	rest := handRestRe.MatchString(lower)
	if !everything && !rest {
		s.PendingHandOffer = 0
		sessions.put(sessionKey(shopID, from), s)
		return false
	}
	s.PendingHandOffer = 0
	settled := basketGoods(s)
	target := offer
	if everything {
		target = offer - settled
	}
	hand := unsettledHand(s, shopID)
	if len(hand) == 0 {
		sessions.put(sessionKey(shopID, from), s)
		whatsapp.SendText(from, "Everything you picked is already settled, at "+money.GHS(settled)+" in all.")
		return true
	}
	var handTotal float64
	for _, p := range hand {
		handTotal += p.ListPrice
	}
	if target <= 0 {
		sessions.put(sessionKey(shopID, from), s)
		whatsapp.SendText(from, "The ones already settled come to "+money.GHS(settled)+" in all, so "+money.GHS(offer)+" does not cover everything. The rest comes to "+money.GHS(handTotal)+" in all. Settle the rest as they are, or make me an offer on the rest.")
		return true
	}
	if len(hand) == 1 {
		// One item still stands: the figure, read on her
		// declared scope, is her offer on it, and the
		// ordinary haggle hears it — including its rule
		// that an offer above the price closes at the
		// price, never above it.
		p := hand[0]
		loc, hist, basket := s.Location, s.History, s.Basket
		ns := openHaggle(shopID, merchant, &p)
		ns.Location, ns.History, ns.Basket = loc, hist, basket
		ns.ShelfSettling = true
		sessions.put(sessionKey(shopID, from), ns)
		processOffer(from, ns, merchant, target, msg)
		return true
	}
	s.ShelfPicks = handIDs(hand)
	s.ShelfSettling = false
	startTotalHaggle(from, s, merchant, hand, target, msg)
	return true
}
