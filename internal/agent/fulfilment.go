// The fulfilment stage: between a price agreement and the
// payment link, the buyer says how the goods reach her, and the
// sale completes only when that is settled.
package agent

import (
	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
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
	// Size is the buyer's chosen size when the product comes
	// in sizes, asked for in the same details or taken from
	// the one she labelled herself earlier in the chat.
	Size string
}

// shopHasDeliveryTerms reports whether the shop states its own
// delivery terms: a fee, a note, or both. Such a shop's buyers are
// offered the choice (delivery on those terms, or pickup free);
// a shop with neither is asked only where the buyer is, so her
// seller can arrange things with her. A shop that quotes its
// fees per order has terms too: it delivers, at a fee the
// seller names for each order (see deliveryfees.go).
func shopHasDeliveryTerms(merchant *catalogue.Merchant) bool {
	return merchant != nil && (merchant.DeliveryFee > 0 || strings.TrimSpace(merchant.DeliveryNote) != "" || deliveryStyleOf(merchant) == "quote")
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
	case deliveryStyleOf(merchant) == "quote":
		line = "Delivery is priced when you order: the shop quotes the fee for your place, and you see it inside your full total before you pay anything."
		if note != "" {
			line = note + ". " + line
		}
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
	if len(s.Basket) > 0 {
		deal += " That makes your order " + money.GHS(basketGoods(s)+price) + " in all."
	}

	// A returning buyer is recognised: when she has bought
	// here before and has not already said where she is this
	// time, last time's way of getting the goods is held up
	// for her to confirm or change, the way a seller who
	// knows her would ask. One question, and a "different"
	// lands on the ordinary choice below, untouched.
	if s.Location == "" {
		// A quote shop's delivery fee is named fresh for every
		// order, so last time's delivery is never held up as
		// this time's way: she chooses again, and the fee is
		// quoted again.
		if remembered, ok := rememberedFulfilment(shopID, from, merchant); ok && (remembered.Location != "" || remembered.Mode == "pickup") && !(deliveryStyleOf(merchant) == "quote" && remembered.Mode == "delivery") {
			s.RememberedFulfil = remembered
			s.FulfilStep = "usual"
			sessions[key] = s
			sendPieces(from, deal, usualQuestionText(remembered))
			return
		}
	}

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

	sendPieces(from, deal)
	sendChoiceQuestion(from, s, merchant)
}

// sendChoiceQuestion opens the fulfilment choice: the step
// is set, and the question goes out with its buttons. The
// choice itself is buttons now, so she taps instead of
// composing a sentence: Delivery, Pick up, Meet up. Pick up
// is two answers wearing one coat, so it splits into its
// own pair when tapped (see routeFulfilmentTap). Her typed
// words keep working exactly as before; the buttons ride in
// the one message that carries the question, and the words
// of the question itself are the same words she heard
// before.
func sendChoiceQuestion(from string, s *Session, merchant *catalogue.Merchant) {
	s.FulfilStep = "choice"
	sessions[sessionKey(s.MerchantID, from)] = s
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
	case "ff_yes":
		if s.FulfilStep != "confirm" {
			return "", true
		}
		return "yes", false
	case "ff_change":
		if s.FulfilStep != "confirm" {
			return "", true
		}
		return "change", false
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
	case "usual":
		return usualQuestionText(s.RememberedFulfil)
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
	case "feewait":
		return "I am still waiting for the shop to give the delivery fee. The moment it comes, I will show you your full total."
	case "feeexpired":
		return "The shop has not given the delivery fee yet. Say wait to keep waiting, or tell me if you would rather pick it up or meet up."
	case "size":
		return sizeQuestionText(s)
	case "pkgname":
		return "What name should go on the package?"
	case "pkgmessage":
		return "Any message for the seller about this order? Say it now, or say no."
	case "confirm":
		return "Please confirm your order, or tell me what to change."
	default:
		return "So, how do you want to get it: we send it, or you pick it up?"
	}
}

