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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/valormarc/hackathon-agent/internal/checkout"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/insights"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/persona"
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
// BasketItem is one agreed line of a multi-item order in
// the making: the product as it stood when its price was
// settled (the scaled copy, for a sale by measure), the
// agreed price, its measure, and the size the buyer chose
// for it in the details.
type BasketItem struct {
	Product     *catalogue.Product
	AgreedPrice float64
	Qty         float64
	Unit        string
	Size        string
}

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

	// DiscoveryShown holds the results discovery last showed
	// this buyer (shop and product ids), so her next message
	// can be read as the pick it usually is.
	DiscoveryShown []ShownProduct

	// DiscoveryAsked marks that the unbound buyer has already
	// been offered the search once, so the no-model net varies
	// its line instead of repeating itself.
	DiscoveryAsked bool

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
	// "who" (a collector is coming, not yet named), "based" (a
	// shop with no delivery terms, asking where she is),
	// "pickupkind" (she tapped Pick up; who is coming is asked
	// with two more buttons), then "pkgname" and "pkgmessage":
	// the package details, collected after the choice (see
	// fulfilment.go). PendingFulfil holds the settled fulfilment
	// while those details are being collected. Location is the
	// place she has stated anywhere in the chat, kept so she is
	// never asked for it twice.
	FulfilStep    string
	Location      string
	PendingFulfil fulfilment

	// FeeQuotePlace is the delivery place whose fee is being
	// quoted by the seller in a "quote" shop, while the buyer
	// waits at the feewait step (see deliveryfees.go).
	FeeQuotePlace string

	// Budget is what this buyer said she can spend, carried
	// for this conversation only (see sales.go): a stated
	// context her recommendations and alternatives respect,
	// never a profile that outlives the chat.
	Budget float64

	// FeePark holds a repeat order's settled fulfilment
	// details across the wait for a fresh delivery fee quote,
	// so the fee is the only thing the repeat ever waits for
	// (see reorder.go and deliveryfees.go).
	FeePark *fulfilment

	// InHandOK records that the seller has confirmed she has
	// this sale's item in hand, when the sale needed that
	// confirmation before payment (see inhand.go).
	InHandOK bool

	// DisputeConfirmRef is the order whose claimed refund
	// this buyer is being asked to confirm or deny (see
	// disputes.go).
	DisputeConfirmRef string

	// SaleID identifies the sale currently being made on
	// this session: minted when its fulfilment begins and
	// recorded on the order it mints, so a completion can
	// never mint the same sale twice (see completeSale).
	SaleID string

	// YesNoQ is the one yes/no question currently armed on
	// buttons in this chat: which question it is (Kind and
	// Ref) and the exact buttons that were rendered for it.
	// An answer — tapped or typed — is consumed only by the
	// question it names (see askYesNo and routeYesNoTap).
	YesNoQ *YesNoQuestion

	// Basket holds the items already agreed in this same
	// order while the buyer haggles over one more: each was
	// settled on its own (its own haggle, its own draw on
	// the day's allowance) and they complete together, one
	// fulfilment, one payment. PendingAdd is a product she
	// has been asked to confirm adding, awaiting her yes or
	// no. SizeQueue lists whose size the details still owe:
	// basket indices, with -1 for the pending product.
	Basket     []BasketItem
	PendingAdd *catalogue.Product
	SizeQueue  []int

	// ShelfPicks holds the product ids a buyer has tapped
	// onto her selection from the shelf sheet, in the order
	// she picked them (see shelf.go). Before she taps Done
	// they are simply her picks, still changeable; after
	// Done they are the queue still waiting to be settled,
	// one by one, into this same order. ShelfSettling marks
	// that Done has been tapped and the settling is under
	// way. ShelfPage is the sheet she is looking at, so a
	// tap's answer reopens the same page she tapped from.
	ShelfPicks    []string
	ShelfSettling bool
	ShelfPage     int

	// TotalHand holds her picks as they stood when she
	// offered on all of them together (see totalhaggle.go):
	// while it is set, the session's haggle is over the
	// picks' combined price, the session's Product is the
	// hand wearing one coat, and an agreement apportions
	// the total back across these products before the
	// order is laid out. It is consumed at that agreement
	// and never outlives it.
	TotalHand []catalogue.Product

	// PendingHandOffer is a whole-hand figure she named
	// while the settling was already under way and some
	// items were already settled at agreed prices (see
	// totalhaggle.go): the agent has asked whether the
	// figure covers everything or only the rest, and the
	// figure waits here for her answer. It binds nothing
	// until she declares the scope herself.
	PendingHandOffer float64

	// RememberedFulfil is how a returning buyer's goods
	// reached her last time, held up at the fulfilment
	// step for her to confirm or change (see returning.go).
	RememberedFulfil fulfilment

	// Relay state: the merchant has asked this buyer a question
	// through the agent (see merchant_think.go), and her next
	// answer is owed to the merchant. RelayOrderRef is the order
	// the question is about; RelayShopID says whose buyer she
	// is. One open question at a time; a payment claim from her
	// answers it too, since the claim itself reaches the
	// merchant.
	RelayOrderRef string
	RelayShopID   string

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
	Onboarding *OnboardingFlow
	// BurstBatch holds the products a seller forwarded while
	// one listing was open, each read with its details and
	// waiting to be priced together (see burst.go).
	// BurstAwaiting marks that the batch has been presented
	// and her next figures belong to it.
	BurstBatch    []burstItem
	BurstAwaiting bool
	// BurstHeld holds burst items whose read price the
	// harvest itself doubted: they are never priced from
	// the batch, and open afterwards as ordinary gated
	// listings where she confirms the price herself.
	BurstHeld     []burstItem
	PendingRemove string
	// PendingRemoveID is the identity of the product the
	// removal question was armed on. The name alone is
	// not enough to act on: a product removed and
	// replaced under the same name while the question
	// stood open — or across a restart — is a different
	// product, and her yes was never about it (the fifth
	// audit).
	PendingRemoveID string

	// LastMerchantNote is the last substantive thing the agent
	// told the merchant in her own chat (today, the allowance
	// prompt that rides the day answer). Her "explain this"
	// points at it, and the thinking needs it in its facts to
	// explain the right thing instead of guessing.
	LastMerchantNote string

	// LastNoteMsgID is the WhatsApp id of the message that
	// carried LastMerchantNote, so a reply quoting that exact
	// message is recognised as pointing at it.
	LastNoteMsgID string

	// AllowanceNagDate is the day the allowance reminder
	// last rode the day answer for this merchant. The
	// reminder is worth hearing once; after that it is
	// noise, so it fires at most once a day, and never at
	// all once she has set an allowance herself.
	AllowanceNagDate string

	// PendingFlow is the token of a details form currently
	// open from this conversation, and FlowKind which form
	// it is ("delivery" in a buyer's chat, "payout" in the
	// merchant's). A completion is only ever read against
	// the token this session issued: a form finished
	// against another conversation, or long after its
	// moment passed, fills nothing here.
	PendingFlow string
	FlowKind    string

	// TouchedAt is when a turn last spoke into this
	// session; the snapshot pass evicts resting sessions
	// idle past the limit, so state cannot grow without
	// bound on a shared number.
	TouchedAt time.Time

	// Signup is a new merchant's open-a-shop flow, held on the
	// pre-shop session until her shop exists; then she gets a normal
	// owner session in her own shop.
	Signup *SignupFlow
}

