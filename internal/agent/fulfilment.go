// The fulfilment stage: between a price agreement and the
// payment link, the buyer says how the goods reach her, and the
// sale completes only when that is settled.
package agent

import (
	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
	"regexp"
	"strings"
)

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
	// PackageName and Note are the package details, collected
	// after the choice above is settled: the name to write on
	// the package, and a message for the seller. Neither is ever
	// a button; they are hers to say (see beginPackageDetails).
	PackageName string
	Note        string
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
			beginPackageDetails(from, s, fulfilment{Mode: "arranged", Location: s.Location})
			return
		}
		s.FulfilStep = "based"
		sessions[key] = s
		sendPieces(from, deal, "Where are you based?")
		return
	}

	s.FulfilStep = "choice"
	sessions[key] = s
	// The choice itself is buttons now, so she taps instead of
	// composing a sentence: Delivery, Pick up, Meet up. Pick up
	// is two answers wearing one coat, so it splits into its own
	// pair when tapped (see routeFulfilmentTap). Her typed words
	// keep working exactly as before; the buttons ride in the one
	// message that carries the question, and the words of the
	// question itself are the same words she heard before.
	sendPieces(from, deal)
	ask := "How do you want to get it? " + deliveryOptionLine(merchant, s.Location) + " Or you can come and pick it up yourself, no delivery fee."
	if s.Location != "" {
		ask += " Which one?"
	} else {
		ask += " Which one, and where are you?"
	}
	whatsapp.SendButtons(from, ask, []whatsapp.Button{
		{ID: "ff_delivery", Title: "Delivery"},
		{ID: "ff_pickup", Title: "Pick up"},
		{ID: "ff_meet", Title: "Meet up"},
	})
}

