// Package agent is the conversation handler. When the Meta Model API
// key is set, Muse Spark leads the haggling inside the persona
// (package persona): it reads the customer and proposes each step, and
// haggle.CheckProposal verifies every proposed price against the day
// book's operative floor before anything is sent. Without a key, or
// whenever a proposal fails that check, the rule-based engine
// (packages haggle and daybook) makes the call, so the conversation
// never depends on the model being reachable.
//
// Branches: greet and list, quote, haggle, close with a payment link,
// notify the merchant. Grounded on the merchant catalogue only, and
// the floor price never reaches the customer.
//
// Haggling doctrine: you do not haggle to bleed.
// You haggle to recover what the day has cost you, bit by bit from the
// customers who come after, and to gain. So the day book firms the
// agent up as the day's giveaway pool drains. And sometimes the money
// on the table means a real loss: then the agent makes one final
// stand, and if the customer will not meet it, the agent lets them go
// politely and keeps the door open.
package agent

import (
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/haggle"
	"github.com/valormarc/hackathon-agent/internal/insights"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/persona"
	"github.com/valormarc/hackathon-agent/internal/wants"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// MerchantID is the id of the seeded demo shop (Efua's Store). It is
// no longer the only shop: the catalogue holds many shops on the one
// shared number, and sessions, orders and the day book are all keyed
// by the shop they belong to. This id survives as the shop the legacy
// MERCHANT_WHATSAPP env number owns when the shop itself records no
// owner.
const MerchantID = "demo"

// MaxCounters is the number of counters after which the agent makes
// its final stand.
const MaxCounters = 5

// Session is one sender's conversation state inside one shop.
// MerchantID is that shop: a session never spans shops, so a haggle
// opened in one shop cannot be read, priced or closed in another.
type Session struct {
	MerchantID  string
	Stage       string
	Product     *catalogue.Product
	Ask         float64
	Round       int
	Floor       float64
	StepScale   float64
	History     []model.Message
	AgreedPrice float64
	PaymentURL  string

	// LastViewed is the item the buyer last asked about while
	// browsing, before any haggle opened. Real buyers jump straight
	// from looking at an item to "can you do it for 150", naming
	// nothing; this is how the agent knows which item she means.
	LastViewed *catalogue.Product

	// Qty and Unit are the measure in play for this conversation
	// (see measure.go): a quantity the buyer has asked about
	// while browsing, remembered so her next plain offer lands on
	// it, or the quantity an open haggle is priced on, in which
	// case Product holds the scaled copy (list and floor scaled
	// to Qty from the product's base quantity). Zero Qty means
	// the whole item, as every session was before measures.
	Qty  float64
	Unit string

	// Buyer commitment state (from my own buying chats): a
	// buyer who says "I'll buy" without a price agreement is quoted
	// one total and asked plainly; only her clear yes at that step
	// closes, at ClosePrice. PendingClose is that open question.
	// CloseFromView says the item was borrowed from LastViewed
	// (browsing), so a no hands it back instead of leaving a haggle
	// product sitting on a browsing session.
	PendingClose  bool
	ClosePrice    float64
	CloseReasked  bool
	CloseFromView bool

	// Fulfilment state: a price agreement holds here until the
	// buyer says how the goods reach her (delivery to a place, her
	// own pickup, a meet-up, a collector she sends, or arrangement
	// with the seller). FulfilStep is the open question: "choice",
	// "where" (delivery chosen, place unknown), "meetwhere",
	// "who" (a collector is coming, not yet named) or "based" (a
	// shop with no delivery terms, asking where she is). Location
	// is the place she has stated anywhere in the chat, kept so
	// she is never asked for it twice.
	FulfilStep string
	Location   string

	// Voice rotation counters: how many times this session has been
	// sent the holding reply, the shelf offer and the bare-ack reply.
	// They exist so repeated lines rotate through natural variants
	// instead of repeating word for word like a machine.
	Holds   int
	Shelves int
	Acks    int

	// Lane state: how many clear off-topic turns in a row this
	// buyer has spent outside the shop (general knowledge, sums,
	// essays and the like), and whether the lane has closed on
	// her. While it is closed her off-topic turns are not answered
	// at all, so they cost the merchant nothing; any turn about
	// the shop reopens it. See the lane block in HandleInbound.
	OffTopic   int
	LaneClosed bool

	// Merchant-only chat state: an in-progress onboarding flow, or a
	// removal waiting for her yes. Only ever set on the merchant's own
	// session; customers never reach the code that reads them.
	Onboarding    *OnboardingFlow
	PendingRemove string

	// Signup is a new merchant's open-a-shop flow, held on the
	// pre-shop session until her shop exists; then she gets a normal
	// owner session in her own shop.
	Signup *SignupFlow
}

var (
	sessionsMu sync.Mutex
	// sessions is keyed shopID + "|" + sender phone (sessionKey).
	// Keying by shop as well as sender is the isolation rule: the
	// same person in two shops has two unrelated sessions.
	sessions = map[string]*Session{}
	// bindings records which shop each buyer is currently inside, so
	// her later messages, which carry no shop code, stay in the shop
	// her link brought her to until she enters another shop's code.
	bindings = map[string]string{} // buyer phone -> shopID
)

func sessionKey(shopID, from string) string {
	return shopID + "|" + from
}

var digitsRe = regexp.MustCompile(`\D`)

func digitsOnly(v string) string {
	return digitsRe.ReplaceAllString(v, "")
}

// IsMerchant reports whether a sender matches the MERCHANT_WHATSAPP
// env number: the original single-shop recognition, kept as the
// fallback owner test for a shop that records no owner of its own
// (the seeded demo shop). Comparison is on digits so +233 and
// spacing differences never misroute her.
func IsMerchant(from string) bool {
	merchant := digitsOnly(os.Getenv("MERCHANT_WHATSAPP"))
	return merchant != "" && digitsOnly(from) == merchant
}

// ownerShopFor resolves the shop a sender owns, if any: first the
// catalogue's own record (each shop names its owner), then the env
// fallback, which owns the demo shop only. One door, two modes: an
// owner's number is recognised, and she gets her business partner
// for her own shop, never another shop's.
func ownerShopFor(from string) *catalogue.Merchant {
	if m := catalogue.MerchantByOwner(from); m != nil {
		return m
	}
	if IsMerchant(from) {
		return catalogue.GetMerchant(MerchantID)
	}
	return nil
}

// merchantNotifyPhone is the number a shop's notices go to: the
// owner the shop itself records, falling back to the env number only
// when the shop has no owner set.
func merchantNotifyPhone(merchant *catalogue.Merchant) string {
	if merchant != nil && merchant.OwnerWhatsApp != "" {
		return merchant.OwnerWhatsApp
	}
	return os.Getenv("MERCHANT_WHATSAPP")
}

// sendPieces sends a reply the way a person types it on WhatsApp:
// piece by piece, each its own short message, one idea each, never
// one big block of text like a letter. Figures stay whole inside
// their own piece, exact and unambiguous.
func sendPieces(from string, pieces ...string) {
	for _, piece := range pieces {
		if strings.TrimSpace(piece) != "" {
			whatsapp.SendText(from, piece)
		}
	}
}

// ackWords are the bare acknowledgements a buyer sends to mean
// "received", never "sold": in my own buying chats "Ok",
// "I'll contact you" and "I'll buy" none of them ever closed
// anything; only payment did. A message that is exactly one of these
// (punctuation aside) must never settle a deal in any stage.
var ackWords = map[string]bool{
	"ok": true, "okay": true, "ayt": true, "noted": true,
	"alright": true, "oh ok": true, "oh okay": true,
	"alright then": true, "fine": true,
}

func isBareAck(lower string) bool {
	return ackWords[controlWord(lower)]
}

// deliveryFeeOf is the shop's delivery charge, zero when the shop
// has no fixed fee (delivery arranged with the seller instead).
func deliveryFeeOf(merchant *catalogue.Merchant) float64 {
	if merchant == nil {
		return 0
	}
	return merchant.DeliveryFee
}

// deliveryPaidToOf is who takes the shop's delivery money:
// "merchant" only when the shop says so in its own record; every
// other shop, set or unset, is "carrier". Buyers mostly pay their
// own delivery to whoever brings the item when it arrives, so the
// common case is the default and the fee joins the merchant's
// money only by her own word.
func deliveryPaidToOf(merchant *catalogue.Merchant) string {
	if merchant != nil && merchant.DeliveryPaidTo == "merchant" {
		return "merchant"
	}
	return "carrier"
}

// carrierTarget names who physically receives the delivery money,
// read conservatively out of the shop's own delivery note: a note
// that speaks of a rider means the rider, one that speaks of the
// bus or the station means the station, a driver or a car means
// the driver, and anything else is whoever brings it. The phrase
// carries its own article, so sentences can lean on it whole:
// "you pay that to the rider when it reaches you", "you pay that
// to whoever brings it when it reaches you".
func carrierTarget(note string) string {
	n := strings.ToLower(note)
	switch {
	case strings.Contains(n, "rider"):
		return "the rider"
	case strings.Contains(n, "bus"), strings.Contains(n, "station"):
		return "the station"
	case strings.Contains(n, "driver"), strings.Contains(n, "car"):
		return "the driver"
	default:
		return "whoever brings it"
	}
}

// Merchant mode day answers: answer from the day book, in day
// language. Muse Spark phrases the answer inside
// BuildMerchantPrompt's grounding when it is reachable; the plain day
// report is the fallback, so she is never left without her figures.
// The full merchant dispatcher (onboarding, commands, then this)
// lives in merchant.go.
func answerMerchantDayQuestion(from, text, merchantID string) {
	merchant := catalogue.GetMerchant(merchantID)
	day := daybook.Default.GetDayState(merchantID, daybook.Options{Allowance: merchant.DailyGiveawayAllowance})
	if model.Enabled() {
		sys := persona.BuildMerchantPrompt(*merchant, day)
		reply := model.Chat([]model.Message{{Role: "user", Content: text}}, model.Options{System: &sys})
		if reply != nil {
			whatsapp.SendText(from, *reply)
			return
		}
	}
	whatsapp.SendText(from, "Here is today so far. "+insights.DayReportText(day)+" Ask me any time and I will pull the same figures for you.")
}

func productListText(merchantID string) string {
	parts := make([]string, 0)
	for _, p := range catalogue.ListProducts(merchantID) {
		parts = append(parts, "- "+p.Name+": "+money.GHS(p.ListPrice))
	}
	return "Here is what we have:\n" + strings.Join(parts, "\n") + "\nAsk me about any item, or make me an offer."
}

var offerRe = regexp.MustCompile(`(\d+(?:\.\d{1,2})?)`)
var numbersRe = regexp.MustCompile(`(\d+(?:\.\d+)?)`)

// parseOffer reads the first price in a message. A number that
// forms part of a quantity phrase is a quantity, never a price,
// on every path that reads offers (browsing, mid-haggle, the
// final stand, the merchant's own amounts): "Ankara 3 yards"
// carries no offer, and in "200 for 3 yards" the 200 is the offer
// and the 3 is the quantity it applies to (see measure.go).
func parseOffer(text string) *float64 {
	stripped := strings.ReplaceAll(text, ",", "")
	spans := quantityNumberSpans(stripped)
	for _, m := range offerRe.FindAllStringSubmatchIndex(stripped, -1) {
		inside := false
		for _, sp := range spans {
			if m[0] < sp[1] && sp[0] < m[1] {
				inside = true
				break
			}
		}
		if inside {
			continue
		}
		f, err := strconv.ParseFloat(stripped[m[2]:m[3]], 64)
		if err != nil {
			continue
		}
		return &f
	}
	return nil
}

func numbersIn(text string) []float64 {
	out := []float64{}
	for _, m := range numbersRe.FindAllStringSubmatch(strings.ReplaceAll(text, ",", ""), -1) {
		if f, err := strconv.ParseFloat(m[1], 64); err == nil {
			out = append(out, f)
		}
	}
	return out
}

// The model's words are used only when every number in them is the
// verified price or the list price it is framed against. A message
// that names any other figure is discarded and the framed counter is
// sent instead: the words a customer reads always match the checked
// price.
func modelMessage(proposal *haggle.Proposal, checked haggle.Result, product catalogue.Product) *string {
	msg := ""
	if proposal != nil {
		msg = strings.TrimSpace(proposal.Message)
	}
	if msg == "" {
		return nil
	}
	nums := numbersIn(msg)
	contains := false
	for _, n := range nums {
		if n == checked.Price {
			contains = true
		}
	}
	if !contains {
		return nil
	}
	for _, n := range nums {
		if n != checked.Price && n != product.ListPrice {
			return nil
		}
	}
	return &msg
}

type stepResult struct {
	haggle.Result
	ViaModel bool
	Message  *string
}

// One haggling step. The model proposes inside the persona; the engine
// verifies. Any doubt at all (model off, no reply, unparseable reply,
// a price outside the bounds) and Decide makes the call instead.
func negotiateStep(s *Session, offer float64, merchant *catalogue.Merchant) stepResult {
	p := s.Product
	fallback := func() stepResult {
		return stepResult{Result: haggle.Decide(haggle.DecideParams{
			Offer: offer, Ask: s.Ask, Floor: s.Floor, Round: s.Round, StepScale: s.StepScale,
		}), ViaModel: false}
	}
	if !model.Enabled() {
		return fallback()
	}
	day := daybook.Default.GetDayState(merchant.ID, daybook.Options{Allowance: merchant.DailyGiveawayAllowance})
	history := s.History
	if len(history) > 8 {
		history = history[len(history)-8:]
	}
	// On a measured haggle the model's context names the amount
	// in play ("3 yards of Ankara Fabric (6 yards)") and bounds
	// it by the scaled figures on the session's product copy: the
	// model negotiates the cut, never the whole piece by accident.
	promptProduct := *p
	if s.Qty > 0 {
		promptProduct.Name = s.itemLabel()
	}
	userContent := "The customer offers " + money.GHS(offer) + " for the " + promptProduct.Name + ". Decide the next step and answer with ONE JSON object only, no other text: " +
		`{"action":"accept"|"counter"|"final","price":<number>,"message":"<the exact short WhatsApp message to send the customer>"}. ` +
		`"accept" settles at the customer's offer. "counter" names your new price. "final" is your last price. ` +
		"Frame the message as movement from the list price. Never propose below your private stop."
	msgs := make([]model.Message, 0, len(history)+1)
	msgs = append(msgs, history...)
	msgs = append(msgs, model.Message{Role: "user", Content: userContent})
	ctx := persona.Context{
		Merchant:  *merchant,
		Product:   &promptProduct,
		Bounds:    &persona.Bounds{ListPrice: p.ListPrice, Stop: s.Floor, Ask: s.Ask},
		Day:       &day,
		Catalogue: catalogue.ListProducts(merchant.ID),
	}
	raw := model.Chat(msgs, model.Options{Context: &ctx})
	proposal := model.ParseProposal(raw)
	var checked *haggle.Result
	if proposal != nil {
		checked = haggle.CheckProposal(proposal, haggle.Bounds{Offer: offer, Ask: s.Ask, Floor: s.Floor})
	}
	if checked == nil {
		return fallback()
	}
	return stepResult{Result: *checked, ViaModel: true, Message: modelMessage(proposal, *checked, *p)}
}

// fulfilment is how the goods reach the buyer, settled in chat
// after the price is agreed: delivery to a place, her own pickup,
// a meet-up, a collector she sends, or (in a shop with no delivery
// terms of its own) arrangement between her and the seller. Only
// delivery ever carries the shop's delivery fee.
type fulfilment struct {
	Mode      string // "delivery", "pickup", "meet", "arranged"
	Fee       float64
	Location  string
	Collector string
}

// shopHasDeliveryTerms reports whether the shop states its own
// delivery terms: a fee, a note, or both. Such a shop's buyers are
// offered the choice (delivery on those terms, or pickup free);
// a shop with neither is asked only where the buyer is, so her
// seller can arrange things with her.
func shopHasDeliveryTerms(merchant *catalogue.Merchant) bool {
	return merchant != nil && (merchant.DeliveryFee > 0 || strings.TrimSpace(merchant.DeliveryNote) != "")
}

// deliveryOptionLine builds the delivery offer from the shop's own
// words: her note as the seller typed it, then the fee. Nothing is
// invented per shop; a fee with no note is just the fee. A place
// the buyer already stated is confirmed inside the line, so she is
// never asked for it again.
//
// Who takes the fee changes what the line promises. When the
// merchant takes it herself, the fee is part of what the buyer
// pays here, and the line says so the way it always has. When the
// carrier takes it (the default), the line must not imply the fee
// joins this payment: it names the second purse plainly, the
// money she hands over when the goods reach her.
func deliveryOptionLine(merchant *catalogue.Merchant, knownLocation string) string {
	note := strings.TrimRight(strings.TrimSpace(merchant.DeliveryNote), ".")
	var line string
	switch {
	case deliveryPaidToOf(merchant) == "carrier" && merchant.DeliveryFee > 0:
		feeLine := "Delivery is " + money.GHS(merchant.DeliveryFee) + ", paid to " + carrierTarget(merchant.DeliveryNote) + " when it reaches you."
		if note != "" {
			line = note + ". " + feeLine
		} else {
			line = feeLine
		}
	case note != "" && merchant.DeliveryFee > 0:
		line = note + ". Delivery is " + money.GHS(merchant.DeliveryFee) + "."
	case note != "":
		line = note + "."
	default:
		line = "Delivery is " + money.GHS(merchant.DeliveryFee) + "."
	}
	if knownLocation != "" {
		line += " It goes to " + knownLocation + "."
	}
	return line
}

// beginFulfilment is where every price agreement now lands: the
// deal is real, but the sale does not complete, and no payment
// link is sent, until the buyer says how the goods reach her. A
// delivery fee is never charged blind: she chooses delivery and
// names the place, or she picks the goods up (herself, by meeting,
// or through a collector) and pays no fee at all. My ruling
// from my own buying chats: delivery method, location and cost
// are what actually close a sale, so they are settled here, in the
// same conversation, before the link.
func beginFulfilment(from string, s *Session, price float64) {
	shopID := s.MerchantID
	key := sessionKey(shopID, from)
	merchant := catalogue.GetMerchant(shopID)
	s.Stage = "fulfilment"
	s.AgreedPrice = price
	s.Ask = price
	sessions[key] = s
	deal := "Deal. " + s.itemLabel() + " is yours for " + money.GHS(price) + "."

	if !shopHasDeliveryTerms(merchant) {
		// No terms of its own: one question, where she is based,
		// and the seller arranges the rest with her. A place she
		// already stated in this chat is never asked for again.
		if s.Location != "" {
			sendPieces(from, deal)
			completeSale(from, s, price, fulfilment{Mode: "arranged", Location: s.Location})
			return
		}
		s.FulfilStep = "based"
		sessions[key] = s
		sendPieces(from, deal, "Where are you based?")
		return
	}

	s.FulfilStep = "choice"
	sessions[key] = s
	pieces := []string{deal, "How do you want to get it?", deliveryOptionLine(merchant, s.Location), "Or you can come and pick it up yourself, no delivery fee."}
	if s.Location != "" {
		pieces = append(pieces, "Which one?")
	} else {
		pieces = append(pieces, "Which one, and where are you?")
	}
	sendPieces(from, pieces...)
}

var (
	// pickupRe: she comes for the goods herself.
	pickupRe = regexp.MustCompile(`(pick\s?(it\s)?up|pickup|collect it myself|collect myself|come for it|i will come|i'll come|i can come|come and collect|pass by|come myself)`)
	// meetRe: she proposes a handover in person, somewhere.
	meetRe = regexp.MustCompile(`\bmeet(ing)?\b`)
	// deliveryIntentRe: she wants the goods sent. sendPersonRe
	// and the collector patterns are read before this one, so
	// "send my brother" is never misread as "send it".
	deliveryIntentRe = regexp.MustCompile(`(deliver|send it|send by|bring it|courier|transport|\bbus\b|\brider\b)`)
	// fulfilCancelRe: she calls the pending sale off.
	fulfilCancelRe = regexp.MustCompile(`(cancel|forget it|leave it|don'?t want|do not want)`)
)

// fulfilPrompt is the open fulfilment question, held up again when
// a reply answers something else: the pending sale is never dropped
// quietly, and never chased with more than the question itself.
func fulfilPrompt(s *Session) string {
	switch s.FulfilStep {
	case "where":
		return "Where should it go?"
	case "meetwhere":
		return "Where should we meet you?"
	case "who":
		return "Who is coming for it?"
	case "based":
		return "Where are you based?"
	default:
		return "So, how do you want to get it: we send it, or you pick it up?"
	}
}

// handleFulfilment runs the fulfilment stage: one open question at
// a time, answered in the buyer's own shapes (delivery with the
// place in the same breath, pickup, a meet-up, a collector), with
// one short follow-up when the missing piece is the place or the
// person. Cancel drops the pending sale cleanly; a question about
// the sale itself is answered and the question held up again, so
// drifting never kills a deal that was already agreed.
func handleFulfilment(from string, s *Session, merchant *catalogue.Merchant, msg, lower string) {
	shopID := s.MerchantID
	key := sessionKey(shopID, from)
	p := s.Product
	price := s.AgreedPrice
	fee := deliveryFeeOf(merchant)

	if fulfilCancelRe.MatchString(lower) {
		// Nothing was ordered and nothing enters the day book:
		// those are written only when a sale completes. The view
		// she keeps is the item as listed, not a measured amount's
		// scaled copy.
		view := p
		if s.Qty > 0 {
			view = wholeProductOf(shopID, p)
		}
		sessions[key] = &Session{Stage: "browsing", MerchantID: shopID, LastViewed: view, Location: s.Location}
		whatsapp.SendText(from, "No problem, I have dropped that sale. The "+p.Name+" is here whenever you are ready.")
		return
	}

	complete := func(f fulfilment) {
		if (f.Mode == "delivery" || f.Mode == "arranged") && f.Location != "" && s.Location == "" {
			s.Location = f.Location
		}
		completeSale(from, s, price, f)
	}

	// How the goods move can be changed at any point in the step:
	// these readings run before whichever question is open. A
	// meet-up is read first (its place rides in the same message),
	// then the collector shapes, then her own pickup.
	if meetRe.MatchString(lower) {
		if place := extractMeetPlace(msg); place != "" {
			complete(fulfilment{Mode: "meet", Location: place})
		} else {
			s.FulfilStep = "meetwhere"
			sessions[key] = s
			whatsapp.SendText(from, "Where should we meet you?")
		}
		return
	}
	if who, named, ok := sentSomebody(msg); ok {
		if named {
			complete(fulfilment{Mode: "pickup", Collector: who})
		} else {
			s.FulfilStep = "who"
			sessions[key] = s
			whatsapp.SendText(from, "Who is coming for it?")
		}
		return
	}
	if pickupRe.MatchString(lower) {
		complete(fulfilment{Mode: "pickup"})
		return
	}

	switch s.FulfilStep {
	case "based":
		// Whatever she answers is her place, read conservatively;
		// no readable place means she declined, and the sale
		// completes anyway: the question is asked once, never twice.
		loc := extractStatedLocation(msg)
		if loc == "" {
			loc = cleanPlace(msg)
		}
		complete(fulfilment{Mode: "arranged", Location: loc})
	case "where":
		loc := extractDeliveryPlace(msg)
		if loc == "" {
			loc = cleanPlace(msg)
		}
		complete(fulfilment{Mode: "delivery", Fee: fee, Location: loc})
	case "meetwhere":
		place := extractMeetPlace(msg)
		if place == "" {
			place = cleanPlace(msg)
			// "the station" is how the place is said; keep the
			// "the" cleanPlace dropped, as extractMeetPlace does.
			if place != "" && strings.HasPrefix(lower, "the ") {
				place = "the " + place
			}
		}
		if place == "" {
			whatsapp.SendText(from, "Where should we meet you?")
			return
		}
		complete(fulfilment{Mode: "meet", Location: place})
	case "who":
		who := cleanCollector(msg)
		if who == "" {
			whatsapp.SendText(from, "Who is coming for it?")
			return
		}
		complete(fulfilment{Mode: "pickup", Collector: who})
	default: // "choice"
		// A question about the sale itself (the total, delivery,
		// a picture) is answered, and the fulfilment question is
		// held up again: the pending sale survives the drift.
		if strings.HasSuffix(lower, "?") && (totalRe.MatchString(lower) || deliveryRe.MatchString(lower) || picRe.MatchString(lower)) {
			if picRe.MatchString(lower) && !totalRe.MatchString(lower) && !deliveryRe.MatchString(lower) {
				whatsapp.SendText(from, "I cannot send a photo of the "+p.Name+" from here, but it is this: "+p.Description+" It goes for "+money.GHS(p.ListPrice)+".")
			} else {
				answerDeliveryOrTotal(from, s, merchant, lower)
			}
			whatsapp.SendText(from, fulfilPrompt(s))
			return
		}
		if deliveryIntentRe.MatchString(lower) {
			loc := extractDeliveryPlace(msg)
			if loc == "" {
				loc = s.Location
			}
			if loc != "" {
				complete(fulfilment{Mode: "delivery", Fee: fee, Location: loc})
			} else {
				s.FulfilStep = "where"
				sessions[key] = s
				whatsapp.SendText(from, "Where should it go?")
			}
			return
		}
		// She answered the "where are you" half with a bare place
		// (a capitalised name, nothing else in the message): that
		// is delivery, to that place.
		if loc := barePlace(msg); loc != "" {
			complete(fulfilment{Mode: "delivery", Fee: fee, Location: loc})
			return
		}
		whatsapp.SendText(from, fulfilPrompt(s))
	}
}

// completeSale finishes a sale whose fulfilment is settled: the
// order is recorded with how the goods move, the day book books
// the item price, the payment link goes out, and the merchant is
// told the whole story, including who to hand the goods to.
func completeSale(from string, s *Session, price float64, f fulfilment) {
	p := s.Product
	shopID := s.MerchantID
	merchant := catalogue.GetMerchant(shopID)
	// The reference is the order's key in the book. It is built from
	// the millisecond clock, so two deals closed inside the same
	// millisecond would collide and the second would silently
	// overwrite the first; bump the stamp until the key is free.
	base := time.Now().UnixMilli()
	reference := "HACK-" + strconv.FormatInt(base, 10)
	for n := int64(1); orders.Default.GetOrder(reference) != nil; n++ {
		reference = "HACK-" + strconv.FormatInt(base+n, 10)
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
	total := price + chargedFee
	link := checkout.CreatePaymentLink(checkout.LinkRequest{Amount: total, Reference: reference})
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
	orders.Default.RecordOrder(reference, orders.Order{
		From:           from,
		ProductName:    p.Name,
		Price:          price,
		DeliveryFee:    deliveryFee,
		Total:          total,
		ProviderRef:    link.ProviderRef,
		Status:         status,
		MerchantID:     shopID,
		PayoutPhone:    payout,
		Fulfilment:     f.Mode,
		Location:       f.Location,
		Collector:      f.Collector,
		DeliveryPaidTo: paidTo,
		Quantity:       s.Qty,
		Unit:           s.Unit,
	})
	merchantPhone := merchantNotifyPhone(merchant)
	// Seev was configured but the session could not be created: say so,
	// to both sides, rather than send a link that does not work. No
	// sale is booked in the day book for a deal the customer cannot pay
	// for.
	if link.URL == nil {
		sessions[sessionKey(shopID, from)] = &Session{Stage: "browsing", MerchantID: shopID, Location: s.Location}
		whatsapp.SendText(from, "We have a deal at "+money.GHS(price)+" for the "+p.Name+", but the payment link is not coming up right now. The shop has been told and will reach out here to complete your order.")
		if merchantPhone != "" {
			whatsapp.SendText(merchantPhone, "Payment link failed for an agreed deal: "+p.Name+" at "+money.GHS(price)+", buyer "+from+". Ref: "+reference+". Please reach out to the buyer to complete it.")
		}
		return
	}
	url := *link.URL
	// The day book books the item price alone (see the delivery note
	// above): a sale at the item price, giveaway measured against the
	// list price, delivery money nowhere in the day's figures.
	day := daybook.Default.RecordSale(shopID, daybook.Sale{
		ListPrice:   p.ListPrice,
		AgreedPrice: price,
		Allowance:   merchant.DailyGiveawayAllowance,
	})
	sessions[sessionKey(shopID, from)] = &Session{Stage: "agreed", MerchantID: shopID, Product: p, AgreedPrice: price, PaymentURL: url, Location: s.Location}
	linkPiece := "Pay here to complete your order: " + url
	// The completion goes out piece by piece, the way a person
	// types it: the total the seller worked out for her, what
	// happens with the goods, then the link. The deal line itself
	// was already sent when the price was agreed (beginFulfilment).
	var pieces []string
	switch {
	case f.Mode == "delivery" && chargedFee > 0:
		// The merchant takes the delivery money herself: one
		// total, worked out for the buyer, fee inside it.
		line := "Item " + money.GHS(price) + " + delivery " + money.GHS(deliveryFee) + " = " + money.GHS(total) + " total"
		if f.Location != "" {
			line += ", to " + f.Location
		}
		pieces = []string{line + "."}
	case f.Mode == "delivery" && deliveryFee > 0:
		// The carrier takes the delivery money: two purses,
		// named plainly, so she never mistakes the fee for part
		// of what she pays here. What she pays here is the item.
		note := ""
		if merchant != nil {
			note = merchant.DeliveryNote
		}
		feeLine := "Delivery is " + money.GHS(deliveryFee) + ", you pay that to " + carrierTarget(note) + " when it reaches you."
		if f.Location != "" {
			feeLine += " It is going to " + f.Location + "."
		}
		pieces = []string{
			"Your total with me is " + money.GHS(price) + ".",
			feeLine,
		}
	case f.Mode == "delivery":
		line := "Your total is " + money.GHS(total) + "."
		if f.Location != "" {
			line = "Your total is " + money.GHS(total) + ". It goes to " + f.Location + "."
		}
		pieces = []string{line}
	case f.Mode == "pickup" && f.Collector != "":
		pieces = []string{
			"Noted. I have told the seller that " + f.Collector + " is coming for it.",
			"No delivery fee. Your total is " + money.GHS(total) + ".",
		}
	case f.Mode == "pickup":
		pieces = []string{
			"No delivery fee, you collect it yourself. Your total is " + money.GHS(total) + ".",
		}
	case f.Mode == "meet":
		// Honest wording, no guarantees beyond the sale: the
		// seller has been told the place and it is on the order;
		// whether she comes to it is hers to say.
		pieces = []string{
			"I have told the seller you want to meet at " + f.Location + " to collect it. The place is on your order.",
			"No delivery fee. Your total is " + money.GHS(total) + ".",
		}
	default: // arranged
		pieces = []string{
			"Delivery will be arranged with the seller.",
			"Your total is " + money.GHS(total) + ".",
		}
	}
	pieces = append(pieces, linkPiece)
	// The second door, offered beside the link: plenty of buyers
	// will not tap a link from a number they just met, but they
	// will send MoMo to the merchant's own number, the way they
	// already buy every day. The offer is worded from the order's
	// own payout phone, and is only made when the shop has one on
	// record: a number is never invented. Choosing it, and what a
	// screenshot can and cannot prove, lives in payment.go.
	if payout != "" {
		shopName := ""
		if merchant != nil {
			shopName = merchant.Name
		}
		pieces = append(pieces, "Or send MoMo directly to "+shopName+"'s number, "+payout+", and send me the screenshot here.")
	}
	sendPieces(from, pieces...)
	if merchantPhone != "" {
		// A measured sale is named by its amount, so the merchant
		// knows what to cut: "Sold: 3 yards of Ankara Fabric
		// (6 yards) at GH₵210." The figures behind it are the
		// scaled ones the haggle ran on, list included.
		what := p.Name + " sold for " + money.GHS(price)
		if s.Qty > 0 {
			what = "Sold: " + qtyLabel(p, s.Qty) + " at " + money.GHS(price)
		}
		notice := "New order: " + what + " (list " + money.GHS(p.ListPrice) + ", margin given away " + money.GHS(haggle.MarginGivenAway(p.ListPrice, price)) + ")."
		if chargedFee > 0 {
			notice += " Item " + money.GHS(price) + " + delivery " + money.GHS(deliveryFee) + " = " + money.GHS(total) + " total, which is what the buyer pays."
		}
		switch f.Mode {
		case "delivery":
			line := " Delivery: "
			note := ""
			rawNote := ""
			if merchant != nil {
				rawNote = merchant.DeliveryNote
				note = strings.TrimRight(strings.TrimSpace(rawNote), ".")
			}
			switch {
			case note != "" && f.Location != "":
				line += note + ", to " + f.Location
			case note != "":
				line += note
			case f.Location != "":
				line += "to " + f.Location
			default:
				line += "arranged with the buyer; she gave no location"
			}
			switch {
			case paidTo == "carrier" && deliveryFee > 0:
				// Her money is the item alone: the fee is the
				// buyer's to hand to the carrier on arrival,
				// and the notice says so in as many words, so
				// she never counts it as hers.
				line += ". The " + money.GHS(deliveryFee) + " delivery is hers to pay " + carrierTarget(rawNote) + " on arrival; your money is the " + money.GHS(price)
			case chargedFee > 0:
				line += ", fee " + money.GHS(deliveryFee) + " included in the total"
			}
			notice += line + "."
		case "pickup":
			if f.Collector != "" {
				notice += " A collector is coming for it: " + f.Collector + "."
			} else {
				notice += " The buyer will come and pick it up herself."
			}
		case "meet":
			notice += " The buyer wants to meet at " + f.Location + " to collect it."
		case "arranged":
			if f.Location != "" {
				notice += " Delivery to be arranged with the buyer (she is in " + f.Location + ")."
			} else {
				notice += " Delivery to be arranged with the buyer; no location given."
			}
		}
		notice += " Buyer: " + from + ". Ref: " + reference + ". Day book: " + insights.DayReportText(day)
		whatsapp.SendText(merchantPhone, notice)
	}
}

// placeWordRe is a single word of a place name: letters, with the
// hyphens and apostrophes real place names carry.
var placeWordRe = regexp.MustCompile(`^[A-Za-z][A-Za-z'\-]*$`)

// placeExtraFiller are words that make a short message a sentence
// rather than a place, beyond askFillerWords (which already holds
// the pronouns, question words and shop talk).
var placeExtraFiller = map[string]bool{
	"i": true, "am": true, "will": true, "would": true, "shall": true,
	"there": true, "here": true, "just": true, "now": true, "then": true,
	"so": true, "well": true, "today": true, "tomorrow": true,
}

// cleanPlace reads a place out of a short message, conservatively:
// the text up to the first punctuation, a leading preposition or
// "the" dropped, one to four words, every word a plain word and
// none of them filler. Anything else reads as no place at all, and
// the caller asks or moves on rather than guessing.
func cleanPlace(raw string) string {
	t := strings.TrimSpace(raw)
	if i := strings.IndexAny(t, ",.;!?"); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	low := strings.ToLower(t)
	for _, pre := range []string{"to ", "in ", "at ", "the "} {
		if strings.HasPrefix(low, pre) {
			t = strings.TrimSpace(t[len(pre):])
			break
		}
	}
	words := strings.Fields(t)
	if len(words) == 0 || len(words) > 4 {
		return ""
	}
	for _, w := range words {
		if !placeWordRe.MatchString(w) {
			return ""
		}
		lw := strings.ToLower(w)
		if askFillerWords[lw] || placeExtraFiller[lw] {
			return ""
		}
	}
	return strings.Join(words, " ")
}

// barePlace reads a message that is nothing but a place: at the
// choice question, a buyer who answers "where are you" with a bare
// capitalised name has answered delivery, to that place. The
// capital is the signal; a lower-case fragment is conversation,
// not a place, and never completes a sale on a guess.
func barePlace(msg string) string {
	loc := cleanPlace(msg)
	if loc == "" {
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(msg))
	if len(fields) == 0 || len(fields) > 3 {
		return ""
	}
	if r := []rune(fields[0]); r[0] < 'A' || r[0] > 'Z' {
		return ""
	}
	return loc
}

var statedLocationRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:i am|i'm|im)\s+in\s+([A-Za-z][A-Za-z \-]{1,30})`),
	regexp.MustCompile(`(?i)\bi\s+dey\s+([A-Za-z][A-Za-z \-]{1,30})`),
	regexp.MustCompile(`(?i)\b(?:based|located)\s+in\s+([A-Za-z][A-Za-z \-]{1,30})`),
	regexp.MustCompile(`(?i)\bi\s+live\s+in\s+([A-Za-z][A-Za-z \-]{1,30})`),
	regexp.MustCompile(`(?i)\bmy\s+location\s+is\s+([A-Za-z][A-Za-z \-]{1,30})`),
}

// extractStatedLocation reads the place from a plain statement of
// where the buyer is ("I am in Koforidua", "I dey Accra", "based
// in Osu"). It is used wherever she says it, browsing, haggling or
// at the fulfilment questions, and the session keeps it.
func extractStatedLocation(msg string) string {
	for _, re := range statedLocationRes {
		if m := re.FindStringSubmatch(msg); m != nil {
			if loc := cleanPlace(m[1]); loc != "" {
				return loc
			}
		}
	}
	return ""
}

var deliveryToRe = regexp.MustCompile(`(?i)\bto\s+([A-Za-z][A-Za-z \-]{1,30})`)

// extractDeliveryPlace reads where delivery should go: a stated
// location, or the place after "to" in a sending message ("send
// it to Koforidua"). "Send it to your station" reads as no place:
// your is filler, so the question is asked instead.
func extractDeliveryPlace(msg string) string {
	if loc := extractStatedLocation(msg); loc != "" {
		return loc
	}
	if m := deliveryToRe.FindStringSubmatch(msg); m != nil {
		return cleanPlace(m[1])
	}
	return ""
}

var meetPlaceRe = regexp.MustCompile(`(?i)\bmeet\b[^,.]*?\bat\s+([A-Za-z][A-Za-z \-]{1,30})`)

// extractMeetPlace reads the meeting place out of a meet proposal
// ("let's meet at Circle", "meet me at the station"). A leading
// "the" is part of how the place is said, so it is kept.
func extractMeetPlace(msg string) string {
	m := meetPlaceRe.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	place := cleanPlace(m[1])
	if place == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(m[1])), "the ") {
		return "the " + place
	}
	return place
}

