package agent

// Details forms: WhatsApp Flows are the platform's own
// slide-up form panels — a buyer fills her delivery details
// once, in one screen, instead of answering four questions
// in a row; a merchant settles her payout number the same
// way. The form is always an offer beside the chat, never
// a gate in front of it: every field it collects can still
// be typed, and the conversation reads typed answers
// exactly as before (the free-input rule — the panel is a
// shortcut for fingers, not a constraint on words).
//
// A form's completion returns in the webhook carrying the
// token the agent issued when it opened the form, and the
// answers are only ever read against that token, in the
// session that issued it: a stale form, or one finished
// against another conversation, fills nothing.
//
// The forms themselves live on the WhatsApp account (their
// screens are defined in scripts/flows/ and created there
// once); the agent knows them only by the ids its
// configuration carries. With no id configured the offers
// simply never appear and chat collection runs unchanged —
// the feature is dark, not broken.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"regexp"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// The two forms, by the work they do.
const (
	flowKindDelivery = "delivery"
	flowKindPayout   = "payout"
)

// deliveryFlowID and payoutFlowID read the form ids from
// the agent's configuration: the forms are account assets,
// created once on the WhatsApp account, and their ids are
// deployment facts, not code.
func deliveryFlowID() string { return os.Getenv("STALL_FLOW_DELIVERY_ID") }
func payoutFlowID() string   { return os.Getenv("STALL_FLOW_PAYOUT_ID") }

// newFlowToken mints the token a form carries out and its
// completion carries back: unguessable, and kind-prefixed
// so a completion names its own errand.
func newFlowToken(kind string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return kind + "-" + hex.EncodeToString(b[:])
}

// offerDeliveryDetailsFlow puts the delivery-details form
// beside the package-name question, for a fulfilment that
// travels: the name, the number the rider calls, the town
// and the landmark, in one panel instead of a walk. It is
// called from the one place that question is asked, so
// chat and form can never both be opened by accident —
// and whichever she answers first, the other simply
// stands down.
func offerDeliveryDetailsFlow(from string, s *Session) {
	id := deliveryFlowID()
	if id == "" {
		return
	}
	switch s.PendingFulfil.Mode {
	case "delivery", "arranged", "meet":
	default:
		return
	}
	token := newFlowToken(flowKindDelivery)
	s.PendingFlow = token
	s.FlowKind = flowKindDelivery
	sessions.put(sessionKey(s.MerchantID, from), s)
	whatsapp.SendFlow(from, "Or fill them all at once: the name for the package, the number the rider can call, and where it is going.", "Fill your details", id, token, "DETAILS")
}

// payoutDetailsRe reads the merchant asking to set or
// change where her money settles, in the desk's words.
var payoutDetailsRe = regexp.MustCompile(`\bpayout details\b|\b(change|update|set)\s+(my\s+)?payout\b`)

// offerPayoutDetailsFlow opens the merchant's payout form,
// or — with no form configured — answers with the payout
// as it stands, which is the fact she was reaching for.
func offerPayoutDetailsFlow(from string, s *Session, merchant *catalogue.Merchant) bool {
	id := payoutFlowID()
	if id == "" {
		whatsapp.SendText(from, "The payout form is not set up on this number yet. As it stands, your payout settles to "+checkout.LocalNumber(merchant.PayoutPhone)+" in the name "+merchant.PayoutHolderName+".")
		return true
	}
	token := newFlowToken(flowKindPayout)
	s.PendingFlow = token
	s.FlowKind = flowKindPayout
	sessions.put(sessionKey(merchant.ID, from), s)
	whatsapp.SendFlow(from, "This is where your sales settle. The name must be the name on the MoMo account itself, exactly as it is registered.", "Set my payout details", id, token, "PAYOUT")
	return true
}

// flowAnswers is a completed form's payload, as it returns
// in the webhook's response_json: the token it carried,
// and the fields of whichever form it was.
type flowAnswers struct {
	FlowToken string `json:"flow_token"`
	BuyerName string `json:"buyer_name"`
	Phone     string `json:"phone"`
	Town      string `json:"town"`
	Landmark  string `json:"landmark"`
	Holder    string `json:"holder"`
}

// handleFlowReply reads a completed form. The token is
// matched against the session that issued it — the
// merchant's own session first (her payout form completes
// in merchant chat), then the buyer's in her bound shop —
// and an unmatched completion is let go quietly: it is
// either stale or not this conversation's business.
func handleFlowReply(from, raw string) {
	var a flowAnswers
	if err := json.Unmarshal([]byte(raw), &a); err != nil || a.FlowToken == "" {
		return
	}
	if shop := ownerShopFor(from); shop != nil {
		if s, ok := sessions.lookup(sessionKey(shop.ID, from)); ok && s.FlowKind == flowKindPayout && s.PendingFlow == a.FlowToken {
			s.PendingFlow, s.FlowKind = "", ""
			completePayoutFlow(from, s, shop, a)
			return
		}
	}
	if shopID := bindings.get(from); shopID != "" {
		if s, ok := sessions.lookup(sessionKey(shopID, from)); ok && s.FlowKind == flowKindDelivery && s.PendingFlow == a.FlowToken {
			s.PendingFlow, s.FlowKind = "", ""
			completeDeliveryFlow(from, s, a)
			return
		}
	}
}