// routeFulfilmentTap reads a tap on the fulfilment buttons. A tap
// that is a whole interaction in itself (Pick up, which must first
// split into herself or somebody she sends) is handled here and
// reports handled. Every other tap is translated into the words
// the buyer would have typed, and the ordinary fulfilment flow
// reads them, so a tap and a typed answer can never diverge.
func routeFulfilmentTap(from, buttonID string) (text string, handled bool) {
	shopID := bindings[from]
	if shopID == "" {
		return "", true
	}
	s, ok := sessions[sessionKey(shopID, from)]
	if !ok || s.Stage != "fulfilment" {
		return "", true
	}
	switch buttonID {
	case "ff_pickup":
		if s.FulfilStep != "choice" {
			return "", true
		}
		s.FulfilStep = "pickupkind"
		sessions[sessionKey(shopID, from)] = s
		whatsapp.SendButtons(from, "Who will come for it?", []whatsapp.Button{
			{ID: "ff_self", Title: "I will come myself"},
			{ID: "ff_other", Title: "Somebody will come"},
		})
		return "", true
	case "ff_delivery":
		return "deliver it", false
	case "ff_meet":
		return "meet", false
	case "ff_self":
		return "i will come myself", false
	case "ff_other":
		return "somebody will come", false
	}
	return "", true
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
	case "pickupkind":
		return "Who will come for it: yourself, or somebody you send?"
	case "pkgname":
		return "What name should go on the package?"
	case "pkgmessage":
		return "Any message for the seller about this order? Say it now, or say no."
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
	fee := deliveryFeeOf(merchant)

	// The package details are collected in words, never buttons,
	// and they are read before anything else in this stage: a
	// name or a note can carry any words at all ("meet me at the
	// gate" is a note, not a new choice), so the choice readings
	// below must never see them.
	if s.FulfilStep == "pkgname" || s.FulfilStep == "pkgmessage" {
		handlePackageDetails(from, s, msg, lower)
		return
	}

	if fulfilCancelRe.MatchString(lower) {
		dropPendingSale(from, s)
		return
	}

	complete := func(f fulfilment) {
		if (f.Mode == "delivery" || f.Mode == "arranged") && f.Location != "" && s.Location == "" {
			s.Location = f.Location
		}
		beginPackageDetails(from, s, f)
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
	case "pickupkind":
		// She tapped Pick up and was asked who is coming. The
		// collector shapes and her own pickup were already read
		// above, so what reaches here is a short answer to that
		// question: a name is a collector; anything else is
		// herself.
		if who := cleanCollector(msg); who != "" && !selfWordSet[lower] {
			complete(fulfilment{Mode: "pickup", Collector: who})
			return
		}
		complete(fulfilment{Mode: "pickup"})
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

// selfWordSet is how a buyer answers "who will come" with herself.
var selfWordSet = map[string]bool{
	"myself": true, "me": true, "i will": true, "i will come": true,
	"i will come myself": true, "i'll come": true, "i am coming": true,
}

// dropPendingSale ends the fulfilment stage with nothing ordered
// and nothing booked: those are written only when a sale
// completes. The view she keeps is the item as listed, not a
// measured amount's scaled copy.
func dropPendingSale(from string, s *Session) {
	shopID := s.MerchantID
	p := s.Product
	view := p
	if s.Qty > 0 {
		view = wholeProductOf(shopID, p)
	}
	sessions[sessionKey(shopID, from)] = &Session{Stage: "browsing", MerchantID: shopID, LastViewed: view, Location: s.Location}
	whatsapp.SendText(from, "No problem, I have dropped that sale. The "+p.Name+" is here whenever you are ready.")
}

// beginPackageDetails is where every settled fulfilment now goes
// before the sale completes. The choice of how the goods move was
// a tap or a sentence; the details that go on the package are
// collected in her own words, because no button can know them:
// the name to write on the package, and then any message she
// wants the seller to have with the order. A collector she sends
// has already given the package its name, so that question is
// skipped for her; everyone else is asked, with the name she used
// last time in this shop held up to confirm when there is one.
func beginPackageDetails(from string, s *Session, f fulfilment) {
	key := sessionKey(s.MerchantID, from)
	s.PendingFulfil = f
	if f.Collector != "" {
		f.PackageName = f.Collector
		s.PendingFulfil = f
		askPackageMessage(from, s)
		return
	}
	s.FulfilStep = "pkgname"
	sessions[key] = s
	if remembered := packageNames[key]; remembered != "" {
		whatsapp.SendText(from, "What name should go on the package? Last time it was "+remembered+". Say same, or give me the name.")
		return
	}
	whatsapp.SendText(from, "What name should go on the package?")
}

// askPackageMessage moves the details onto their second question.
func askPackageMessage(from string, s *Session) {
	s.FulfilStep = "pkgmessage"
	sessions[sessionKey(s.MerchantID, from)] = s
	whatsapp.SendText(from, "Any message for the seller about this order? Say it now, or say no.")
}

// detailsCancelRe is the narrow cancel read inside the details
// questions. The stage's wide cancel words are not safe here: "I
// don't want a message" must decline the note, not kill the sale.
var detailsCancelRe = regexp.MustCompile(`^(cancel|cancel it|forget it|drop it|drop the sale|leave it|leave the sale)[.!]?$`)

// handlePackageDetails reads the two details answers. A name is
// cleaned conservatively; anything that is not name-shaped is
// asked for again, once per message, and the sale waits. The
// message for the seller is optional and taken as said: a plain
// no (in its ordinary shapes) means no message, and everything
// else is her note, carried onto the order and into the
// merchant's notice word for word.
func handlePackageDetails(from string, s *Session, msg, lower string) {
	key := sessionKey(s.MerchantID, from)
	if detailsCancelRe.MatchString(lower) {
		dropPendingSale(from, s)
		return
	}
	f := s.PendingFulfil
	switch s.FulfilStep {
	case "pkgname":
		remembered := packageNames[key]
		switch {
		case remembered != "" && (lower == "same" || isClearYes(lower) || strings.EqualFold(strings.TrimSpace(msg), remembered)):
			f.PackageName = remembered
		default:
			name := cleanPackageName(msg)
			if name == "" {
				whatsapp.SendText(from, "What name should go on the package?")
				return
			}
			f.PackageName = name
		}
		s.PendingFulfil = f
		sessions[key] = s
		askPackageMessage(from, s)
	case "pkgmessage":
		if !isNoMessage(lower) {
			if note := cleanNote(msg); note != "" {
				f.Note = note
			}
		}
		packageNames[key] = f.PackageName
		s.PendingFulfil = fulfilment{}
		sessions[key] = s
		completeSale(from, s, s.AgreedPrice, f)
	}
}

// cleanPackageName reads the name for the package out of a short
// answer: one to five plain words, letters with the marks real
// names carry. A figure, a question or a sentence is no name, and
// the question is asked again rather than a guess being written
// on somebody's package.
func cleanPackageName(msg string) string {
	t := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(msg), ".!?"))
	if t == "" || strings.ContainsAny(t, "0123456789?@") {
		return ""
	}
	words := strings.Fields(t)
	if len(words) == 0 || len(words) > 5 {
		return ""
	}
	for _, w := range words {
		if !placeWordRe.MatchString(w) {
			return ""
		}
	}
	return t
}

// isNoMessage reads a plain decline of the optional message: no,
// none, nothing, and their ordinary kin.
func isNoMessage(lower string) bool {
	switch lower {
	case "no", "no.", "none", "nothing", "no message", "no thanks",
		"skip", "that's all", "that is all", "i am fine", "i'm fine":
		return true
	}
	return isClearNo(lower)
}

// cleanNote takes her message for the seller as said, trimmed of
// its edges and capped at a note's length.
func cleanNote(msg string) string {
	t := strings.TrimSpace(msg)
	if len(t) > 300 {
		t = strings.TrimSpace(t[:300])
	}
	return t
}