var (
	// sessions is keyed shopID + "|" + sender phone (sessionKey).
	// Keying by shop as well as sender is the isolation rule: the
	// same person in two shops has two unrelated sessions. The
	// maps live behind lockedMap (see store.go): turns lock per
	// speaker, and the maps guard their own structure.
	sessions = newLockedMap[string, *Session]()
	// bindings records which shop each buyer is currently inside, so
	// her later messages, which carry no shop code, stay in the shop
	// her link brought her to until she enters another shop's code.
	bindings = newLockedMap[string, string]() // buyer phone -> shopID
	// doorShown records which unknown numbers have already been
	// offered the front door buttons, so the buttons greet a
	// stranger once, on her first hello, and never nag her again.
	doorShown = newLockedMap[string, bool]()
	// packageNames remembers, per shop and buyer, the name she
	// last put on a package, so the next order confirms it
	// instead of asking from nothing.
	packageNames = newLockedMap[string, string]()
)

// turnTokens holds each live turn's model budget token,
// keyed by the speaker, so the drafting and understanding
// calls a turn makes deep in the handlers charge their own
// turn and no other (see model.BeginTurn).
var turnTokens = newLockedMap[string, string]()

func turnTokenFor(from string) string {
	return turnTokens.get(from)
}

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
	// The env number is the demo shop's owner only while
	// the shop itself records none: the moment a shop has a
	// recorded owner, she alone holds merchant authority
	// over it, and the legacy number is just another
	// stranger (the same precondition merchantNotifyPhone
	// already applies to notices). Without it, a stale env
	// number silently keeps full authority — last prices,
	// payout changes — over a shop that has moved to a new
	// owner.
	if IsMerchant(from) {
		if demo := catalogue.GetMerchant(MerchantID); demo != nil && demo.OwnerWhatsApp == "" {
			return demo
		}
	}
	return nil
}

