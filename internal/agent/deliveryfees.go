package agent

// How a shop's delivery fee is known, and what happens
// while it is being found out.
//
// A shop sets its fees one of two ways, and the choice is
// hers. "set": the fees are given up front — one standard
// fee, plus her own fees for named areas where a place costs
// different (see catalogue.AreaFee). The buyer who names her
// place is charged that place's fee, shown inside her full
// total before she pays, and locked when she confirms.
// "quote": the seller names the fee for each delivery order
// as it comes. The buyer's sale pauses, the seller is asked
// in her own chat, and her answer carries the sale forward
// with the fee inside the total. If she does not answer in
// time, the buyer is never stranded: she is offered pickup,
// meet-up, or waiting, and no payment is ever taken before
// she has seen the full total.
//
// Who takes the delivery money is a separate decision (see
// DeliveryPaidTo) and this file does not touch it: a quoted
// or area fee enters the buyer's total the same way the
// standard fee always has, into the merchant's purse when
// she takes delivery money herself, and as the amount she
// hands the carrier on arrival when she does not.

import (
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// feeQuoteTimeout is how long a buyer waits for a seller's
// per-order fee before she is offered the other ways. The
// watch ticks every five minutes, so the offer lands within
// a few minutes of this.
const feeQuoteTimeout = 10 * time.Minute

// feeQuote is one open request for a seller's per-order
// delivery fee: which shop, which buyer, what she is
// buying, and the place it must reach.
type feeQuote struct {
	ShopID    string  `json:"shopId"`
	Buyer     string  `json:"buyer"`
	Item      string  `json:"item"`
	Price     float64 `json:"price"`
	Place     string  `json:"place"`
	CreatedAt string  `json:"createdAt"`
	Status    string  `json:"status"` // open, answered, declined, expired, cancelled
	Fee       float64 `json:"fee,omitempty"`
}

var feeQuotesMu sync.Mutex

func loadFeeQuotes() []feeQuote {
	data, err := os.ReadFile(datadir.Path("feequotes.json"))
	if err != nil {
		return nil
	}
	var out []feeQuote
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func saveFeeQuotes(all []feeQuote) {
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(datadir.Path("feequotes.json"), data, 0o644)
}

// openFeeQuote records a fresh request for this buyer in
// this shop, superseding any open one she already has there.
func openFeeQuote(q feeQuote) {
	feeQuotesMu.Lock()
	defer feeQuotesMu.Unlock()
	all := loadFeeQuotes()
	for i := range all {
		if all[i].ShopID == q.ShopID && all[i].Buyer == q.Buyer && all[i].Status == "open" {
			all[i].Status = "cancelled"
		}
	}
	q.Status = "open"
	q.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	all = append(all, q)
	saveFeeQuotes(all)
}

// oldestOpenFeeQuote is the shop's longest-waiting open
// request, the one a seller's bare fee answer belongs to.
func oldestOpenFeeQuote(shopID string) *feeQuote {
	feeQuotesMu.Lock()
	defer feeQuotesMu.Unlock()
	for _, q := range loadFeeQuotes() {
		if q.ShopID == shopID && q.Status == "open" {
			qq := q
			return &qq
		}
	}
	return nil
}

// settleFeeQuote closes the shop's open request for one
// buyer with the given outcome.
func settleFeeQuote(shopID, buyer, status string, fee float64) {
	feeQuotesMu.Lock()
	defer feeQuotesMu.Unlock()
	all := loadFeeQuotes()
	for i := range all {
		if all[i].ShopID == shopID && all[i].Buyer == buyer && all[i].Status == "open" {
			all[i].Status = status
			all[i].Fee = fee
		}
	}
	saveFeeQuotes(all)
}

// cancelFeeQuote closes a buyer's open request because her
// sale has moved on by another road (a different way of
// getting the goods, or a cancellation).
func cancelFeeQuote(shopID, buyer string) {
	settleFeeQuote(shopID, buyer, "cancelled", 0)
}

// parseAreaFees reads the seller's area fees from one
// message: "Osu 30" or "Osu 30, Kasoa 45, East Legon 50".
// Each part is a place name and its fee; the fee is the
// part's last figure, and everything before it is the name.
// A part with no figure or no name is skipped, so one bad
// part never sinks the rest.
func parseAreaFees(text string) []catalogue.AreaFee {
	var out []catalogue.AreaFee
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		locs := figureTokenRe.FindAllStringIndex(part, -1)
		if len(locs) == 0 {
			continue
		}
		last := locs[len(locs)-1]
		fee, err := strconv.ParseFloat(part[last[0]:last[1]], 64)
		if err != nil || fee <= 0 {
			continue
		}
		name := strings.TrimSpace(part[:last[0]])
		name = strings.Trim(name, " -:")
		if name == "" {
			continue
		}
		out = append(out, catalogue.AreaFee{Area: name, Fee: fee})
	}
	return out
}

// deliveryStyleOf is how the shop's fees are known: "quote"
// only when she has chosen it; every other shop is "set".
func deliveryStyleOf(merchant *catalogue.Merchant) string {
	if merchant != nil && merchant.DeliveryStyle == "quote" {
		return "quote"
	}
	return "set"
}

// deliveryFeeFor is the fee a "set" shop charges to a place:
// her own fee for the area the place names, when she has
// one (the longest matching area name wins, so "East Legon"
// beats "Legon"), and her standard fee everywhere else.
func deliveryFeeFor(merchant *catalogue.Merchant, place string) float64 {
	if merchant == nil {
		return 0
	}
	lower := strings.ToLower(place)
	best := -1
	fee := merchant.DeliveryFee
	for _, a := range merchant.DeliveryAreas {
		if a.Area == "" || !strings.Contains(lower, strings.ToLower(a.Area)) {
			continue
		}
		if len(a.Area) > best {
			best = len(a.Area)
			fee = a.Fee
		}
	}
	return fee
}

// beginFeeQuote pauses the buyer's sale at the feewait step
// and asks the seller, in her own chat, for this order's
// delivery fee. The buyer is told plainly what is happening
// and that nothing is paid before she sees the full total.
func beginFeeQuote(from string, s *Session, merchant *catalogue.Merchant, loc string) {
	shopID := s.MerchantID
	openFeeQuote(feeQuote{
		ShopID: shopID, Buyer: from, Item: s.itemLabel(),
		Price: basketGoods(s) + s.AgreedPrice, Place: loc,
	})
	s.FulfilStep = "feewait"
	s.FeeQuotePlace = loc
	sessions[sessionKey(shopID, from)] = s
	sendPieces(from,
		"Delivery to "+loc+" is priced by "+merchant.Name+" for each order, so I have asked her what it will cost.",
		"The moment she answers, I will show you the fee and your full total. Nothing is paid before you see it.")
	if phone := merchantNotifyPhone(merchant); phone != "" {
		whatsapp.SendText(phone, "Delivery fee needed\n\nA buyer is buying the "+s.itemLabel()+" ("+money.GHS(basketGoods(s)+s.AgreedPrice)+") for delivery to "+loc+".\nWhat should delivery cost her? Reply with the fee, like: fee 30.\nIf you cannot deliver there, reply: no delivery.")
	}
}

// reopenFeeQuote asks the seller again after a timeout, at
// the buyer's word: a fresh request, a fresh alert.
func reopenFeeQuote(from string, s *Session, merchant *catalogue.Merchant) {
	loc := s.FeeQuotePlace
	if loc == "" {
		loc = s.Location
	}
	whatsapp.SendText(from, "I have asked "+merchant.Name+" again.")
	beginFeeQuote(from, s, merchant, loc)
}

var (
	noDeliveryRe = regexp.MustCompile(`(?i)^(no delivery|no|none|cannot deliver|can't deliver|can not deliver|do not deliver|don't deliver)`)
	feeLeadRe    = regexp.MustCompile(`(?i)^(fee|delivery|charge|it is|its|it's)\b`)
)

// feeAnswerOf reads a seller's message as her answer to an
// open fee request: a fee figure in a short message, the
// words "fee 30" however they are dressed, or a refusal.
// Anything longer or less plain is not an answer, and her
// ordinary handling keeps it.
func feeAnswerOf(msg, lower string) (fee float64, declined bool, ok bool) {
	if noDeliveryRe.MatchString(strings.TrimSpace(lower)) && len(strings.Fields(lower)) <= 4 {
		return 0, true, true
	}
	if len(strings.Fields(msg)) > 6 && !feeLeadRe.MatchString(strings.TrimSpace(lower)) {
		return 0, false, false
	}
	if amt := parseOffer(msg); amt != nil && *amt > 0 {
		return *amt, false, true
	}
	return 0, false, false
}

// handleMerchantFeeQuote is the seller's answer to an open
// per-order fee request, consulted in her own chat after her
// in-progress flows. It reports whether it took the message.
// Her figure answers the shop's oldest open request; a
// refusal tells that buyer plainly and hands her the choice
// again.
func handleMerchantFeeQuote(from string, merchant *catalogue.Merchant, msg, lower string) bool {
	q := oldestOpenFeeQuote(merchant.ID)
	if q == nil {
		return false
	}
	fee, declined, ok := feeAnswerOf(msg, lower)
	if !ok {
		return false
	}
	buyer := q.Buyer
	key := sessionKey(merchant.ID, buyer)
	sess := sessions[key]
	live := sess != nil && sess.Stage == "fulfilment" && (sess.FulfilStep == "feewait" || sess.FulfilStep == "feeexpired") && sess.FeeQuotePlace == q.Place
	if declined {
		settleFeeQuote(merchant.ID, buyer, "declined", 0)
		if live {
			sess.FulfilStep = "choice"
			sess.FeeQuotePlace = ""
			sessions[key] = sess
			sendPieces(buyer, merchant.Name+" cannot deliver to "+q.Place+".", "You can pick it up yourself, meet up, or give me another place to try.")
			sendChoiceQuestion(buyer, sess, merchant)
		} else {
			whatsapp.SendText(buyer, merchant.Name+" cannot deliver to "+q.Place+". When you are ready to buy again, tell me, and we will find another way for the goods to reach you.")
		}
		whatsapp.SendText(from, "Understood. I have told the buyer you cannot deliver to "+q.Place+".")
		return true
	}
	settleFeeQuote(merchant.ID, buyer, "answered", fee)
	if live {
		sess.FeeQuotePlace = ""
		sessions[key] = sess
		purse := "It joins the total you pay here, and you will see the full total before you pay."
		if deliveryPaidToOf(merchant) == "carrier" {
			purse = "You pay that to whoever brings the item, when it arrives."
		}
		sendPieces(buyer,
			merchant.Name+" says delivery to "+q.Place+" is "+money.GHS(fee)+".",
			purse)
		beginPackageDetails(buyer, sess, fulfilment{Mode: "delivery", Fee: fee, Location: q.Place})
		whatsapp.SendText(from, "Done. I have told the buyer delivery to "+q.Place+" is "+money.GHS(fee)+", and her order is going ahead.")
	} else {
		whatsapp.SendText(buyer, merchant.Name+" has quoted "+money.GHS(fee)+" for delivery to "+q.Place+". When you are ready to buy, tell me and we will use it.")
		whatsapp.SendText(from, "Done. That buyer is not in a live chat just now, so I have sent her the fee for when she is ready.")
	}
	return true
}

// CheckFeeQuotes is the timeout pass, run by the unpaid
// watch's sweep: a request the seller has not answered in
// time expires, and the buyer is offered the other ways —
// wait, pickup, or meet-up — rather than left standing.
func CheckFeeQuotes(now time.Time) {
	feeQuotesMu.Lock()
	all := loadFeeQuotes()
	var expired []feeQuote
	for i := range all {
		if all[i].Status != "open" {
			continue
		}
		created, err := time.Parse(time.RFC3339, all[i].CreatedAt)
		if err != nil || now.Sub(created) < feeQuoteTimeout {
			continue
		}
		all[i].Status = "expired"
		expired = append(expired, all[i])
	}
	if len(expired) > 0 {
		saveFeeQuotes(all)
	}
	feeQuotesMu.Unlock()
	for _, q := range expired {
		merchant := catalogue.GetMerchant(q.ShopID)
		name := "The shop"
		if merchant != nil {
			name = merchant.Name
		}
		key := sessionKey(q.ShopID, q.Buyer)
		if sess := sessions[key]; sess != nil && sess.Stage == "fulfilment" && sess.FulfilStep == "feewait" {
			sess.FulfilStep = "feeexpired"
			sessions[key] = sess
		}
		whatsapp.SendText(q.Buyer, name+" has not given the delivery fee for "+q.Place+" yet. You can keep waiting, just say wait, or pick it up yourself, or meet up instead. Tell me which, and nothing is paid until you have seen your full total.")
	}
}
