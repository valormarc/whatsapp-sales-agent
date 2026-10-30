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
	var totalList, totalFloor, dearest float64
	allFixed := true
	for _, p := range hand {
		totalList += p.ListPrice
		totalFloor += effFloor(p)
		if p.ListPrice > dearest {
			dearest = p.ListPrice
		}
		if !p.Fixed {
			allFixed = false
		}
	}
	if offer <= dearest && !totalFrameRe.MatchString(strings.ToLower(msg)) {
		return false // this figure can belong to one item, and she
		// did not declare it for the lot; it is read there
	}
	synth := &catalogue.Product{
		Name:       handLabel(hand),
		ListPrice:  totalList,
		FloorPrice: totalFloor,
		Fixed:      allFixed,
	}
	ns := openHaggle(shopID, merchant, synth)
	ns.Location, ns.History, ns.Basket = s.Location, s.History, s.Basket
	ns.ShelfPicks = s.ShelfPicks
	ns.LastViewed = s.LastViewed
	ns.TotalHand = hand
	sessions[sessionKey(shopID, from)] = ns
	whatsapp.SendText(from, "For all of them together, then. "+handLabel(hand)+" come to "+money.GHS(totalList)+" in all.")
	processOffer(from, ns, merchant, offer, msg)
	return true
}

// apportionTotal shares an agreed total back across the
// hand: every item gives in proportion to its headroom
// (the distance between its list and its last price), no
// item settles below its last price, and the shares sum
// to the total exactly, with any rounding residue laid
// on the item that can carry it.
func apportionTotal(hand []catalogue.Product, total float64) []float64 {
	n := len(hand)
	prices := make([]float64, n)
	heads := make([]float64, n)
	var listSum, headSum float64
	for i, p := range hand {
		listSum += p.ListPrice
		heads[i] = p.ListPrice - effFloor(p)
		headSum += heads[i]
	}
	discount := listSum - total
	for i, p := range hand {
		share := 0.0
		if headSum > 0 {
			share = discount * heads[i] / headSum
		}
		prices[i] = money.Round(p.ListPrice - share)
		if prices[i] < effFloor(p) {
			prices[i] = effFloor(p)
		}
	}
	var sum float64
	for _, v := range prices {
		sum += v
	}
	if residue := money.Round(total - sum); residue != 0 {
		order := make([]int, n)
		for i := range order {
			order[i] = i
		}
		sort.Slice(order, func(a, b int) bool { return heads[order[a]] > heads[order[b]] })
		for _, i := range order {
			if nv := money.Round(prices[i] + residue); nv >= effFloor(hand[i]) && nv <= hand[i].ListPrice {
				prices[i] = nv
				break
			}
		}
	}
	return prices
}