var (
	sendPersonRe       = regexp.MustCompile(`(?i)\bsend\s+((?:my|a|the)\s+[A-Za-z]+(?:\s+[A-Z][a-z]+)?|somebody|someone)`)
	personWillComeRe   = regexp.MustCompile(`(?i)\b(my\s+(?:brother|sister|rider|friend|cousin|mother|father|driver|boy|husband|wife)(?:\s+[A-Z][a-z]+)?)\s+will\s+come`)
	somebodyWillComeRe = regexp.MustCompile(`(?i)\b(somebody|someone)\s+will\s+come`)
)

// sentSomebody reads a collector arrangement: the buyer sends
// another person for the goods. It reports the message was such an
// arrangement at all, and whether someone is actually named (a
// person word or a description, "my brother Kwame", "my rider");
// a bare "somebody" or "someone" is an arrangement with nobody
// named yet, and the follow-up asks who.
func sentSomebody(msg string) (who string, named bool, ok bool) {
	if somebodyWillComeRe.MatchString(msg) {
		return "", false, true
	}
	if m := personWillComeRe.FindStringSubmatch(msg); m != nil {
		return strings.TrimSpace(m[1]), true, true
	}
	if m := sendPersonRe.FindStringSubmatch(msg); m != nil {
		who = strings.TrimSpace(m[1])
		if lw := strings.ToLower(who); lw == "somebody" || lw == "someone" {
			return "", false, true
		}
		return who, true, true
	}
	return "", false, false
}