// reaskFulfilStep puts the stage's open question to her
// again after the intent gate has served a turn that was
// not an answer to it. At the confirm step the question is
// the recap itself.
func reaskFulfilStep(from string, s *Session) {
	if s.FulfilStep == "confirm" {
		sendOrderRecap(from, s, s.PendingFulfil)
		return
	}
	whatsapp.SendText(from, fulfilPrompt(s))
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

	// An add she has been asked to confirm outranks the open
	// question: her yes or no belongs to the add until she
	// gives it.
	if s.PendingAdd != nil {
		servePendingAdd(from, s, merchant, msg, lower)
		return
	}

	// The intent layer runs before any step reads her words:
	// the thinking reads what she means against the open
	// question, and two meanings outrank the question itself.
	// A picture intent is served as a picture ask and the
	// question is asked again; a cancel ends the sale. Her
	// words are never taken as a name, a place or a note when
	// she meant something else, and the sale does not advance
	// while she waits to see the goods.
	reading := thinkStageIntent(from, s, merchant, msg)
	stageIntent := reading.Intent
	switch stageIntent {
	case stageIntentPicture:
		whole := p
		if s.Qty > 0 {
			whole = wholeProductOf(shopID, p)
		}
		answerPictureAsk(from, merchant, whole)
		reaskFulfilStep(from, s)
		return
	case stageIntentCancel:
		dropPendingSale(from, s)
		return
	case stageIntentConfirm:
		if s.FulfilStep == "confirm" {
			f := s.PendingFulfil
			s.PendingFulfil = fulfilment{}
			sessions[key] = s
			completeSale(from, s, s.AgreedPrice, f)
			return
		}
	case stageIntentAsk:
		// She asked something about the sale: the thinking
		// answers it from the facts, every figure verified
		// against the sale's own numbers, and the open
		// question is held up again. A draft that cannot be
		// verified is not sent; the step machine below then
		// does what it can with her words, as before.
		if reading.Reply != "" && voiceClean(reading.Reply) &&
			figuresClean(reading.Reply, s.Product.ListPrice, s.AgreedPrice, merchant.DeliveryFee, s.AgreedPrice+merchant.DeliveryFee) {
			whatsapp.SendText(from, reading.Reply)
			reaskFulfilStep(from, s)
			return
		}
	}

	// An add wish is served before the open question reads
	// her words, but after the gate: the thinking names it
	// (verified against the shelf), or, with no model to
	// read, the product's own full name in her message does.
	// In the details the words belong to the details, so the
	// add is only read at the fulfilment choices themselves.
	// The returning buyer's same-or-different question: a
	// yes (or "same") takes last time's way whole, a no (or
	// "different") opens the ordinary choice, and anything
	// else is a fresh answer in her own words, read by the
	// ordinary choice readings below as this same message.
	if s.FulfilStep == "usual" {
		remembered := s.RememberedFulfil
		switch {
		case isClearYes(lower) || strings.Contains(lower, "same"):
			s.RememberedFulfil = fulfilment{}
			sessions[key] = s
			beginPackageDetails(from, s, remembered)
			return
		case isClearNo(lower) || strings.Contains(lower, "different"):
			s.RememberedFulfil = fulfilment{}
			sessions[key] = s
			if shopHasDeliveryTerms(merchant) {
				sendChoiceQuestion(from, s, merchant)
			} else {
				s.FulfilStep = "based"
				sessions[key] = s
				whatsapp.SendText(from, "Where are you based?")
			}
			return
		default:
			s.RememberedFulfil = fulfilment{}
			if shopHasDeliveryTerms(merchant) {
				s.FulfilStep = "choice"
			} else {
				s.FulfilStep = "based"
			}
			sessions[key] = s
		}
	}

	detailsStep := s.FulfilStep == "size" || s.FulfilStep == "pkgname" || s.FulfilStep == "pkgmessage" || s.FulfilStep == "confirm"
	if !detailsStep {
		if stageIntent == stageIntentAdd && reading.Item != "" {
			if offerAdd(from, s, merchant, catalogue.FindProduct(shopID, strings.ToLower(reading.Item))) {
				return
			}
		} else if candidate := namedBasketCandidate(s, msg); candidate != nil &&
			(stageIntent == "" || strings.Contains(lower, "add") || strings.Contains(lower, "plus") || strings.Contains(lower, "also")) {
			// No thinking to read her, or the thinking called
			// it an answer while her words carry an add and a
			// full product name: the confirming question is
			// what makes the structural read safe.
			if offerAdd(from, s, merchant, candidate) {
				return
			}
		}
		if stageIntent == stageIntentAdd {
			// The wish was read but no shelf product could be
			// verified behind it: hold the open question up
			// rather than let the step machine eat her words.
			reaskFulfilStep(from, s)
			return
		}
	}

	// The package details are collected in words, never buttons,
	// and they are read before anything else in this stage: a
	// name or a note can carry any words at all ("meet me at the
	// gate" is a note, not a new choice), so the choice readings
	// below must never see them.
	if detailsStep {
		handlePackageDetails(from, s, msg, lower, reading)
		return
	}

	// The thinking has already ruled on a call-off for this
	// turn; the older word net only speaks when it could not.
	if stageIntent == "" && fulfilCancelRe.MatchString(lower) {
		dropPendingSale(from, s)
		return
	}

	complete := func(f fulfilment) {
		if (f.Mode == "delivery" || f.Mode == "arranged") && f.Location != "" && s.Location == "" {
			s.Location = f.Location
		}
		beginPackageDetails(from, s, f)
	}

	// deliver settles a chosen delivery to a named place. In
	// a "set" shop the place's own fee is known the moment the
	// place is (see deliveryFeeFor), and the sale goes on to
	// its details. In a "quote" shop the fee is the seller's
	// to name for this order, so the sale pauses and asks her
	// (see deliveryfees.go).
	deliver := func(loc string) {
		if deliveryStyleOf(merchant) == "quote" {
			beginFeeQuote(from, s, merchant, loc)
			return
		}
		complete(fulfilment{Mode: "delivery", Fee: deliveryFeeFor(merchant, loc), Location: loc})
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
		deliver(loc)
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
	case "feewait":
		// She is waiting on the seller's fee. Changing how the
		// goods move was already read above (pickup, meet-up, a
		// collector); anything else is talk while she waits,
		// and the wait is held up again, never mistaken for a
		// place or a name.
		whatsapp.SendText(from, fulfilPrompt(s))
		return
	case "feeexpired":
		// The seller never answered in time, and she was
		// offered the ways out. Waiting is hers to choose: it
		// asks the seller again. The other ways were read
		// above; anything else holds the offer up again.
		if waitWordRe.MatchString(lower) {
			reopenFeeQuote(from, s, merchant)
			return
		}
		whatsapp.SendText(from, fulfilPrompt(s))
		return
	default: // "choice"
		// A question about the sale itself (the total, delivery)
		// is answered, and the fulfilment question is held up
		// again: the pending sale survives the drift. A picture
		// ask never reaches here; the intent gate serves it
		// above, with or without a question mark.
		if strings.HasSuffix(lower, "?") && (totalRe.MatchString(lower) || deliveryRe.MatchString(lower)) {
			answerDeliveryOrTotal(from, s, merchant, lower)
			whatsapp.SendText(from, fulfilPrompt(s))
			return
		}
		if deliveryIntentRe.MatchString(lower) {
			loc := extractDeliveryPlace(msg)
			if loc == "" {
				loc = s.Location
			}
			if loc != "" {
				deliver(loc)
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
			deliver(loc)
			return
		}
		whatsapp.SendText(from, fulfilPrompt(s))
	}
}

// waitWordRe reads a buyer choosing to keep waiting after a
// fee quote has timed out.
var waitWordRe = regexp.MustCompile(`(?i)^\s*(wait|i will wait|i'll wait|keep waiting|let me wait|waiting)\s*\.?\s*$`)

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
	cancelFeeQuote(shopID, from)
	sessions[sessionKey(shopID, from)] = &Session{Stage: "browsing", MerchantID: shopID, LastViewed: view, Location: s.Location, History: s.History}
	whatsapp.SendText(from, "Okay, cancelled. Tell me what else you need.")
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
	// The fulfilment is settled now, by whichever road got
	// here: an open fee quote for this buyer is finished
	// business, answered or not.
	cancelFeeQuote(s.MerchantID, from)
	s.PendingFulfil = f
	// A product that comes in sizes is not sold sizeless:
	// every sized line in the order owes its size here,
	// before the name. A size she already labelled in her
	// own words somewhere in this chat is held and read
	// back in the recap instead of asked again; the rest
	// are asked for one at a time, in basket order, the
	// pending product last.
	s.SizeQueue = nil
	spoken := recentSpeakerWords(s, 12)
	// A size settles three ways, freshest first: one she
	// labelled herself in this chat, the size she took for
	// the same product on her last paid order here, or her
	// answer to the question. The first two are read back
	// in the recap, where she can correct them.
	settleSize := func(p *catalogue.Product) string {
		if stated := statedSize(p, spoken); stated != "" {
			return stated
		}
		if remembered := lastSizeFor(s.MerchantID, from, p.Name); remembered != "" && matchListedSize(p, remembered) != "" {
			return remembered
		}
		return ""
	}
	for i := range s.Basket {
		it := &s.Basket[i]
		if len(it.Product.Sizes) == 0 || it.Size != "" {
			continue
		}
		if size := settleSize(it.Product); size != "" {
			it.Size = size
		} else {
			s.SizeQueue = append(s.SizeQueue, i)
		}
	}
	if f.Size == "" && s.Product != nil && len(s.Product.Sizes) > 0 {
		if size := settleSize(s.Product); size != "" {
			f.Size = size
			s.PendingFulfil = f
		} else {
			s.SizeQueue = append(s.SizeQueue, -1)
		}
	}
	if len(s.SizeQueue) > 0 {
		s.FulfilStep = "size"
		sessions[key] = s
		whatsapp.SendText(from, sizeQuestionText(s))
		return
	}
	beginPackageName(from, s, f)
}

