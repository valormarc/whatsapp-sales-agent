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
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

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
	// Parked carries a repeat order's settled details (see
	// reorder.go) across the fee wait, so when the fee lands
	// the sale goes straight to its recap instead of asking
	// again for what the book already holds.
	Parked *fulfilment `json:"parked,omitempty"`
}

var feeQuotesMu sync.Mutex

func loadFeeQuotes() []feeQuote {
	var out []feeQuote
	loadBookJSON(datadir.Path("feequotes.json"), &out)
	return out
}

func saveFeeQuotes(all []feeQuote) {
	saveBookJSON(datadir.Path("feequotes.json"), all)
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
// forgetFeeQuotes drops every open fee quote naming one
// buyer, for a "delete my data" request.
func forgetFeeQuotes(buyer string) {
	feeQuotesMu.Lock()
	defer feeQuotesMu.Unlock()
	all := loadFeeQuotes()
	kept := all[:0]
	changed := false
	for _, q := range all {
		if q.Buyer == buyer {
			changed = true
			continue
		}
		kept = append(kept, q)
	}
	if changed {
		saveFeeQuotes(kept)
	}
}

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

// openFeeQuotes lists the shop's open requests in file
// order, oldest first.
func openFeeQuotes(shopID string) []feeQuote {
	feeQuotesMu.Lock()
	defer feeQuotesMu.Unlock()
	var out []feeQuote
	for _, q := range loadFeeQuotes() {
		if q.ShopID == shopID && q.Status == "open" {
			out = append(out, q)
		}
	}
	return out
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
		fee, err := parseFigure(part[last[0]:last[1]])
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
// An area matches only as a whole phrase — bounded by
// non-letters or the string's edges — never as a raw
// substring: the third audit found "Bosuso" charged the
// Osu fee because "osu" sits inside it.
func deliveryFeeFor(merchant *catalogue.Merchant, place string) float64 {
	if merchant == nil {
		return 0
	}
	lower := strings.ToLower(place)
	best := -1
	fee := merchant.DeliveryFee
	for _, a := range merchant.DeliveryAreas {
		if a.Area == "" || !areaPhraseIn(lower, strings.ToLower(a.Area)) {
			continue
		}
		if len(a.Area) > best {
			best = len(a.Area)
			fee = a.Fee
		}
	}
	return fee
}

// areaPhraseIn reports whether the area phrase stands in
// the place as a phrase of its own: the characters on
// either side of the match are not letters, so "osu" is
// in "Osu, Accra" and "East Legon" but not in "Bosuso".
func areaPhraseIn(lowerPlace, lowerArea string) bool {
	for from := 0; from < len(lowerPlace); {
		i := strings.Index(lowerPlace[from:], lowerArea)
		if i < 0 {
			return false
		}
		i += from
		before := i == 0 || !unicode.IsLetter(rune(lowerPlace[i-1]))
		afterAt := i + len(lowerArea)
		after := afterAt == len(lowerPlace) || !unicode.IsLetter(rune(lowerPlace[afterAt]))
		if before && after {
			return true
		}
		from = i + 1
	}
	return false
}

// beginFeeQuote pauses the buyer's sale at the feewait step
// and asks the seller, in her own chat, for this order's
// delivery fee. The buyer is told plainly what is happening
// and that nothing is paid before she sees the full total.
func beginFeeQuote(from string, s *Session, merchant *catalogue.Merchant, loc string) {
	shopID := s.MerchantID
	q := feeQuote{
		ShopID: shopID, Buyer: from, Item: s.itemLabel(),
		Price: basketGoods(s) + s.AgreedPrice, Place: loc,
	}
	if s.FeePark != nil {
		parked := *s.FeePark
		q.Parked = &parked
		s.FeePark = nil
	}
	openFeeQuote(q)
	s.FulfilStep = "feewait"
	s.FeeQuotePlace = loc
	sessions.put(sessionKey(shopID, from), s)
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
// merchantAwaiting resolves whose open request a seller's
// answer belongs to: her current shop when it is waiting,
// otherwise any other shop she owns that is. A seller with
// two shops answers once, in her own chat, and the answer
// belongs to the request that stands open, not to whichever
// shop her last conversation happened to sit in.
func merchantAwaiting(from string, current *catalogue.Merchant, waiting func(string) bool) *catalogue.Merchant {
	if waiting(current.ID) {
		return current
	}
	for id := range shopsOfOwner(from) {
		if id != current.ID && waiting(id) {
			if m := catalogue.GetMerchant(id); m != nil {
				return m
			}
		}
	}
	return current
}

func handleMerchantFeeQuote(from string, merchant *catalogue.Merchant, msg, lower string) bool {
	merchant = merchantAwaiting(from, merchant, func(id string) bool { return oldestOpenFeeQuote(id) != nil })
	quotes := openFeeQuotes(merchant.ID)
	if len(quotes) == 0 {
		return false
	}
	// Her commands are never per-order answers, however
	// many quotes stand open. The second audit exempted
	// the standing-fee command alone; the third found
	// its sibling — "set area fee Kasoa 80", a command
	// carrying exactly a place and a figure, the quote
	// answer's own shape — still consumed as the oldest
	// quote's fee, the area fee left unset. The exemption
	// is the command family, not the one shape.
	for _, re := range []*regexp.Regexp{
		setDeliveryFeeRe, setAreaFeeRe, setDeliveryNoteRe,
		setDeliveryQuoteRe, setDeliverySetRe, setMeasureRe,
		changePriceRe, changeFloorRe, setStockRe,
		setAllowanceRe, priceCorrectionRe,
		// The allowance note's own set shapes ("set it
		// 300", "make it 300", "put 200") are answers to
		// the note, never to a fee request, however many
		// quotes stand open (the fifth audit: the note
		// invites exactly this reply, and the fee lane
		// read it as the oldest quote's fee, in the same
		// family as the area-fee command above).
		noteSetItRe,
	} {
		if re.MatchString(lower) {
			return false
		}
	}
	// A bare yes or no belongs to an armed button question
	// when one stands on her session (an in-hand wait above
	// all): the refusal shape below would otherwise settle
	// the oldest quote as declined on a "no" that was
	// answering something else entirely.
	if isClearYes(lower) || isClearNo(lower) {
		if ms := sessions.get(sessionKey(merchant.ID, from)); ms != nil && ms.YesNoQ != nil {
			return false
		}
	}
	fee, declined, ok := feeAnswerOf(msg, lower)
	if !ok {
		return false
	}
	// The waiting quotes are counted across ALL the
	// seller's shops before anything binds (the sixth
	// audit): merchantAwaiting resolved her to one shop
	// first — current, else an arbitrary other — so a
	// bare figure bound to that shop's oldest quote while
	// another of her shops also waited, invisible. When
	// more than one shop waits, the answer must name
	// exactly one waiting place across every shop, or
	// she is asked with the full list.
	{
		type waitingQuote struct {
			shop  *catalogue.Merchant
			quote feeQuote
		}
		var all []waitingQuote
		seen := map[string]bool{}
		for id := range shopsOfOwner(from) {
			seen[id] = true
			if m := catalogue.GetMerchant(id); m != nil {
				for _, oq := range openFeeQuotes(id) {
					all = append(all, waitingQuote{m, oq})
				}
			}
		}
		if !seen[merchant.ID] {
			for _, oq := range quotes {
				all = append(all, waitingQuote{merchant, oq})
			}
		}
		if len(all) > len(quotes) {
			matches := 0
			var namedShop *catalogue.Merchant
			for _, wq := range all {
				if wq.quote.Place != "" && strings.Contains(lower, strings.ToLower(wq.quote.Place)) {
					matches++
					namedShop = wq.shop
				}
			}
			if matches == 1 && namedShop.ID != merchant.ID {
				merchant = namedShop
				quotes = openFeeQuotes(merchant.ID)
			} else if matches != 1 {
				var lines []string
				for _, wq := range all {
					lines = append(lines, wq.quote.Item+" to "+wq.quote.Place+" ("+wq.shop.Name+")")
				}
				whatsapp.SendText(from, "More than one buyer is waiting on a delivery fee: "+strings.Join(lines, "; ")+". Reply with the place and the fee, like: "+all[0].quote.Place+" 30 — or say no delivery for the place you cannot reach.")
				return true
			}
		}
	}
	q := &quotes[0]
	if len(quotes) > 1 {
		// More than one buyer is waiting: a bare figure
		// cannot say whose fee it is, and binding it to
		// the oldest by file order prices the wrong
		// buyer's delivery (the second audit). The answer
		// must name the place; until it does, she is asked
		// which, and nothing is bound.
		var named *feeQuote
		matches := 0
		for i := range quotes {
			if quotes[i].Place != "" && strings.Contains(lower, strings.ToLower(quotes[i].Place)) {
				named = &quotes[i]
				matches++
			}
		}
		if matches == 1 {
			q = named
		} else {
			var lines []string
			for _, oq := range quotes {
				lines = append(lines, oq.Item+" to "+oq.Place)
			}
			whatsapp.SendText(from, "More than one buyer is waiting on a delivery fee: "+strings.Join(lines, "; ")+". Reply with the place and the fee, like: "+quotes[0].Place+" 30 — or say no delivery for the place you cannot reach.")
			return true
		}
	}
	buyer := q.Buyer
	key := sessionKey(merchant.ID, buyer)
	// Everything below writes the buyer's session from
	// the merchant's turn — rewinding her fulfilment,
	// parking the quoted fee, driving her flow onward —
	// so it runs under the buyer's turn lock.
	withCrossLock(from, buyer, func() {
		sess := sessions.get(key)
		live := sess != nil && sess.Stage == "fulfilment" && (sess.FulfilStep == "feewait" || sess.FulfilStep == "feeexpired") && sess.FeeQuotePlace == q.Place
		if declined {
			settleFeeQuote(merchant.ID, buyer, "declined", 0)
			if live {
				sess.FulfilStep = "choice"
				sess.FeeQuotePlace = ""
				sessions.put(key, sess)
				sendPieces(buyer, merchant.Name+" cannot deliver to "+q.Place+".", "You can pick it up yourself, meet up, or give me another place to try.")
				sendChoiceQuestion(buyer, sess, merchant)
			} else {
				whatsapp.SendText(buyer, merchant.Name+" cannot deliver to "+q.Place+". When you are ready to buy again, tell me, and we will find another way for the goods to reach you.")
			}
			whatsapp.SendText(from, "Understood. I have told the buyer you cannot deliver to "+q.Place+".")
			return
		}
		settleFeeQuote(merchant.ID, buyer, "answered", fee)
		if live {
			sess.FeeQuotePlace = ""
			sessions.put(key, sess)
			purse := "It joins the total you pay here, and you will see the full total before you pay."
			if deliveryPaidToOf(merchant) == "carrier" {
				purse = "You pay that to whoever brings the item, when it arrives."
			}
			sendPieces(buyer,
				merchant.Name+" says delivery to "+q.Place+" is "+money.GHS(fee)+".",
				purse)
			if q.Parked != nil {
				// A repeat order rides through on its parked
				// details: the fee was the only missing fact, and
				// it has just landed, so the recap is next.
				f := *q.Parked
				f.Fee = fee
				f.Location = q.Place
				sess.PendingFulfil = f
				sess.FulfilStep = "confirm"
				sessions.put(key, sess)
				sendOrderRecap(buyer, sess, f)
			} else {
				beginPackageDetails(buyer, sess, fulfilment{Mode: "delivery", Fee: fee, Location: q.Place})
			}
			whatsapp.SendText(from, "Done. I have told the buyer delivery to "+q.Place+" is "+money.GHS(fee)+", and her order is going ahead.")
		} else {
			whatsapp.SendText(buyer, merchant.Name+" has quoted "+money.GHS(fee)+" for delivery to "+q.Place+". When you are ready to buy, tell me and we will use it.")
			whatsapp.SendText(from, "Done. That buyer is not in a live chat just now, so I have sent her the fee for when she is ready.")
		}
	})
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
		if len(expired) >= maxPrePassPerSweep {
			continue // bounded per sweep; the rest carry to the next tick
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
	for i, q := range expired {
		if prePassSpent() {
			// The sweep's shared pre-pass budget is
			// gone: the tail goes back to open, in
			// step with the messages never sent, and
			// carries to the next tick — an expired
			// mark without its message would strand
			// the buyer in a step nothing advances.
			unexpireFeeQuotes(expired[i:])
			break
		}
		merchant := catalogue.GetMerchant(q.ShopID)
		name := "The shop"
		if merchant != nil {
			name = merchant.Name
		}
		key := sessionKey(q.ShopID, q.Buyer)
		// The pass writes the buyer's session, so it takes
		// her turn lock, the same discipline as the
		// in-hand pass beside it.
		lock := turnLockFor(q.Buyer)
		lock.Lock()
		if sess := sessions.get(key); sess != nil && sess.Stage == "fulfilment" && sess.FulfilStep == "feewait" {
			sess.FulfilStep = "feeexpired"
			sessions.put(key, sess)
		}
		lock.Unlock()
		whatsapp.SendText(q.Buyer, name+" has not given the delivery fee for "+q.Place+" yet. You can keep waiting, just say wait, or pick it up yourself, or meet up instead. Tell me which, and nothing is paid until you have seen your full total.")
	}
}

// unexpireFeeQuotes returns quotes a budget-cut pass
// marked expired but never messaged back to open, so the
// next tick actions them whole — mark and message in
// step, never one without the other.
func unexpireFeeQuotes(qs []feeQuote) {
	feeQuotesMu.Lock()
	defer feeQuotesMu.Unlock()
	all := loadFeeQuotes()
	for i := range all {
		if all[i].Status != "expired" {
			continue
		}
		for _, q := range qs {
			if all[i].ShopID == q.ShopID && all[i].Buyer == q.Buyer && all[i].Place == q.Place && all[i].CreatedAt == q.CreatedAt {
				all[i].Status = "open"
			}
		}
	}
	saveFeeQuotes(all)
}