// cleanCollector reads who is coming out of a short answer to
// "Who is coming for it?": her words as given, up to six plain
// words ("my brother Kwame", "Kwame"). Anything shaped otherwise
// is no answer, and the question is asked again.
func cleanCollector(msg string) string {
	t := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(msg), ".!?"))
	words := strings.Fields(t)
	if len(words) == 0 || len(words) > 6 {
		return ""
	}
	for _, w := range words {
		if !placeWordRe.MatchString(w) {
			return ""
		}
	}
	return strings.Join(words, " ")
}

var (
	linkRe     = regexp.MustCompile(`(link|pay|checkout)`)
	yesRe      = regexp.MustCompile(`(yes|ok|okay|deal|send|take|agree)`)
	howMuchRe  = regexp.MustCompile(`(how much|price)`)
	intentRe   = regexp.MustCompile(`(how much|price|cost|offer|buy|interested|need|want|looking for|get me|sell me|give me)`)
	greetingRe = regexp.MustCompile(`^(hi|hello|hey|good (morning|afternoon|evening))\b`)
	// wantRe catches the bare want, the way buyers open in the real
	// selling groups: "tv needed", "I need one", "who sells bags".
	wantRe = regexp.MustCompile(`(needed|need|want|looking for|searching for|who sells|who has|any plug|a plug|plug for)`)
	// picRe catches the buyer's other first move: "send pic".
	picRe = regexp.MustCompile(`(\bpic\b|pics|photo|picture|image)`)
	// buyIntentRe catches commitment words with no price in them:
	// "I'll buy", "I'll take it", "I want to buy". Warm words, but
	// they are not an order; they open the explicit confirmation
	// step, and only a clear yes there closes.
	buyIntentRe = regexp.MustCompile(`(i['’]ll buy|i will buy|i['’]ll take|i will take|i want to buy|\btake it\b|\bbuy it\b)`)
	// deliveryRe catches the delivery question, which real buyers
	// ask before the item price as often as after it.
	deliveryRe = regexp.MustCompile(`(deliver|delivery|how do i get it|how will i receive|how do i receive|how will i get it|transport|\bbus\b)`)
	// totalRe catches the demand for one figure: "How much in
	// total?" A buyer should never have to add two numbers herself.
	totalRe = regexp.MustCompile(`\btotal\b`)
	// needRe catches the need-led opening: the buyer describes a
	// problem, not a product ("something to keep the moisture",
	// "something for my hair"), and lets the seller recommend.
	needRe = regexp.MustCompile(`(something (to|for|that)|need something|anything (to|for)|for my (hair|skin|face|body))`)
	// The availability frames: the standard buyer ask in this
	// market is a question, "do you have X", "have you got X",
	// "do you sell X", "is X available". None of them carries a
	// want word for wantRe to catch, so the asked-for thing is
	// pulled out of the frame itself (see unstockedAsk).
	doYouHaveRe   = regexp.MustCompile(`^do you have (?:got )?(.+)$`)
	haveYouGotRe  = regexp.MustCompile(`^have you got (.+)$`)
	doYouSellRe   = regexp.MustCompile(`^(?:do you sell|are you selling) (.+)$`)
	isAvailableRe = regexp.MustCompile(`^is (.+?) available$`)
	// The lane patterns: clear off-topic asks, the ones people put
	// to any AI they can reach — meanings, sums, public figures,
	// capitals, homework, jokes, the weather. Narrow on purpose,
	// and the lane block only ever tries them on a message with no
	// shop signal in it at all, so nothing the shop can answer is
	// ever steered away from the shop.
	arithmeticRe = regexp.MustCompile(`\d+\s*[-+*/x]\s*\d+`)
	offTopicRe   = regexp.MustCompile(`(who is|who was|who won|what is the meaning|what does .+ mean|meaning of|capital of|president of|prime minister of|how old is|tell me a joke|write (me )?(an? )?(essay|poem|story|letter|cv|code|program)|translate .+ (to|into)|what is the weather|weather (today|now)|news today)`)
)