// sizeTargetProduct is the product whose size the details
// are asking for now: the head of the size queue.
func sizeTargetProduct(s *Session) *catalogue.Product {
	if len(s.SizeQueue) == 0 {
		return s.Product
	}
	if idx := s.SizeQueue[0]; idx >= 0 && idx < len(s.Basket) {
		return s.Basket[idx].Product
	}
	return s.Product
}

// sizeQuestionText asks for the size at the head of the
// queue. A lone sized product keeps the plain question it
// has always had; in a basket each item is asked for by
// name, so she always knows which one she is sizing.
func sizeQuestionText(s *Session) string {
	target := sizeTargetProduct(s)
	if target == nil {
		return ""
	}
	if len(s.SizeQueue) == 1 && len(s.Basket) == 0 {
		return "What size do you take? They come in " + sizesLine(target) + "."
	}
	return "What size for the " + target.Name + "? They come in " + sizesLine(target) + "."
}

// rememberedPackageName is the name this buyer last put on
// a package at this shop: the running memory first, then the
// order book itself, so a restart or a deploy never costs
// her the name her last paid order already carries. A name
// recovered from the book warms the memory for next time.
func rememberedPackageName(shopID, from string) string {
	key := sessionKey(shopID, from)
	if n := packageNames[key]; n != "" {
		return n
	}
	bestAt, name := "", ""
	for _, o := range orders.Default.All() {
		if o.MerchantID != shopID || o.From != from || o.Status != "paid" || o.PackageName == "" || o.CreatedAt < bestAt {
			continue
		}
		bestAt, name = o.CreatedAt, o.PackageName
	}
	if name != "" {
		packageNames[key] = name
	}
	return name
}