// merchantNotifyPhone is the number a shop's notices go to: the
// owner the shop itself records. The env number is the DEMO
// shop's owner and nobody else's: the second audit found an
// ownerless shop's payment notices — product, amount, reference,
// buyer number — landing on the demo owner's phone, a different
// merchant. The fallback is scoped to the demo shop exactly as
// the authority path already scopes it (see ownerShopFor).
// subaccountFor returns the Seev subaccount a shop's
// payment links may route through: the recorded code,
// only while the number it was minted for is still the
// shop's payout number. The binding is recorded at mint
// time (catalogue.SubaccountPhone); a record whose code
// and payout number disagree — a payout change that
// somehow landed without its new account — routes no
// money through the stale account, and the mismatch is
// logged where an operator will see it. A record with no
// binding recorded (older shops) stands as it always did.
func subaccountFor(merchant *catalogue.Merchant) string {
	if merchant == nil || merchant.SubaccountCode == "" {
		return ""
	}
	if merchant.SubaccountPhone != "" && merchant.PayoutPhone != "" &&
		checkout.LocalNumber(merchant.SubaccountPhone) != checkout.LocalNumber(merchant.PayoutPhone) {
		fmt.Fprintf(os.Stderr, "subaccount binding mismatch for shop %s: code minted for %s, payout is %s; links will not route through it\n",
			merchant.ID, checkout.LocalNumber(merchant.SubaccountPhone), checkout.LocalNumber(merchant.PayoutPhone))
		return ""
	}
	return merchant.SubaccountCode
}

func merchantNotifyPhone(merchant *catalogue.Merchant) string {
	if merchant != nil && merchant.OwnerWhatsApp != "" {
		return merchant.OwnerWhatsApp
	}
	if merchant != nil && merchant.ID == "demo" {
		return os.Getenv("MERCHANT_WHATSAPP")
	}
	return ""
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

// The yes/no buttons: whenever the agent's question is a
// two-option choice, the question rides on buttons instead
// of asking for typed words — a tap cannot be misread the
// way a word can, and it is less work for the person
// answering. Her typed yes and no keep working too.
//
// Every question is armed with its own identity (askYesNo):
// the buttons carry a per-question id, the session records
// which question is open, and an answer is consumed only by
// the question it names. A tap on an earlier question's
// buttons can never be executed against a question armed
// later — it names the old question, and the old question is
// gone. (The first version used two universal ids and let
// whichever confirmation a dispatcher reached first consume
// any tap; the 2026-10-30 audit showed a buyer's dispute tap
// and a merchant's in-hand tap both executed against the
// wrong question.)
const (
	ynYesID = "yn_yes" // legacy ids, honoured only for buttons
	ynNoID  = "yn_no"  // already delivered before per-question ids
)

// YesNoQuestion is one armed yes/no question: the flow that
// asked it (Kind), what it is about (Ref: an order reference,
// a product name, a buyer number), and the rendered question
// itself, kept so a stale tap can be answered with the
// question that is actually waiting.
type YesNoQuestion struct {
	ID   string
	Kind string
	Ref  string
	Body string
	Yes  string
	No   string
}

// askYesNo arms a yes/no question on the recipient's session
// and sends it on buttons that carry the question's own id.
// Arming replaces any earlier armed question: the buttons in
// front of the person are the current question, and the
// earlier question's state stays where its flow left it, to
// be re-asked by that flow if it is still owed.
func askYesNo(from string, s *Session, kind, ref, body, yesTitle, noTitle string) {
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// A question that cannot be given an identity must
		// not ride on buttons at all; ask it in plain words.
		whatsapp.SendText(from, body+" Reply yes or no.")
		return
	}
	q := &YesNoQuestion{
		ID:   hex.EncodeToString(raw[:]),
		Kind: kind,
		Ref:  ref,
		Body: body,
		Yes:  yesTitle,
		No:   noTitle,
	}
	s.YesNoQ = q
	sessions.put(sessionKey(s.MerchantID, from), s)
	sendArmedYesNo(from, q)
}

// sendArmedYesNo renders an armed question's buttons.
func sendArmedYesNo(from string, q *YesNoQuestion) {
	whatsapp.SendButtons(from, q.Body, []whatsapp.Button{
		{ID: "ynq_" + q.ID + "_y", Title: q.Yes},
		{ID: "ynq_" + q.ID + "_n", Title: q.No},
	})
}