// availabilityFramed reports whether the buyer asked in the
// availability frame ("is X available", "do you have X"): in the
// real groups a seller answers that question with the plain words
// "Still available", and the agent leads with them too.
func availabilityFramed(lower string) bool {
	text := strings.TrimRight(strings.TrimSpace(lower), ".?!")
	return isAvailableRe.MatchString(text) || doYouHaveRe.MatchString(text) ||
		haveYouGotRe.MatchString(text) || doYouSellRe.MatchString(text)
}

// laneStrike answers one clear off-topic turn, in the shape the
// merchant's money deserves: the first turn earns one short steer
// naming the agent's work, with the shelf offered; the second a
// briefer repeat; the third closes the lane politely. After that
// the buyer's off-topic turns are met with silence (see the lane
// block in HandleInbound) until she talks about the shop again.
// Nothing here lectures and nothing answers the question itself:
// answering even the easy ones teaches the whole market to come
// back with more, and every answer is bought with the merchant's
// money and spoken in her name.
func laneStrike(from string, s *Session, merchant *catalogue.Merchant) {
	s.OffTopic++
	switch {
	case s.OffTopic == 1:
		sendPieces(from,
			"That one is outside my work here. I sell for "+merchant.Name+": the goods, their prices and delivery.",
			"Tell me what you are after and I will sort you out.\n"+productListText(merchant.ID))
	case s.OffTopic == 2:
		whatsapp.SendText(from, "I only handle the shop here: what we sell, prices and delivery. Name an item and I will price it for you.")
	default:
		s.LaneClosed = true
		whatsapp.SendText(from, "I will be here when you need something from the shop.")
	}
}