// completeDeliveryFlow settles a buyer's delivery details
// onto the fulfilment they belong to: the name onto the
// package, the town onto the place (and her session's,
// where her words about places are kept), the rider's
// number and the landmark onto the fulfilment itself,
// where they ride into the order and the seller's notice.
// Mid-walk, it answers the package-name question the form
// was offered beside, and the walk moves on to its next
// question exactly as if she had typed the name.
func completeDeliveryFlow(from string, s *Session, a flowAnswers) {
	if s.PendingFulfil.Mode == "" {
		// The moment the form belonged to has passed — the
		// sale was cancelled or closed without it. Nothing
		// is attached to anything, and she is told so
		// plainly rather than thanked for a save that
		// never happened.
		whatsapp.SendText(from, "Thank you. Those details came in after that order had moved on, so I have not attached them to anything.")
		return
	}
	// The completion is bound to the state it was issued
	// into (the fifth audit): the form answers the
	// details questions, so it lands only while the walk
	// stands at those steps, or at the recap those
	// details feed. Past them — the fee wait, a closed
	// sale, a fresh browse — its moment has passed, and
	// nothing is attached.
	switch s.FulfilStep {
	case "pkgname", "pkgmessage", "confirm":
	default:
		whatsapp.SendText(from, "Thank you. Those details came in after that order had moved on, so I have not attached them to anything.")
		return
	}
	f := s.PendingFulfil
	if name := flatLine(strings.TrimSpace(a.BuyerName)); name != "" {
		f.PackageName = name
	}
	feeNote := ""
	if town := flatLine(strings.TrimSpace(a.Town)); town != "" {
		// A new town is a new delivery: the fee settled
		// for the old place does not price the new one
		// (the fourth audit — the form overwrote the
		// place and left the fee and the recap bound to
		// the place she had replaced). Where the shop
		// prices delivery itself, the fee rebinds from
		// its own terms for the new town, so the recap —
		// whenever it is composed or re-asked — speaks
		// for where the goods are actually going. Where
		// the shop quotes per order, the quote was for
		// the old place and is void: the fulfilment is
		// parked and a fresh quote opens for the new
		// town, pausing the sale at feewait exactly as a
		// typed correction does, rather than the order
		// settling quietly at the wrong place's price
		// behind a notification (the fifth audit).
		if f.Mode == "delivery" && !strings.EqualFold(town, strings.TrimSpace(f.Location)) {
			if merchant := catalogue.GetMerchant(s.MerchantID); merchant != nil {
				if deliveryStyleOf(merchant) == "quote" {
					f.Location = town
					s.Location = town
					f.Phone = strings.TrimSpace(a.Phone)
					f.Landmark = flatLine(strings.TrimSpace(a.Landmark))
					f.Fee = 0
					s.PendingFulfil = f
					s.FeePark = &f
					sessions.put(sessionKey(s.MerchantID, from), s)
					whatsapp.SendText(from, "Got them, thank you.")
					beginFeeQuote(from, s, merchant, town)
					return
				} else if fee := deliveryFeeFor(merchant, town); fee != f.Fee {
					f.Fee = fee
					feeNote = "Delivery to " + town + " is " + money.GHS(fee) + "."
				}
			}
		}
		f.Location = town
		s.Location = town
	}
	f.Phone = strings.TrimSpace(a.Phone)
	f.Landmark = flatLine(strings.TrimSpace(a.Landmark))
	s.PendingFulfil = f
	sessions.put(sessionKey(s.MerchantID, from), s)
	whatsapp.SendText(from, "Got them, thank you.")
	if feeNote != "" {
		whatsapp.SendText(from, feeNote)
	}
	if s.FulfilStep == "pkgname" {
		askPackageMessage(from, s)
	}
	if s.FulfilStep == "confirm" {
		// The recap she confirmed against has changed
		// under her: it is read back to her in full
		// before any yes can complete the sale, so the
		// yes she gives is for the order as it now
		// stands (the fifth audit).
		sendOrderRecap(from, s, s.PendingFulfil)
	}
}

// createSubaccountFn is the payment-account creation, a
// variable so tests can stand in for the provider.
var createSubaccountFn = checkout.CreateSubaccount

// completePayoutFlow changes a merchant's payout details.
// The order is the safety: the replacement payment account
// is created and verified FIRST, and only then do the
// number, the name and the account switch together, in
// one write. The order this replaces recorded the new
// number first and remade the account after, so a failed
// creation left the record showing the new number while
// payment links still routed money through the old
// account — a mismatch a shop's money could fall into.
// A failed replacement now leaves the old payout standing
// exactly as it was, and she is told so, with the old
// details named.
func completePayoutFlow(from string, s *Session, merchant *catalogue.Merchant, a flowAnswers) {
	phone := strings.TrimSpace(a.Phone)
	holder := flatLine(strings.TrimSpace(a.Holder))
	if phone == "" || holder == "" {
		whatsapp.SendText(from, "That form came back without the number or the name on the account, so nothing has changed.")
		return
	}
	still := "you have no payout account set up yet"
	if merchant.PayoutPhone != "" && merchant.PayoutHolderName != "" {
		still = "your payout still settles to " + checkout.LocalNumber(merchant.PayoutPhone) + " in the name " + merchant.PayoutHolderName
	} else if merchant.PayoutPhone != "" {
		still = "your payout still settles to " + checkout.LocalNumber(merchant.PayoutPhone)
	}
	code, ok := createSubaccountFn(merchant.Name, phone, holder)
	if !ok {
		whatsapp.SendText(from, "That did not go through, so nothing has changed: "+still+". Try the form again whenever you are ready.")
		return
	}
	// The account exists; switching the record is one
	// write of all three fields. If even that write fails,
	// the active payout is untouched — the new account
	// simply stands unused until she sends the form again.
	if err := catalogue.SetPayoutDetails(merchant.ID, phone, holder, code); err != nil {
		whatsapp.SendText(from, "That did not go through, so nothing has changed: "+still+". Try the form again whenever you are ready.")
		return
	}
	whatsapp.SendText(from, "Done. Your payout now settles to "+checkout.LocalNumber(phone)+" in the name "+holder+", usually the next day.")
}