// beginPackageName opens the name question of the details:
// a collector she sends has already given the package its
// name, so that question is skipped for her; everyone else
// is asked, with the name she used last time in this shop
// held up to confirm when there is one.
func beginPackageName(from string, s *Session, f fulfilment) {
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
	if remembered := rememberedPackageName(s.MerchantID, from); remembered != "" {
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

// A call-off wearing natural lead-ins ("What? Just forget
// it") is the intent layer's judgement to make, from the
// meaning; no word pattern here tries to read it, because a
// note carrying the same words onward ("leave it at the
// gate") must survive.

// matchOneSizedLine finds the one sized line of the pending
// order a text names a listed size for: a basket index, or
// -1 for the pending product itself. ok is false when the
// text fits no line, or more than one: a correction is only
// ever applied where it can land in exactly one place.
func matchOneSizedLine(s *Session, text string) (idx int, size string, ok bool) {
	idx, size = -2, ""
	for i := range s.Basket {
		if sz := matchListedSize(s.Basket[i].Product, text); sz != "" {
			if idx != -2 {
				return -2, "", false
			}
			idx, size = i, sz
		}
	}
	if s.Product != nil {
		if sz := matchListedSize(s.Product, text); sz != "" {
			if idx != -2 {
				return -2, "", false
			}
			idx, size = -1, sz
		}
	}
	if idx == -2 {
		return -2, "", false
	}
	return idx, size, true
}

// applyStatedCorrection lands a correction the buyer has
// already stated, at the confirm step, and reads the order
// back to her again. Two roads reach it: she quoted one of
// the sale's own questions and answered it (the quote itself
// says which part of the order her words belong to), or the
// stage reading named the part and the value she stated.
// Either way the value is verified here before it lands —
// a name must clean as a name, a place as a place, a size
// must be one of the listed sizes on exactly one line — and
// anything that cannot be verified is not applied: the
// caller's ordinary correction prompts then take the turn.
func applyStatedCorrection(from string, s *Session, f fulfilment, msg string, reading stageReading) bool {
	key := sessionKey(s.MerchantID, from)
	apply := func() bool {
		s.PendingFulfil = f
		sessions[key] = s
		sendOrderRecap(from, s, f)
		return true
	}
	// The quoted name question answered with a name: her
	// words are the new name for the package. A stage
	// reading that puts her turn on a different part of
	// the order outranks the anchor.
	quotedNameAnswer := turnQuote != nil && turnQuote.Known &&
		strings.HasPrefix(turnQuote.Body, "What name should go on the package?")
	if quotedNameAnswer && !(reading.Intent == stageIntentChange && reading.Field != "" && reading.Field != "name") {
		if name := cleanPackageName(msg); name != "" {
			f.PackageName = name
			return apply()
		}
	}
	if reading.Intent != stageIntentChange {
		return false
	}
	switch reading.Field {
	case "name":
		name := cleanPackageName(reading.Value)
		if name == "" {
			name = cleanPackageName(msg)
		}
		if name != "" {
			f.PackageName = name
			return apply()
		}
	case "note":
		if note := cleanNote(reading.Value); note != "" {
			f.Note = note
			return apply()
		}
	case "place":
		if f.Mode == "delivery" || f.Mode == "arranged" {
			if loc := cleanPlace(reading.Value); loc != "" {
				f.Location = loc
				return apply()
			}
		}
	case "size":
		if idx, size, ok := matchOneSizedLine(s, reading.Value); ok {
			if idx >= 0 {
				s.Basket[idx].Size = size
				return apply()
			}
			if size != f.Size {
				f.Size = size
				return apply()
			}
		}
	}
	return false
}

// handlePackageDetails reads the two details answers. A name is
// cleaned conservatively; anything that is not name-shaped is
// asked for again, once per message, and the sale waits. The
// message for the seller is optional and taken as said: a plain
// no (in its ordinary shapes) means no message, and everything
// else is her note, carried onto the order and into the
// merchant's notice word for word.
func handlePackageDetails(from string, s *Session, msg, lower string, reading stageReading) {
	key := sessionKey(s.MerchantID, from)
	// The intent layer has already ruled on this turn when
	// it could be read; the narrow word net below only
	// speaks for the bare forms on a day it could not.
	if reading.Intent == "" && detailsCancelRe.MatchString(lower) {
		dropPendingSale(from, s)
		return
	}
	f := s.PendingFulfil
	switch s.FulfilStep {
	case "size":
		// Her answer must be one of the sizes of the product
		// she is being asked about; anything else is asked
		// again with that list, and the sale waits.
		target := sizeTargetProduct(s)
		size := matchListedSize(target, msg)
		if size == "" {
			what := "Which size? They come in "
			if len(s.SizeQueue) > 1 || len(s.Basket) > 0 {
				what = "Which size for the " + target.Name + "? They come in "
			}
			whatsapp.SendText(from, what+sizesLine(target)+".")
			return
		}
		if len(s.SizeQueue) > 0 && s.SizeQueue[0] >= 0 {
			s.Basket[s.SizeQueue[0]].Size = size
		} else {
			f.Size = size
			s.PendingFulfil = f
		}
		s.SizeQueue = s.SizeQueue[1:]
		sessions[key] = s
		if len(s.SizeQueue) > 0 {
			whatsapp.SendText(from, sizeQuestionText(s))
			return
		}
		beginPackageName(from, s, f)
	case "pkgname":
		remembered := rememberedPackageName(s.MerchantID, from)
		switch {
		case remembered != "" && (lower == "same" || strings.HasPrefix(lower, "same ") || isClearYes(lower) || strings.EqualFold(strings.TrimSpace(msg), remembered)):
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
		s.PendingFulfil = f
		s.FulfilStep = "confirm"
		sessions[key] = s
		sendOrderRecap(from, s, f)
	case "confirm":
		// The last word before money moves is hers. A yes
		// completes the sale exactly as it always did; a
		// correction goes back to the step it belongs to, with
		// everything else she settled still held.
		if isClearYes(lower) {
			s.PendingFulfil = fulfilment{}
			sessions[key] = s
			completeSale(from, s, s.AgreedPrice, f)
			return
		}
		// A correction she has already stated is applied,
		// not asked about: the value she gave is verified
		// here and the order is read back to her again.
		if applyStatedCorrection(from, s, f, msg, reading) {
			return
		}
		if nameRe.MatchString(lower) {
			whatsapp.SendText(from, "No problem. Let us set the name right.")
			beginPackageDetails(from, s, f)
			return
		}
		if placeRe.MatchString(lower) {
			whatsapp.SendText(from, "No problem. Let us set that right.")
			beginFulfilment(from, s, s.AgreedPrice)
			return
		}
		if strings.Contains(lower, "size") && anySizedLine(s) {
			// A size correction lands where the size belongs:
			// when it fits exactly one sized line in the
			// order, that line takes it and the recap is read
			// again. Otherwise the pending product's size is
			// asked for afresh, as before.
			matched, matchedSize, single := matchOneSizedLine(s, msg)
			if single {
				switch {
				case matched >= 0:
					s.Basket[matched].Size = matchedSize
					sessions[key] = s
					sendOrderRecap(from, s, f)
					return
				case matchedSize != f.Size:
					f.Size = matchedSize
					s.PendingFulfil = f
					sessions[key] = s
					sendOrderRecap(from, s, f)
					return
				}
			}
			if s.Product != nil && len(s.Product.Sizes) > 0 {
				s.SizeQueue = []int{-1}
				s.FulfilStep = "size"
				s.PendingFulfil = f
				sessions[key] = s
				whatsapp.SendText(from, "What size do you take? They come in "+sizesLine(s.Product)+".")
				return
			}
		}
		changeLine := "Tell me what to change: the name on the package, or how and where the goods reach you. Or say yes and I will send your payment link."
		if anySizedLine(s) {
			changeLine = "Tell me what to change: the size, the name on the package, or how and where the goods reach you. Or say yes and I will send your payment link."
		}
		whatsapp.SendText(from, changeLine)
	}
}

var (
	nameRe  = regexp.MustCompile(`\b(name|package)\b`)
	placeRe = regexp.MustCompile(`\b(place|location|drop|address|meet|pick ?up|deliver|delivery|collector|collect)\b`)
)

// sendOrderRecap reads the whole order back to the buyer
// before the payment link exists, in the agent's own words:
// the seller's format is a shape (the item and its price, the
// name for the package, how the goods reach her and where,
// the size when the product carries one, then the ask to
// confirm), never a script. The model drafts it from the
// settled facts and the draft is checked the way a price is
// checked: the item and the exact agreed figure must be in
// it, and the voice laws hold. Without a sound draft, the
// facts are read back plainly.
func sendOrderRecap(from string, s *Session, f fulfilment) {
	// The recap carries its answer on buttons, the way the
	// fulfilment choice does: Yes closes, Change opens the
	// correction, and typing either word works exactly as
	// before, because a tap is translated into the word and
	// the one road reads both.
	whatsapp.SendButtons(from, composeOrderRecap(s, f), []whatsapp.Button{
		{ID: "ff_yes", Title: "Yes"},
		{ID: "ff_change", Title: "Change"},
	})
}

func composeOrderRecap(s *Session, f fulfilment) string {
	price := money.GHS(s.AgreedPrice)
	multi := len(s.Basket) > 0
	if model.Enabled() {
		var b strings.Builder
		for _, it := range s.Basket {
			b.WriteString("Also in this order: " + it.Product.Name + "\n")
			b.WriteString("Its agreed price: " + money.GHS(it.AgreedPrice) + "\n")
			if it.Size != "" {
				b.WriteString("Its size: " + it.Size + "\n")
			}
		}
		b.WriteString("Item: " + s.Product.Name + "\n")
		b.WriteString("Agreed price: " + price + "\n")
		if multi {
			b.WriteString("The items come to " + money.GHS(basketGoods(s)+s.AgreedPrice) + " in all.\n")
		}
		if f.Size != "" {
			b.WriteString("Size: " + f.Size + "\n")
		} else if m := sizePhraseRe.FindStringSubmatch(s.Product.Description); m != nil {
			b.WriteString("Size: " + strings.ToUpper(m[1]) + "\n")
		}
		if f.PackageName != "" {
			b.WriteString("Name for the package: " + f.PackageName + "\n")
		}
		switch f.Mode {
		case "delivery":
			b.WriteString("Delivery to: " + f.Location + "\n")
		case "pickup":
			if f.Collector != "" {
				b.WriteString("Collection: " + f.Collector + " collects it from the shop\n")
			} else {
				b.WriteString("Collection: she collects it from the shop herself\n")
			}
		case "meet":
			b.WriteString("Meeting place: " + f.Location + "\n")
		default:
			b.WriteString("Delivery: arranged with the seller\n")
			if f.Location != "" {
				b.WriteString("Place she gave: " + f.Location + "\n")
			}
		}
		if f.Note != "" {
			b.WriteString("Her message for the seller is on the order.\n")
		}
		needFigure := price
		if multi {
			// The figure the draft must carry is the order's
			// whole, not one line of it.
			needFigure = money.GHS(basketGoods(s) + s.AgreedPrice)
		}
		if draft := draftWithBudget(recapCharter, "The settled facts of the order:\n"+b.String()+"\nWrite the read-back now, following the format, in your own words."); draft != "" &&
			strings.Contains(draft, s.Product.Name) && strings.Contains(draft, needFigure) && voiceClean(draft) {
			return draft
		}
	}
	// The bare factual read-back.
	lines := []string{"Please confirm your order:"}
	if multi {
		items := orderItemsOf(s, f, s.AgreedPrice)
		for _, it := range items {
			line := it.Name + " for " + money.GHS(it.Price)
			if it.Size != "" {
				line += ", size " + it.Size
			}
			lines = append(lines, line+".")
		}
		lines = append(lines, "Your items come to "+money.GHS(basketGoods(s)+s.AgreedPrice)+" in all.")
	} else {
		lines = append(lines, s.Product.Name+" for "+price+".")
		if f.Size != "" {
			lines = append(lines, "Size: "+f.Size+".")
		} else if m := sizePhraseRe.FindStringSubmatch(s.Product.Description); m != nil {
			lines = append(lines, "Size: "+strings.ToUpper(m[1])+".")
		}
	}
	if f.PackageName != "" {
		lines = append(lines, "The package will carry the name "+f.PackageName+".")
	}
	switch f.Mode {
	case "delivery":
		lines = append(lines, "Delivery to "+f.Location+".")
	case "pickup":
		if f.Collector != "" {
			lines = append(lines, f.Collector+" will collect it from the shop.")
		} else {
			lines = append(lines, "You collect it from the shop yourself.")
		}
	case "meet":
		lines = append(lines, "Meeting place: "+f.Location+".")
	default:
		line := "Delivery will be arranged with the seller."
		if f.Location != "" {
			line = "Delivery will be arranged with the seller. The place noted is " + f.Location + "."
		}
		lines = append(lines, line)
	}
	if f.Note != "" {
		lines = append(lines, "Your message for the seller is on the order.")
	}
	lines = append(lines, "Reply yes and I will send your payment link, or tell me what to change.")
	return strings.Join(lines, "\n")
}

// recapCharter is the seller's format for the moment before
// money moves, stated as a shape to follow in the agent's own
// words: nothing here is wording to recite.
const recapCharter = `You are the sales agent for a small shop on WhatsApp. A buyer has agreed a price and given her fulfilment details. Before any payment link is sent, read the order back to her so she can confirm it, in your own words, following this format:

- Name the item and the exact agreed price.
- State the size, when the facts give one.
- State the name the package will carry, when the facts give one.
- State how the goods reach her: the delivery place, the meeting place, or who collects, exactly as the facts give it.
- Note that her message for the seller is on the order, when the facts say so.
- Close by asking her to confirm, or to say what should change, before the payment link is sent.

Rules: use only the facts given; invent nothing and promise nothing about times or arrangements. Plain correct English, complete sentences, no titles for anyone, no Pidgin. The item name and the price must appear exactly as given.`

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