// Inbound is one inbound WhatsApp message.
type Inbound struct {
	From      string
	Text      string
	ImageID   string
	Caption   string
	MessageID string
}

func lastN(msgs []model.Message, n int) []model.Message {
	if len(msgs) > n {
		return msgs[len(msgs)-n:]
	}
	return msgs
}

var (
	// The explicit entry token in a share link's prefilled text, in
	// its SHOP-EFUA and shop:efua spellings. The word after the
	// marker must resolve to a real shop code, or nothing is routed.
	shopTokenRe = regexp.MustCompile(`\bshop[-:\s]+([a-z0-9]+)\b`)
	// A bare shop code standing as a word of the message (what a
	// buyer types when she has the code but not the link).
	bareWordSplit = regexp.MustCompile(`[^a-z0-9]+`)
)

// askShopText is what a buyer with no resolved shop hears. It is
// deliberately all she hears: until a shop is resolved, nothing is
// sold, no catalogue is shown and no product is named, so the agent
// can never sell her one shop's goods under another shop's name.
const askShopText = "Hello, and welcome. I sell for many shops on this one number, so I need to know whose shop you have come for. Send me the shop code the seller shared with you, for example SHOP-EFUA, or tell me the shop name."

// parseShopEntry finds the shop a buyer's message enters, and what
// of the message is left once the entry marker is taken out. The
// marker is an explicit SHOP-<code> or shop:<code> token, or a bare
// code word matching a shop. When the buyer has no bound shop yet, a
// shop name in the text also resolves, as the fallback for a buyer
// who arrived with no link. An unresolvable marker resolves to no
// shop at all: routing is never a guess.
func parseShopEntry(lower string, allowName bool) (*catalogue.Merchant, string) {
	if m := shopTokenRe.FindStringSubmatchIndex(lower); m != nil {
		code := lower[m[2]:m[3]]
		if shop := catalogue.MerchantByShopCode(code); shop != nil {
			rest := strings.TrimSpace(lower[:m[0]] + " " + lower[m[1]:])
			return shop, rest
		}
		return nil, lower
	}
	for _, w := range bareWordSplit.Split(lower, -1) {
		if len(w) < 3 {
			continue
		}
		if shop := catalogue.MerchantByShopCode(w); shop != nil {
			rest := strings.TrimSpace(bareWordSplit.ReplaceAllString(
				strings.Replace(lower, w, " ", 1), " "))
			return shop, rest
		}
	}
	if allowName {
		if shop := catalogue.MerchantByShopName(lower); shop != nil {
			return shop, lower
		}
	}
	return nil, lower
}