// parseYesNoTap reads a per-question button id back into the
// question id it names and the answer it carries.
func parseYesNoTap(id string) (qid string, yes bool, ok bool) {
	if !strings.HasPrefix(id, "ynq_") {
		return "", false, false
	}
	rest := id[len("ynq_"):]
	if strings.HasSuffix(rest, "_y") {
		return rest[:len(rest)-2], true, true
	}
	if strings.HasSuffix(rest, "_n") {
		return rest[:len(rest)-2], false, true
	}
	return "", false, false
}

// yesNoFor reports whether a typed yes or no in this chat
// may be consumed by the named kind: yes when that kind is
// the armed question, or when no question is armed (the
// word then belongs to whatever state the flows hold, as it
// always did). A different armed question owns the answer.
func (s *Session) yesNoFor(kind string) bool {
	return s.YesNoQ == nil || s.YesNoQ.Kind == kind
}

// clearYesNo disarms the named kind's question once it has
// been answered, leaving any other armed question alone.
func (s *Session) clearYesNo(kind string) {
	if s.YesNoQ != nil && s.YesNoQ.Kind == kind {
		s.YesNoQ = nil
	}
}

// serveCloseTap settles the payment-link question with a
// tapped answer, exactly as the typed yes and no arms of the
// close branch do (see buyer.go).
func serveCloseTap(from string, s *Session, yes bool) {
	name := ""
	if s.Product != nil {
		name = s.Product.Name
	}
	if yes {
		price := s.ClosePrice
		s.PendingClose = false
		beginFulfilment(from, s, price)
		return
	}
	clearPendingClose(s)
	whatsapp.SendText(from, "No problem. The "+name+" is here whenever you are ready.")
}

// routeBuyerYesNoTap delivers a tap to the buyer question it
// names — and only to that question. A tap whose question is
// no longer the open one executes nothing: the question that
// is actually waiting is shown again instead.
func routeBuyerYesNoTap(from, buttonID string) {
	qid, yes, _ := parseYesNoTap(buttonID)
	shopID := bindings.get(from)
	if shopID == "" {
		return
	}
	s := sessions.get(sessionKey(shopID, from))
	if s == nil {
		return
	}
	q := s.YesNoQ
	if q == nil || q.ID != qid {
		if q != nil {
			sendArmedYesNo(from, q)
		}
		return
	}
	s.YesNoQ = nil
	merchant := catalogue.GetMerchant(shopID)
	if merchant == nil {
		return
	}
	word := "no"
	if yes {
		word = "yes"
	}
	switch q.Kind {
	case "add":
		if s.PendingAdd != nil && s.PendingAdd.Name == q.Ref {
			servePendingAdd(from, s, merchant, word, word)
		}
	case "close":
		if s.PendingClose && (s.Product == nil || s.Product.Name == q.Ref) {
			serveCloseTap(from, s, yes)
		}
	case "dispute":
		if s.DisputeConfirmRef == q.Ref {
			handleDisputeConfirm(from, s, merchant, word, word)
		}
	}
}

// routeMerchantYesNoTap is the merchant side of the same
// rule: her tap answers the question it was rendered on —
// a removal, an in-hand confirmation, a listing summary —
// and no other.
func routeMerchantYesNoTap(from string, shop *catalogue.Merchant, buttonID string) {
	qid, yes, _ := parseYesNoTap(buttonID)
	s := sessions.get(sessionKey(shop.ID, from))
	if s == nil {
		return
	}
	q := s.YesNoQ
	if q == nil || q.ID != qid {
		if q != nil {
			sendArmedYesNo(from, q)
		}
		return
	}
	s.YesNoQ = nil
	merchant := catalogue.GetMerchant(shop.ID)
	if merchant == nil {
		return
	}
	word := "no"
	if yes {
		word = "yes"
	}
	switch q.Kind {
	case "remove":
		if q.Ref != "" && s.PendingRemove == q.Ref {
			serveRemoveAnswer(from, s, shop.ID, word)
		}
	case "inhand":
		if req := oldestOpenInHand(shop.ID); req != nil && req.Buyer == q.Ref {
			handleMerchantInHand(from, merchant, word, word)
		}
	case "listing":
		if s.Onboarding != nil && s.Onboarding.Step == "confirm" && s.Onboarding.Name == q.Ref {
			if serveSellerWalkTurn(from, s, merchant, word, word) {
				return
			}
			advanceOnboarding(from, s, word, word)
		}
	}
}