// HandleInbound handles one inbound message.
//
// Routing comes before everything else, because everything else
// depends on whose shop this is:
//
//   - An owner (her number is her shop's owner number, or the legacy
//     env number for the demo shop) gets merchant mode for her shop.
//   - A buyer is bound to the shop her entry code names, stays bound
//     across her messages, and switches only when she sends another
//     shop's code, which resets her session in the old shop outright.
//   - A buyer with no shop resolved is asked for the code or the
//     shop name, and nothing is sold to her until one resolves.
//   - A sender who asks to sell starts merchant signup, which opens
//     her shop in chat and hands her into product onboarding.
func HandleInbound(in Inbound) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	from := in.From

	// Photo intake, routed by shop context like everything else.
	// A shop owner's photo is a throw-in: it opens onboarding from
	// the photo and whatever words came with it (see photo.go).
	// A buyer's photo in a shop she is bound to is read as her
	// payment claim or as a question the text shelf must answer.
	if in.ImageID != "" {
		if shop := ownerShopFor(from); shop != nil {
			startPhotoOnboarding(from, shop, in.ImageID, in.Caption)
			return
		}
		if shopID := bindings[from]; shopID != "" {
			if shop := catalogue.GetMerchant(shopID); shop != nil {
				// A buyer with an unpaid order in this shop who
				// sends a picture is sending her payment
				// screenshot: the direct path's claim, forwarded
				// to this shop's merchant for her word (see
				// payment.go). The media reference travels with
				// the claim; the picture alone settles nothing.
				if order := orders.Default.NewestUnpaidFor(shopID, from); order != nil {
					createClaim(from, shop, order, claimAmountOf(in.Caption), in.ImageID, true)
					return
				}
				if isClaimText(strings.ToLower(in.Caption)) {
					whatsapp.SendText(from, noOrderClaimText(shop))
					return
				}
				whatsapp.SendText(from, "Got your photo. I sell in text here at "+shop.Name+", and I cannot price or list goods from a photo yet. Tell me the name of the item you are asking about and I will help.")
				return
			}
		}
		whatsapp.SendText(from, askShopText)
		return
	}

	// The merchant herself gets merchant mode for her own shop.
	if shop := ownerShopFor(from); shop != nil {
		handleMerchantMessage(from, strings.TrimSpace(in.Text), shop.ID)
		return
	}

	msg := strings.TrimSpace(in.Text)
	lower := strings.ToLower(msg)

	// A signup already under way owns her words until it finishes or
	// she cancels.
	if pre, ok := sessions[sessionKey("", from)]; ok && pre.Signup != nil {
		advanceSignup(from, pre, msg, lower)
		return
	}

	// Shop entry and switching. A token for the shop she is already
	// in is just part of the message; a token for another shop moves
	// her, wiping the old shop's session completely so no haggle,
	// product or price can follow her across.
	boundID := bindings[from]
	if shop, rest := parseShopEntry(lower, boundID == ""); shop != nil {
		switch {
		case boundID == "":
			bindings[from] = shop.ID
			boundID = shop.ID
			lower = rest
		case boundID != shop.ID:
			old := catalogue.GetMerchant(boundID)
			delete(sessions, sessionKey(boundID, from))
			bindings[from] = shop.ID
			boundID = shop.ID
			lower = rest
			oldName := boundID
			if old != nil {
				oldName = old.Name
			}
			if rest == "" {
				whatsapp.SendText(from, "You have left "+oldName+". You are now at "+shop.Name+". "+productListText(shop.ID))
				return
			}
			whatsapp.SendText(from, "You have left "+oldName+". You are now at "+shop.Name+".")
		default:
			lower = rest
		}
	}

	// No shop resolved: the only other door is opening a shop of her
	// own; anything else gets the ask, and nothing is sold.
	if boundID == "" {
		if sellIntentRe.MatchString(lower) {
			startSignup(from)
			return
		}
		whatsapp.SendText(from, askShopText)
		return
	}

	// A bare entry (the link's prefilled text and nothing more) is a
	// knock on the shop's door: greet her by the shop's name. The
	// greeting goes as its own short message, then the goods, the
	// way a person types them, not as one block.
	if strings.TrimSpace(lower) == "" {
		if shop := catalogue.GetMerchant(boundID); shop != nil {
			sendPieces(from, "Welcome to "+shop.Name+".", productListText(shop.ID))
			return
		}
	}

	merchant := catalogue.GetMerchant(boundID)
	if merchant == nil {
		delete(bindings, from)
		whatsapp.SendText(from, askShopText)
		return
	}
	shopID := merchant.ID
	key := sessionKey(shopID, from)
	s, ok := sessions[key]
	if !ok {
		s = &Session{Stage: "browsing", MerchantID: shopID}
		sessions[key] = s
	}

	// A place she states anywhere in the chat is kept on her
	// session, so the fulfilment step never asks for it twice.
	if s.Location == "" {
		if loc := extractStatedLocation(msg); loc != "" {
			s.Location = loc
		}
	}

	// Paying for a sale already completed: choosing the direct
	// route to the merchant's own number, or claiming a direct
	// payment. Both attach to her own newest unpaid order in this
	// shop (payment.go), and run before the stage handling below:
	// a claim's words ("I have paid", "payment done") must never
	// be mistaken for a request to resend the link.
	if handleBuyerPayment(from, merchant, msg, lower) {
		return
	}

	// An agreed deal waiting on the buyer: resend the link on request.
	if s.Stage == "agreed" && linkRe.MatchString(lower) {
		whatsapp.SendText(from, "Here is your payment link: "+s.PaymentURL)
		return
	}

	// A price is agreed and the sale is waiting on how the goods
	// reach her: the fulfilment step owns her words until it
	// completes or she cancels.
	if s.Stage == "fulfilment" {
		handleFulfilment(from, s, merchant, msg, lower)
		return
	}

	// The confirmation step is open: the buyer said the warm words
	// ("I'll buy") and was quoted the price and asked plainly. Only
	// a clear yes here moves on, into the fulfilment step at the
	// quoted price; a no leaves the item on the shelf; anything
	// else is re-asked once, plainly, and then left open rather
	// than chased. A bare "ok" at THIS step is the clear yes,
	// because the question was explicit; everywhere else it is
	// only ever an acknowledgement. A restated figure is a yes
	// too: a buyer who answers by naming the quoted price ("I
	// will pay 165") has confirmed in her own words, and she is
	// not made to translate herself into a bare yes.
	if s.PendingClose {
		name := ""
		if s.Product != nil {
			name = s.Product.Name
		}
		switch {
		case isClearYes(lower):
			price := s.ClosePrice
			s.PendingClose = false
			beginFulfilment(from, s, price)
		case restatesQuotedPrice(msg, s, merchant):
			price := s.ClosePrice
			s.PendingClose = false
			beginFulfilment(from, s, price)
		case isClearNo(lower):
			clearPendingClose(s)
			whatsapp.SendText(from, "No problem. The "+name+" is here whenever you are ready.")
		case s.CloseReasked:
			clearPendingClose(s)
			whatsapp.SendText(from, "I will leave it there for now. The "+name+" is here whenever you are ready.")
		default:
			s.CloseReasked = true
			whatsapp.SendText(from, "Shall I send the payment link for the "+name+" at "+money.GHS(s.ClosePrice)+"? Say yes or no.")
		}
		return
	}

	// Delivery, and the one total. Real buyers ask about delivery
	// before the item price as often as after it, and they want one
	// figure, not two to add up. Asked while a price is in play, the
	// answer carries that total; the haggle state is untouched.
	if (s.Stage == "browsing" || s.Stage == "haggling") && parseOffer(lower) == nil &&
		(totalRe.MatchString(lower) || deliveryRe.MatchString(lower)) {
		if answerDeliveryOrTotal(from, s, merchant, lower) {
			return
		}
	}

	// The final stand: one price is on the table. Meet it, say the
	// word, or part as friends. The agent does not chase a losing sale.
	if s.Stage == "lastprice" {
		// A bare acknowledgement is never the word: "Ok" here means
		// she heard the price, not that she took it. The stand is
		// held up again, and nothing closes.
		if isBareAck(lower) {
			whatsapp.SendText(from, "My last price stands at "+money.GHS(s.Ask)+" for the "+s.Product.Name+". Say yes and I will send the payment link, or feel free to look around.")
			return
		}
		if totalRe.MatchString(lower) || deliveryRe.MatchString(lower) {
			if answerDeliveryOrTotal(from, s, merchant, lower) {
				return
			}
		}
		offer := parseOffer(lower)
		if offer != nil {
			if *offer >= s.Ask {
				beginFulfilment(from, s, s.Ask)
			} else {
				p := s.Product
				lastPrice := s.Ask
				daybook.Default.RecordWalkAway(shopID, daybook.Options{Allowance: merchant.DailyGiveawayAllowance})
				sessions[key] = &Session{Stage: "browsing", MerchantID: shopID}
				// The let-go carries the next step: the total with
				// delivery, and the link ready the moment she says
				// so. A bare "that is final" is where real chats go
				// silent and die.
				if fee := deliveryFeeOf(merchant); fee > 0 {
					next := "That is " + money.GHS(lastPrice+fee) + " with delivery, and I can send the payment link the moment you say so."
					if deliveryPaidToOf(merchant) == "carrier" {
						next = "That is " + money.GHS(lastPrice) + " with me, plus " + money.GHS(fee) + " delivery paid to " + carrierTarget(merchant.DeliveryNote) + " when it reaches you. I can send the payment link the moment you say so."
					}
					sendPieces(from,
						"No problem at all. "+money.GHS(*offer)+" would leave me at a loss on this one, so I will let you go with thanks. The "+p.Name+" is here at "+money.GHS(lastPrice)+" whenever you are ready.",
						next)
				} else {
					whatsapp.SendText(from, "No problem at all. "+money.GHS(*offer)+" would leave me at a loss on this one, so I will let you go with thanks. The "+p.Name+" is here at "+money.GHS(lastPrice)+" whenever you are ready.")
				}
			}
			return
		}
		if yesRe.MatchString(lower) {
			beginFulfilment(from, s, s.Ask)
			return
		}
		whatsapp.SendText(from, "My last price stands at "+money.GHS(s.Ask)+" for the "+s.Product.Name+". Say yes and I will send the payment link, or feel free to look around.")
		return
	}

	// Mid-haggle: a bare number (or "offer N") is the buyer's next move.
	if s.Stage == "haggling" {
		// A quantity phrase mid-haggle belongs to the measure
		// behaviour first: a new quantity restarts the haggle on
		// the scaled figures for it, and a quantity is never read
		// as the offer itself (see measure.go).
		if handleQuantity(from, s, merchant, msg, lower) {
			return
		}
		offer := parseOffer(lower)
		if offer != nil && !howMuchRe.MatchString(lower) {
			processOffer(from, s, merchant, *offer, msg)
			return
		}
		// Commitment words with no price in them ("I'll buy") move
		// to the confirmation step at the standing price: the buyer
		// is quoted the one total and asked plainly. Her "I'll buy"
		// alone closes nothing.
		if buyIntentRe.MatchString(lower) {
			startConfirmation(from, s, merchant, s.Product, s.Ask, false)
			return
		}
		// She is talking, not bidding. In the real groups a vague
		// turn ("any type") never ends the talk: the seller holds
		// the standing price up and keeps the door open. Only a
		// turn naming a different item leaves this haggle.
		other := catalogue.FindProduct(shopID, lower)
		if other == nil || (s.Product != nil && other.ID == s.Product.ID) {
			p := s.Product
			if picRe.MatchString(lower) {
				whatsapp.SendText(from, "I cannot send a photo of the "+p.Name+" from here, but it is this: "+p.Description+" The price stands at "+money.GHS(s.Ask)+". Give me your price and let us talk.")
				return
			}
			// The holding reply rotates: the same line twice in one
			// chat reads like a machine, so the wording moves while
			// the figure and the meaning stay exactly the same.
			holds := []string{
				"The " + p.Name + " stands at " + money.GHS(s.Ask) + ". Give me your price and let us see.",
				"We are still on the " + p.Name + " at " + money.GHS(s.Ask) + ". What price works for you?",
				"The " + p.Name + " is at " + money.GHS(s.Ask) + " for now. Talk to me, give me your price.",
			}
			whatsapp.SendText(from, holds[s.Holds%len(holds)])
			s.Holds++
			return
		}
	}

	// A bare acknowledgement while browsing ("ok", "ayt", "noted"):
	// she heard us, nothing more. It settles nothing and invents no
	// sale; the door stays open with the item she was looking at, or
	// an invitation to name one. The wording rotates like the
	// holding reply's does.
	if s.Stage == "browsing" && isBareAck(lower) {
		var variants []string
		if s.LastViewed != nil {
			p := s.LastViewed
			variants = []string{
				"The " + p.Name + " is here at " + money.GHS(p.ListPrice) + " whenever you are ready.",
				"Still here: the " + p.Name + " at " + money.GHS(p.ListPrice) + ". Say the word when you want it.",
				"No rush at all. The " + p.Name + " stands at " + money.GHS(p.ListPrice) + ".",
			}
		} else {
			variants = []string{
				"No problem. Tell me what you are after and I will sort you out.",
				"Alright. What are you looking for today?",
				"Ok, I am here. Name the item and I will price it for you.",
			}
		}
		whatsapp.SendText(from, variants[s.Acks%len(variants)])
		s.Acks++
		return
	}

	// Quantities while browsing: a measured amount is quoted,
	// refused or clarified by the measure behaviour, and the
	// quantity she settles on is remembered for the offer that
	// follows (see measure.go).
	if s.Stage == "browsing" {
		if handleQuantity(from, s, merchant, msg, lower) {
			return
		}
	}

	// The lane. People put general questions to any AI they can
	// reach: what a word means, a sum, who the president is. This
	// agent works for the merchant and answers only about her
	// shop, so a clear off-topic turn is steered back (laneStrike)
	// and never answered itself. The check runs only when the
	// message carries no shop signal at all: anything naming a
	// product, a price, a want, a delivery or an availability ask
	// is the shop's business and is handled by the branches below,
	// never steered. A bare figure is not a shop signal here on
	// purpose: "what is 5 + 5" carries figures, but it is a sum,
	// not an offer, and the arithmetic pattern settles it.
	{
		_, namedAsk := unstockedAsk(lower)
		shopish := catalogue.FindProduct(shopID, lower) != nil ||
			intentRe.MatchString(lower) || howMuchRe.MatchString(lower) ||
			wantRe.MatchString(lower) || needRe.MatchString(lower) ||
			greetingRe.MatchString(lower) || buyIntentRe.MatchString(lower) ||
			picRe.MatchString(lower) || deliveryRe.MatchString(lower) ||
			totalRe.MatchString(lower) || namedAsk
		switch {
		case s.LaneClosed && !shopish && (parseOffer(lower) == nil || arithmeticRe.MatchString(lower) || offTopicRe.MatchString(lower)):
			// The lane is closed on this buyer: her off-topic
			// turns cost the merchant nothing, not even a reply.
			// A plain figure still falls through below: a number
			// after all that silence is a buyer trying to buy.
			return
		case !shopish && (arithmeticRe.MatchString(lower) || offTopicRe.MatchString(lower)):
			laneStrike(from, s, merchant)
			return
		case shopish || parseOffer(lower) != nil:
			// Talk about the shop reopens the lane and clears
			// the strikes, the way a returning customer is just
			// a customer again.
			s.OffTopic = 0
			s.LaneClosed = false
		}
	}

	// A buyer who jumps straight to an offer, the way people actually
	// talk: a price named in the same breath as an item, or a price
	// right after looking at one item, is an offer for that item. She
	// should never need the perfect question to be understood. A
	// price with no item in sight gets the human question back:
	// which item is it for.
	if s.Stage == "browsing" {
		if offer := parseOffer(lower); offer != nil {
			target := catalogue.FindProduct(shopID, lower)
			if target == nil {
				target = s.LastViewed
			}
			if target != nil {
				loc := s.Location
				// An offer right after a measured quote lands on
				// the quantity she was quoted, for that same
				// item: the haggle opens on the scaled figures.
				if s.Qty > 0 && s.LastViewed != nil && target.ID == s.LastViewed.ID && target.ByMeasure() {
					s = openHaggleQty(shopID, merchant, target, s.Qty)
				} else {
					s = openHaggle(shopID, merchant, target)
				}
				s.Location = loc // a fresh haggle never forgets where she is
				sessions[key] = s
				processOffer(from, s, merchant, *offer, msg)
				return
			}
			whatsapp.SendText(from, "I want to get you the right price. Which item is that offer for?\n"+productListText(shopID))
			return
		}
	}

	// Commitment words while browsing ("I'll buy", "I'll take it"),
	// naming an item or following one she just viewed: quote the one
	// total and ask plainly. The words themselves close nothing;
	// only her yes at the confirmation step does, at the list price.
	if s.Stage == "browsing" && buyIntentRe.MatchString(lower) {
		target := catalogue.FindProduct(shopID, lower)
		if target == nil {
			target = s.LastViewed
		}
		if target != nil {
			startConfirmation(from, s, merchant, target, target.ListPrice, true)
			return
		}
	}

	// "Send pic": in the real groups this is a buyer's first move
	// once an item has her eye. There is no photo to send from here,
	// so the agent says so plainly and puts the item and its price
	// in front of her instead of going blank.
	if picRe.MatchString(lower) {
		target := catalogue.FindProduct(shopID, lower)
		if target == nil {
			target = s.LastViewed
		}
		if target != nil {
			s.LastViewed = target
			whatsapp.SendText(from, "I cannot send a photo of the "+target.Name+" from here, but it is this: "+target.Description+" It goes for "+money.GHS(target.ListPrice)+". Make me an offer and let us see.")
			return
		}
	}

	// Price question or product mention: quote and open the haggle,
	// with the day book setting the posture for this customer.
	product := catalogue.FindProduct(shopID, lower)
	if product != nil && intentRe.MatchString(lower) {
		loc := s.Location
		s = openHaggle(shopID, merchant, product)
		s.Location = loc // a fresh haggle never forgets where she is
		sessions[key] = s
		whatsapp.SendText(from, product.Name+": "+product.Description+" The price is "+money.GHS(product.ListPrice)+". Make me an offer and let us see.")
		return
	}
	if product != nil {
		// Looking at a different item ends the old item's pending
		// quantity: a remembered measure belongs to what it was
		// quoted on, never to whatever she looks at next.
		if s.LastViewed == nil || s.LastViewed.ID != product.ID {
			s.Qty, s.Unit = 0, ""
		}
		s.LastViewed = product
		view := product.Name + ": " + product.Description + " It goes for " + money.GHS(product.ListPrice) + ". Want to make an offer?"
		if availabilityFramed(lower) {
			// She asked in the availability frame, so the
			// answer leads with the market's own plain words
			// for it, then the goods.
			sendPieces(from, "Still available.", view)
		} else {
			whatsapp.SendText(from, view)
		}
		return
	}

	// The need-led opening: the buyer describes a problem, not a
	// product ("something to keep the moisture", "you understand
	// what I mean right?"), and a real seller understands her,
	// recommends the one thing on her own shelf that answers it,
	// and anchors the price early. The recommendation is grounded
	// in this shop's catalogue only, by word overlap with the
	// goods' names and descriptions. Nothing matching means the
	// shelf is offered and the want is written down, as below.
	if needRe.MatchString(lower) {
		if rec := recommendForNeed(shopID, lower); rec != nil {
			s.LastViewed = rec
			sendPieces(from,
				"I understand what you mean.",
				"For that, the "+rec.Name+" is the one: "+rec.Description+" It goes for "+money.GHS(rec.ListPrice)+".")
			return
		}
		recordWantAndOfferShelf(from, s, msg)
		return
	}

	// The bare want naming nothing we sell: "I need a tv" in a shop
	// with no televisions. A real seller does not go blank at it;
	// she says what she does have and asks which one the buyer is
	// after. And she writes the want down: if it ever lands on her
	// shelf, this buyer hears about it (see recordWantAndOfferShelf).
	if wantRe.MatchString(lower) {
		recordWantAndOfferShelf(from, s, msg)
		return
	}

	if greetingRe.MatchString(lower) {
		sendPieces(from, "Welcome to "+merchant.Name+".", productListText(shopID))
		return
	}

	// The availability question naming something this shop does
	// not stock: "do you have a rice cooker?" It carries no want
	// word, so it used to fall past the want path into the generic
	// fallback and the want was never written down. It reaches
	// here only after the shelf has had its say (a stocked item is
	// answered as an item further up), so what it names is a want
	// like any other, and it is recorded the same way.
	if item, ok := unstockedAsk(lower); ok {
		if p := stockedByLooseName(shopID, item); p != nil {
			// She named something on the shelf in words the
			// stricter product matching missed ("bags" for the
			// beaded bag): answer the item itself, and never
			// write a want for goods already in the shop.
			s.LastViewed = p
			sendPieces(from, "Still available.", p.Name+": "+p.Description+" It goes for "+money.GHS(p.ListPrice)+". Want to make an offer?")
			return
		}
		recordWantAndOfferShelf(from, s, msg)
		return
	}

	whatsapp.SendText(from, "Tell me what you are after and I will sort you out.\n"+productListText(shopID))
}