// sendPayButton presents a payment link the way a buyer
// should meet it: a button she taps, with the amount she is
// about to pay on the button's face, never a raw link
// sitting in the chat. WhatsApp caps a button's words at 25
// characters; an amount too long for the button rides in
// the body instead, so the figure she pays is always in
// front of her before she taps.
func sendPayButton(to string, total float64, url string) {
	display := "Click to pay " + money.GHS(total)
	body := "Pay here to complete your order."
	if len([]rune(display)) > 25 {
		display = "Click to pay"
		body = "Pay here to complete your order: " + money.GHS(total) + "."
	}
	whatsapp.SendPayButton(to, body, display, url)
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
	answered := false
	if model.Enabled() {
		sys := persona.BuildMerchantPrompt(*merchant, day)
		reply := model.Chat([]model.Message{{Role: "user", Content: text}}, model.Options{System: &sys, Kind: model.KindDraft, Turn: turnTokenFor(from)})
		// This was the one drafting path with no figure
		// gate (second audit): the model's answer went out
		// verbatim, its grounding prompt-only. The answer
		// may carry only the day book's own figures — the
		// same set the verified fallback reports — and
		// anything else falls back to that report.
		left := day.Allowance - day.GivenAway
		if left < 0 {
			left = 0
		}
		// The day's figures verify in two pools (the
		// fifth audit): currency-marked figures against
		// the money alone, bare figures against money or
		// counts. Pooled in one set, a count of sales
		// licensed a GH₵ figure the day never saw.
		if reply != nil && voiceClean(*reply) && recapFiguresClean(*reply,
			[]float64{day.Revenue, day.GivenAway, day.Allowance, left},
			[]float64{float64(day.Sales), float64(day.WalkAways)}) {
			whatsapp.SendText(from, *reply)
			answered = true
		}
	}
	if !answered {
		whatsapp.SendText(from, "Here is today so far. "+insights.DayReportText(day)+" Ask me any time and I will pull the same figures for you.")
	}
	// A shop that never chose its daily giveaway allowance
	// hears its own figures back as GH₵0 of GH₵0, and its
	// agent has nothing to give away when buyers bargain.
	// The day answer is where she meets those figures, so
	// the reminder rides here, in plain words with the
	// command itself spelled out — under two disciplines:
	// it fires at most once a day (a reminder repeated on
	// every answer is noise, and she heard it the first
	// time), and it never fires at all once she has set an
	// allowance herself, zero included, because zero can be
	// her choice and a choice is settled.
	if !merchant.AllowanceSet && merchant.DailyGiveawayAllowance <= 0 {
		s, hasSession := sessions.lookup(sessionKey(merchantID, from))
		if !hasSession || s.AllowanceNagDate != daybook.TodayString() {
			note := "One thing: you have not set a daily giveaway allowance, so it stands at GH₵0 and I have nothing to give away when buyers bargain. To set it, say: set allowance 300."
			res := whatsapp.SendText(from, note)
			if hasSession {
				s.AllowanceNagDate = daybook.TodayString()
				s.LastMerchantNote = note
				s.LastNoteMsgID = res.MessageID
			}
		}
	}
}

func productListText(merchantID string) string {
	// The priced list is a buying surface wherever it is
	// sent (entry, fallbacks, clarifier, photo-match and
	// reorder fallbacks), so it is built from the stocked
	// shelf only (the seventh audit): a sold-out product
	// is never priced here or invited an offer on.
	parts := make([]string, 0)
	for _, p := range stockedProducts(merchantID) {
		parts = append(parts, p.Name+" - "+money.GHS(p.ListPrice))
	}
	return "Here is what we have:\n```\n" + strings.Join(parts, "\n") + "\n```\nAsk me about any item, or make me an offer."
}

// Inbound is one inbound WhatsApp message.
type Inbound struct {
	From      string
	Text      string
	ImageID   string
	Caption   string
	MessageID string
	// ButtonID is set when the message is a tap on a reply button
	// the agent sent (an interactive button_reply webhook), and
	// empty for every typed message.
	ButtonID string
	// ListID is set when the message is a tap on a row of a
	// list sheet the agent sent (an interactive list_reply
	// webhook): the row's id, which names what she picked.
	ListID string
	// FlowReply carries a completed form's answers, as the
	// raw response_json of an interactive nfm_reply webhook,
	// when the message is a WhatsApp Flow completion.
	FlowReply string
	// ContextID is the id of the message this one quotes,
	// when the sender replied with a quote, and ContextFrom
	// is who sent that quoted message. Together they tell the
	// agent what a message points at when its words alone do
	// not: a merchant quoting the allowance note is talking
	// about the note, whatever words she adds. The quote is
	// resolved against the message book at the door (see
	// quotes.go).
	ContextID   string
	ContextFrom string
}

// noteSpeakerWords remembers what the speaker said on this
// turn, whatever stage she is in. The record used to be
// kept only inside a haggle; reasoning starved everywhere
// else, because a read that cannot see her earlier words
// cannot tell what "that one", "it" or "the other one"
// points at.
func noteSpeakerWords(s *Session, msg string) {
	if s == nil || strings.TrimSpace(msg) == "" {
		return
	}
	s.History = lastN(append(append([]model.Message{}, s.History...), model.Message{Role: "user", Content: msg}), 12)
}

// recentSpeakerWords renders her earlier words in this chat
// (not the current message, which every read is given
// separately) as one fact line for a thinking step, oldest
// first, at most max of them.
func recentSpeakerWords(s *Session, max int) string {
	if s == nil {
		return ""
	}
	var words []string
	for _, m := range s.History {
		if m.Role == "user" {
			words = append(words, m.Content)
		}
	}
	if len(words) <= 1 {
		return ""
	}
	words = words[:len(words)-1]
	if len(words) > max {
		words = words[len(words)-max:]
	}
	return strings.Join(words, " | ")
}

func lastN(msgs []model.Message, n int) []model.Message {
	if len(msgs) > n {
		return msgs[len(msgs)-n:]
	}
	return msgs
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
// handleInboundTurn runs one delivery's turn. Delivery
// discipline — the redelivery drop, the durable inbox,
// the handled mark — belongs to HandleInbound (inbox.go),
// which is the only caller.
func handleInboundTurn(in Inbound) {
	// One lock per speaker, held for the turn: her own
	// messages can never race her conversation, and nobody
	// else's slow turn queues ahead of hers. Taken through
	// claimTurn so the cross lock's registry knows a turn
	// for this speaker stands (see store.go).
	defer claimTurn(in.From)()

	// Her message is read the moment the agent picks it up:
	// she sees the blue ticks, and the typing indicator runs
	// while this turn works, the way a person who has opened
	// the chat looks to the one waiting on her reply.
	whatsapp.MarkReadTyping(in.MessageID)

	// Every inbound message is one turn, and the turn's
	// model budget opens with it: one call to understand,
	// drafting on its own small budget (see model.BeginTurn).
	turnToken := model.BeginTurn()
	turnTokens.put(in.From, turnToken)
	defer func() {
		model.EndTurn(turnToken)
		turnTokens.del(in.From)
	}()

	from := in.From

	// The turn stamps the speaker's standing sessions, the
	// clock the idle eviction reads.
	sessions.rangeAll(func(key string, s *Session) bool {
		if s != nil && strings.HasSuffix(key, "|"+from) {
			s.TouchedAt = time.Now()
		}
		return true
	})

	// The message book: her message enters it under its own
	// id, so it can be quoted and resolved later, and the
	// message it quotes, if any, is resolved now, before any
	// board runs (see quotes.go). The resolved quote is a
	// fact of this turn only.
	recordInboundMsg(in)
	if q := resolveQuote(in); q != nil {
		turnQuotes.put(from, q)
	} else {
		turnQuotes.del(from)
	}
	defer turnQuotes.del(from)

	// Photo intake, routed by shop context like everything else.
	// A shop owner's photo is a throw-in: it opens onboarding from
	// the photo and whatever words came with it (see photo.go).
	// A buyer's photo in a shop she is bound to is read as a
	// payment screenshot she is showing, or as a question the
	// text shelf must answer.
	if in.ImageID != "" {
		if shop := ownerShopFor(from); shop != nil {
			startPhotoOnboarding(from, shop, in.ImageID, in.Caption)
			return
		}
		if shopID := bindings.get(from); shopID != "" {
			if shop := catalogue.GetMerchant(shopID); shop != nil {
				// A buyer with an unpaid order in this shop who
				// sends a picture is almost always showing a
				// payment screenshot. A screenshot confirms
				// nothing here: payment is only through the
				// button Stall sent for the order, and Stall
				// checks that itself the moment it lands. She
				// is told so plainly, and the button is sent
				// again for the order her picture points at
				// (quoted) or her newest unpaid one.
				order := orders.Default.NewestUnpaidFor(shopID, from)
				if qo := quotedUnpaidOrder(shopID, from); qo != nil {
					order = qo
				}
				if order != nil {
					whatsapp.SendText(from, "Thank you. A screenshot cannot confirm a payment here, so I have not marked anything paid. Payment for your order is only through the button I sent you, and I check it myself the moment it lands.")
					sendPayButton(from, order.Charged(), orderPaymentURL(order))
					return
				}
				if isClaimText(strings.ToLower(in.Caption)) {
					whatsapp.SendText(from, noOrderPaymentText(shop))
					return
				}
				// A buyer's photo with no payment behind it
				// is a reference: the thing she likes, shown
				// instead of described. It is read against
				// this shop's shelf and the closest match is
				// put in front of her (see photomatch.go).
				if handleBuyerPhotoMatch(from, shopID, in.ImageID, in.Caption) {
					return
				}
				whatsapp.SendText(from, "Got your photo. I sell in text here at "+shop.Name+", and I cannot price or list goods from a photo yet. Tell me the name of the item you are asking about and I will help.")
				return
			}
		}
		if handleBuyerPhotoMatch(from, "", in.ImageID, in.Caption) {
			return
		}
		whatsapp.SendText(from, askShopText)
		return
	}

	// A completed details form (see flows.go). The form was
	// opened from a conversation on one side of the counter
	// or the other, and its completion is matched to that
	// conversation by the token it carries — before the
	// mode dispatch below, because the merchant's own
	// payout form completes in her chat (which the merchant
	// branch would otherwise swallow as an empty message)
	// and the buyer's delivery form in hers.
	if in.FlowReply != "" {
		handleFlowReply(from, in.FlowReply)
		return
	}

	// The privacy promise is a lane of its own, ahead of the
	// merchant dispatch and every other reading of her words:
	// "delete my data" is never a note, a listing or a
	// browsing fallback (see privacy.go).
	if text := strings.TrimSpace(in.Text); text != "" && isDeleteMyDataAsk(text) {
		handleDeleteMyData(from)
		return
	}

	// The merchant herself gets merchant mode for her own shop.
	// A tap on a yes/no button in her chat is translated into
	// the word she would have typed, so a tap and a typed
	// answer can never diverge (see sendYesNo).
	if shop := ownerShopFor(from); shop != nil {
		// A tap on a per-question yes/no button answers the
		// question it names, and only that question.
		if _, _, isYesNoTap := parseYesNoTap(in.ButtonID); isYesNoTap {
			routeMerchantYesNoTap(from, shop, in.ButtonID)
			return
		}
		text := strings.TrimSpace(in.Text)
		switch in.ButtonID {
		case ynYesID:
			text = "yes"
		case ynNoID:
			text = "no"
		}
		handleMerchantMessageCtx(from, text, shop.ID, in.ContextID)
		return
	}

	// A tap on a shelf sheet row (see shelf.go). The sheet is
	// only ever sent to a buyer standing in a shop, so a tap
	// is read there; a tap with no shop behind it, or while
	// her own signup owns her attention, is let go quietly.
	if in.ListID != "" {
		if pre, ok := sessions.lookup(sessionKey("", from)); ok && pre.Signup != nil {
			return
		}
		routeShelfTap(from, in.ListID)
		return
	}

	// A tap on the front door buttons. The buttons are only ever
	// shown to a number the agent does not know, with no shop
	// resolved; a merchant never sees them (her number is her key,
	// handled above) and a buyer who arrived by a shop's link never
	// sees them either (her shop resolves below before any door is
	// offered). A tap while her own signup is under way changes
	// nothing: the signup owns her attention until it ends.
	if in.ButtonID != "" {
		if pre, ok := sessions.lookup(sessionKey("", from)); ok && pre.Signup != nil {
			return
		}
		// A tap on a per-question yes/no button answers the
		// question it names, and only that question.
		if _, _, isYesNoTap := parseYesNoTap(in.ButtonID); isYesNoTap {
			routeBuyerYesNoTap(from, in.ButtonID)
			return
		}
		switch {
		case in.ButtonID == "door_sell":
			startSignup(from)
			return
		case in.ButtonID == "door_buy":
			whatsapp.SendText(from, "Welcome in. If you have a shop code, send it, like SHOP-EFUA. If not, tell me what you are looking to buy and I will check every shop for it.")
			return
		case in.ButtonID == ynYesID:
			// A tap on a yes/no button (see sendYesNo): the
			// tap becomes the word she would have typed, and
			// the ordinary flow below reads it.
			in.Text = "yes"
		case in.ButtonID == ynNoID:
			in.Text = "no"
		case strings.HasPrefix(in.ButtonID, "ff_"):
			// A fulfilment tap (see fulfilment.go). Some taps are
			// whole answers in themselves; the rest are translated
			// into the words the buyer would have typed, and the
			// ordinary flow below reads them exactly as if she had.
			text, handled := routeFulfilmentTap(from, in.ButtonID)
			if handled {
				return
			}
			in.Text = text
		default:
			return
		}
	}

	msg := strings.TrimSpace(in.Text)
	lower := strings.ToLower(msg)

	// A signup already under way owns her words until it finishes or
	// she cancels.
	if pre, ok := sessions.lookup(sessionKey("", from)); ok && pre.Signup != nil {
		advanceSignup(from, pre, msg, lower)
		return
	}

	// Shop entry and switching. A token for the shop she is already
	// in is just part of the message; a token for another shop moves
	// her, wiping the old shop's session completely so no haggle,
	// product or price can follow her across.
	boundID := bindings.get(from)
	// A quoted order reference is not travel: EFUA-0001 names an
	// order at Efua's, and a buyer standing in another shop who
	// quotes it is checking that order, not moving house. Entry
	// parsing is skipped for such a message when she is already
	// bound somewhere, and the order check answers her where she
	// stands. Unbound, the reference's shop code still brings her
	// to the right shop's door, as any code does.
	quotingRef := boundID != "" && orderRefRe.MatchString(msg)
	wasBound := boundID
	if shop, rest := parseShopEntry(lower, boundID == ""); shop != nil && !quotingRef {
		switch {
		case boundID == "":
			bindings.put(from, shop.ID)
			boundID = shop.ID
			lower = rest
		case boundID != shop.ID:
			old := catalogue.GetMerchant(boundID)
			sessions.del(sessionKey(boundID, from))
			bindings.put(from, shop.ID)
			boundID = shop.ID
			lower = rest
			oldName := boundID
			if old != nil {
				oldName = old.Name
			}
			if rest == "" {
				whatsapp.SendText(from, "You have left "+oldName+". You are now at "+shop.Name+".")
				presentShelf(from, shop)
				return
			}
			whatsapp.SendText(from, "You have left "+oldName+". You are now at "+shop.Name+".")
		default:
			lower = rest
		}
		// Entry text that quotes the shop's own line back
		// (her discovery answer, pasted or quoted) selects
		// the product it names; it is not an offer, and no
		// deal is closed on the entry turn.
		if rest != "" {
			if p := entrySelection(shop, rest); p != nil {
				// A view, not a sale in progress: the
				// product rides in LastViewed only, the
				// shape every other view road uses. A
				// browsing session carrying a Product is
				// never admitted as resting, so this
				// class of one-off session stood exempt
				// from idle eviction forever (the fifth
				// audit).
				sessions.put(sessionKey(shop.ID, from), &Session{Stage: "browsing", MerchantID: shop.ID, LastViewed: p})
				if wasBound == "" {
					whatsapp.SendText(from, welcomeFor(shop, from))
				}
				sendProductView(from, p, " Want to make an offer?")
				return
			}
		}
	}

	// No shop resolved by word or link, but the order book may
	// still know her: a buyer whose session was lost to a
	// restart and whose order is still open is recovered from
	// the book before she can be greeted as a stranger.
	if boundID == "" {
		boundID = recoverBuyerFromOrders(from)
	}

	// No shop resolved: the only other door is opening a shop of her
	// own; anything else gets the ask, and nothing is sold. A bare
	// greeting from a number the agent has never greeted gets the
	// front door once, with its Buy and Sell buttons; every other
	// message gets the plain ask, exactly as before.
	if boundID == "" {
		if catalogue.AmbiguousShopName(lower) {
			// Her words name more than one shop: sending
			// her to the first would sell her another
			// merchant's goods. The shop's own code is
			// the unambiguous door, so she hears that.
			whatsapp.SendText(from, "More than one shop here matches that name, so a name alone cannot take you to the right one. Start with the shop's own code instead — it is in the link the shop shares — and I will take you straight there.")
			return
		}
		if sellIntentRe.MatchString(lower) {
			startSignup(from)
			return
		}
		if bareGreetingRe.MatchString(lower) && !doorShown.get(from) && !midDiscovery(from) {
			doorShown.put(from, true)
			whatsapp.SendButtons(from, doorText(), []whatsapp.Button{
				{ID: "door_buy", Title: "I came to buy"},
				{ID: "door_sell", Title: "I want to sell"},
			})
			return
		}
		handleUnboundBuyer(from, strings.TrimSpace(in.Text))
		return
	}

	// A bare entry (the link's prefilled text and nothing more) is a
	// knock on the shop's door: greet her by the shop's name. The
	// greeting goes as its own short message, then the goods, the
	// way a person types them, not as one block.
	if strings.TrimSpace(lower) == "" {
		if shop := catalogue.GetMerchant(boundID); shop != nil {
			whatsapp.SendText(from, welcomeFor(shop, from))
			presentShelf(from, shop)
			return
		}
	}

	handleBuyerTurn(from, boundID, msg, lower)
}