// needStopWords are the filler words of a need description: they say
// how the buyer is asking, not what would answer her, so they never
// count toward a recommendation.
var needStopWords = map[string]bool{
	"need": true, "want": true, "something": true, "anything": true,
	"keep": true, "keeps": true, "with": true, "from": true,
	"that": true, "this": true, "have": true, "will": true,
	"your": true, "yours": true, "please": true, "looking": true,
	"give": true, "really": true, "just": true, "like": true,
	"what": true, "mean": true, "right": true, "understand": true,
}

var needWordSplit = regexp.MustCompile(`[^a-z0-9]+`)

// needWords tokenises text for recommendation matching: lower case,
// words of at least four letters, lightly stemmed (a trailing s
// dropped) and stripped of filler.
func needWords(text string) map[string]bool {
	out := map[string]bool{}
	for _, w := range needWordSplit.Split(strings.ToLower(text), -1) {
		if len(w) < 4 || needStopWords[w] {
			continue
		}
		out[strings.TrimSuffix(w, "s")] = true
	}
	return out
}

// recommendForNeed picks the one product on this shop's shelf whose
// name and description best answer the buyer's stated need, by word
// overlap. It returns nil when nothing on the shelf shares a single
// meaningful word with the need: the agent recommends only what it
// can ground, and never reaches into another shop.
func recommendForNeed(shopID, lower string) *catalogue.Product {
	need := needWords(lower)
	if len(need) == 0 {
		return nil
	}
	var best *catalogue.Product
	bestScore := 0
	for _, p := range catalogue.ListProducts(shopID) {
		words := needWords(p.Name + " " + p.Description)
		score := 0
		for w := range need {
			if words[w] {
				score++
			}
		}
		if score > bestScore {
			pp := p
			best = &pp
			bestScore = score
		}
	}
	return best
}

// restatesQuotedPrice reports whether the buyer's words name the
// very figure she was quoted at the confirmation step: the close
// price itself, or, where the merchant takes the delivery money,
// the close price plus the delivery fee that was quoted beside
// it. Any other figure is not a confirmation: it is re-asked
// about, the way any other unclear answer is.
func restatesQuotedPrice(msg string, s *Session, merchant *catalogue.Merchant) bool {
	offer := parseOffer(msg)
	if offer == nil {
		return false
	}
	if *offer == s.ClosePrice {
		return true
	}
	if deliveryPaidToOf(merchant) == "merchant" {
		if fee := deliveryFeeOf(merchant); fee > 0 && *offer == s.ClosePrice+fee {
			return true
		}
	}
	return false
}

// clearPendingClose ends an open confirmation question without a
// sale: the item goes back to being the one she was viewing, and no
// haggle product is left sitting on a browsing session.
func clearPendingClose(s *Session) {
	s.PendingClose = false
	s.CloseReasked = false
	if s.CloseFromView && s.Product != nil {
		// The view goes back to the item as listed: a measured
		// close borrowed a scaled copy, and the shelf must never
		// quote the amount's price as the item's afterwards. A
		// close borrowed from a live haggle keeps the haggle, and
		// its quantity, exactly as they were.
		if s.Qty > 0 {
			s.LastViewed = wholeProductOf(s.MerchantID, s.Product)
		} else {
			s.LastViewed = s.Product
		}
		s.Product = nil
		s.Qty, s.Unit = 0, ""
	}
	s.CloseFromView = false
}

// startConfirmation opens the explicit confirmation step for a buyer
// who has said the warm words without a price agreement: she is
// quoted the price (with the delivery fee named as what it is, a
// charge that only applies if the goods are sent) and asked
// plainly. No final total is promised here, because how the goods
// reach her is not settled yet; only her clear yes at this step
// moves on, into the fulfilment step, where the total is settled.
func startConfirmation(from string, s *Session, merchant *catalogue.Merchant, product *catalogue.Product, price float64, fromView bool) {
	s.Product = product
	s.PendingClose = true
	s.ClosePrice = price
	s.CloseReasked = false
	s.CloseFromView = fromView
	sessions[sessionKey(s.MerchantID, from)] = s
	// A measured close quotes the amount, not the whole item:
	// the session's quantity names it ("3 yards of ...").
	name := product.Name
	if s.Qty > 0 && product.Unit != "" {
		name = qtyLabel(product, s.Qty)
	}
	if fee := deliveryFeeOf(merchant); fee > 0 {
		if deliveryPaidToOf(merchant) == "carrier" {
			// The fee is not hers to pay here at all: it goes
			// to whoever brings the item, when it arrives.
			sendPieces(from,
				"The "+name+" is "+money.GHS(price)+". If you want it sent, delivery is "+money.GHS(fee)+", paid to "+carrierTarget(merchant.DeliveryNote)+" when it reaches you.",
				"Shall I send the payment link?")
			return
		}
		sendPieces(from,
			"The "+name+" is "+money.GHS(price)+", plus "+money.GHS(fee)+" delivery if you want it sent to you.",
			"Shall I send the payment link?")
		return
	}
	sendPieces(from,
		"The "+name+" is "+money.GHS(price)+".",
		"Shall I send the payment link?")
}

// answerDeliveryOrTotal answers a delivery question, or the demand
// for one total, for the item in play (the haggle's product at its
// standing price, else the item in view at its list price). It
// reports whether it answered; with no item in play it says nothing
// and the conversation continues elsewhere. Haggle state is never
// touched: asking about delivery does not move a price.
func answerDeliveryOrTotal(from string, s *Session, merchant *catalogue.Merchant, lower string) bool {
	p := catalogue.FindProduct(s.MerchantID, lower)
	named := p != nil
	if p == nil {
		p = s.Product
	}
	if p == nil {
		p = s.LastViewed
	}
	if p == nil {
		return false
	}
	if named {
		s.LastViewed = p
	}
	price := p.ListPrice
	if s.Product != nil && p.ID == s.Product.ID && s.Ask > 0 {
		price = s.Ask
	}
	fee := deliveryFeeOf(merchant)
	carrier := fee > 0 && deliveryPaidToOf(merchant) == "carrier"
	if totalRe.MatchString(lower) {
		switch {
		case carrier:
			// Both purses, plainly: what she pays here, what
			// she hands over on arrival, and the two together,
			// so she still gets one clear picture and never
			// has to add anything up herself.
			whatsapp.SendText(from, "With me the "+p.Name+" is "+money.GHS(price)+". Delivery is "+money.GHS(fee)+", paid to "+carrierTarget(merchant.DeliveryNote)+" when it reaches you, so "+money.GHS(price+fee)+" in all.")
		case fee > 0:
			whatsapp.SendText(from, "Your total is "+money.GHS(price+fee)+": the "+p.Name+" at "+money.GHS(price)+", plus "+money.GHS(fee)+" delivery.")
		default:
			whatsapp.SendText(from, "The "+p.Name+" is "+money.GHS(price)+". Delivery is arranged with the seller, so there is no fixed delivery fee on top.")
		}
		return true
	}
	if fee > 0 {
		if carrier {
			sendPieces(from, deliveryOptionLine(merchant, ""),
				"The "+p.Name+" itself is "+money.GHS(price)+" with me.")
			return true
		}
		line := "Delivery is " + money.GHS(fee) + "."
		if merchant.DeliveryNote != "" {
			line = merchant.DeliveryNote + " Delivery is " + money.GHS(fee) + "."
		}
		sendPieces(from, line,
			"So the "+p.Name+" comes to "+money.GHS(price+fee)+" in total.")
	} else {
		whatsapp.SendText(from, "Delivery is arranged with the seller directly, so there is no fixed fee here.")
	}
	return true
}

// askFillerWords say how a buyer is asking, not what she is
// asking for: articles, pronouns, question words, polite filler
// and shop talk (price, delivery, discount). A candidate made of
// nothing else names nothing: "do you have anything nice?" is a
// buyer thinking aloud, not a want to write down.
var askFillerWords = map[string]bool{
	"a": true, "an": true, "the": true, "any": true, "some": true,
	"anything": true, "something": true, "nothing": true, "everything": true,
	"it": true, "one": true, "ones": true, "this": true, "that": true,
	"me": true, "my": true, "you": true, "your": true, "please": true,
	"nice": true, "good": true, "cheap": true, "new": true, "quality": true,
	"price": true, "prices": true, "discount": true, "discounts": true,
	"delivery": true, "deliver": true, "location": true, "located": true,
	"stock": true, "much": true, "many": true,
	"how": true, "what": true, "when": true, "where": true, "why": true, "who": true,
	"do": true, "does": true, "is": true, "are": true, "can": true, "could": true,
	"have": true, "has": true, "got": true, "get": true, "sell": true,
	"selling": true, "available": true, "in": true, "on": true, "for": true,
	"of": true, "to": true, "at": true,
	"stuff": true, "thing": true, "things": true, "item": true, "items": true,
}

// itemNamed pulls the thing a buyer is asking for out of her
// words: what is left once the asking words are dropped. minLen
// is the shortest leftover word that still names a thing: inside
// an availability frame the frame itself does the signalling, so
// even "tv" counts; standing alone it takes a fuller word.
func itemNamed(candidate string, minLen int) (string, bool) {
	words := []string{}
	for _, w := range needWordSplit.Split(candidate, -1) {
		if w == "" || askFillerWords[w] {
			continue
		}
		words = append(words, w)
	}
	for _, w := range words {
		if len(w) >= minLen {
			return strings.Join(words, " "), true
		}
	}
	return "", false
}

// unstockedAsk reads an availability question, or a bare item
// name with a question mark ("Rice cooker?"), and returns the
// thing the buyer named, when she clearly named one. A frame
// that matches but names nothing specific ("do you have
// anything nice?") is decided by its candidate and goes no
// further: vague questions record no want.
func unstockedAsk(lower string) (string, bool) {
	text := strings.TrimRight(strings.TrimSpace(lower), ".?!")
	for _, re := range []*regexp.Regexp{doYouHaveRe, haveYouGotRe, doYouSellRe, isAvailableRe} {
		if m := re.FindStringSubmatch(text); m != nil {
			return itemNamed(m[1], 2)
		}
	}
	// A bare item name with a question mark is the same ask with
	// the frame worn off. It must be short, the way item names
	// are, and it must be about a thing, not about the seller: a
	// question carrying "you" is a question for the shop ("where
	// are you?"), never an item.
	if strings.HasSuffix(strings.TrimSpace(lower), "?") {
		fields := strings.Fields(text)
		if len(fields) == 0 || len(fields) > 4 {
			return "", false
		}
		for _, f := range fields {
			if f == "you" || f == "your" || f == "yours" {
				return "", false
			}
		}
		return itemNamed(text, 3)
	}
	return "", false
}

// stockedByLooseName resolves an asked-for thing against the
// shelf the way the merchant's own commands do, trying the plural
// as typed and then singular: close enough to answer the item
// itself rather than write a want for goods already in the shop.
func stockedByLooseName(shopID, item string) *catalogue.Product {
	if p := catalogue.FindByName(shopID, item); p != nil {
		return p
	}
	if strings.HasSuffix(item, "s") {
		return catalogue.FindByName(shopID, strings.TrimSuffix(item, "s"))
	}
	return nil
}

// recordWantAndOfferShelf handles a buyer asking for something this
// shop does not stock: the want is written into the shop's want book
// (she will be alerted if it ever lands; see package wants), and the
// shelf is offered back with a light question, the way a real seller
// answers. The offer wording rotates like the other repeated lines.
func recordWantAndOfferShelf(from string, s *Session, msg string) {
	wants.Default.Record(s.MerchantID, from, msg)
	shelfLines := []string{
		"Let me help you find it. Tell me which of these you are after.",
		"This is what is on the shelf just now. Which of these are you after?",
		"Have a look at these and tell me which one you want.",
	}
	prefix := shelfLines[s.Shelves%len(shelfLines)]
	s.Shelves++
	sendPieces(from,
		"I do not have that one here yet. The moment it lands, I will alert you on this number.",
		prefix+"\n"+productListText(s.MerchantID))
}

// alertWaitingBuyers tells every buyer whose recorded want a newly
// stocked product answers, once each, through the ordinary send path
// (so dry-run runs land in the outbox like every other message). The
// want book marks the wants alerted, so a second matching product
// never alerts the same want again.
func alertWaitingBuyers(merchantID string, product catalogue.Product) {
	shop := catalogue.GetMerchant(merchantID)
	if shop == nil {
		return
	}
	matched := wants.Default.MatchAndMark(merchantID, product.Name)
	told := map[string]bool{}
	for _, w := range matched {
		if told[w.Buyer] {
			continue
		}
		told[w.Buyer] = true
		whatsapp.SendText(w.Buyer, product.Name+" is back in stock at "+shop.Name+", "+money.GHS(product.ListPrice)+". Reply to buy.")
	}
}

// openHaggle starts a negotiation session for one product, with the
// day book setting the posture for this customer: how much of the
// day's giveaway allowance is left decides the operative floor and
// how far each concession steps.
func openHaggle(shopID string, merchant *catalogue.Merchant, product *catalogue.Product) *Session {
	day := daybook.Default.GetDayState(shopID, daybook.Options{Allowance: merchant.DailyGiveawayAllowance})
	ctx := daybook.HaggleContext(*product, day)
	variation := 0.95 + rand.Float64()*0.1 // a trader's feel varies a little
	return &Session{
		Stage:      "haggling",
		MerchantID: shopID,
		Product:    product,
		Ask:        product.ListPrice,
		Round:      0,
		Floor:      ctx.Floor,
		StepScale:  ctx.StepScale * variation,
	}
}

// processOffer runs one buyer offer through the negotiation: the
// model proposes (or the rule engine decides), haggle verification
// bounds the price, and the outcome is an acceptance, a counter, or
// the final stand after which the agent holds its last price.
func processOffer(from string, s *Session, merchant *catalogue.Merchant, offer float64, msg string) {
	key := sessionKey(s.MerchantID, from)
	p := s.Product
	s.History = lastN(append(append([]model.Message{}, s.History...), model.Message{Role: "user", Content: msg}), 12)
	result := negotiateStep(s, offer, merchant)
	if result.Type == "accept" {
		beginFulfilment(from, s, result.Price)
		return
	}
	s.Ask = result.Price
	s.Round++
	if result.LastPrice || s.Round >= MaxCounters {
		s.Stage = "lastprice"
		sessions[key] = s
		// The final stand carries its next step as its own short
		// piece: the total with delivery, and the link ready now.
		// A bare "that is my final price" is exactly where real
		// chats end in silence.
		stand := "I have come down as far as I can today. My last price for the " + p.Name + " is " + money.GHS(s.Ask) + ". Say the word and I will send the payment link, no pressure at all."
		if fee := deliveryFeeOf(merchant); fee > 0 {
			next := "With delivery, the total is " + money.GHS(s.Ask+fee) + ". I can send the payment link now, whenever you say."
			if deliveryPaidToOf(merchant) == "carrier" {
				next = "It is " + money.GHS(s.Ask) + " with me, plus " + money.GHS(fee) + " delivery paid to " + carrierTarget(merchant.DeliveryNote) + " when it reaches you. I can send the payment link now, whenever you say."
			}
			sendPieces(from, stand, next)
		} else {
			whatsapp.SendText(from, stand)
		}
		return
	}
	text := ""
	if result.Message != nil {
		text = *result.Message
	} else {
		text = persona.FrameCounter(p.ListPrice, result.Price, s.itemLabel()) + " What do you say?"
	}
	s.History = lastN(append(append([]model.Message{}, s.History...), model.Message{Role: "assistant", Content: text}), 12)
	sessions[key] = s
	whatsapp.SendText(from, text)
}
