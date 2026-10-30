package agent

// Merchant onboarding and catalogue commands, inside the same chat
// customers haggle in. One door, two modes: only the merchant's own
// number (see IsMerchant) ever reaches this file's handlers.
//
// The onboarding flow is a small deterministic state machine held in
// her session. No model calls take part in it: a product's name and
// prices are collected question by question, and nothing is saved
// until she confirms the summary with a clear yes. Her last price
// is asked for in the words sellers use themselves, never as a
// "floor": that is our word inside the code, and a seller at the
// market has never used it. It is never shown to customers: the
// merchant's own catalogue listing is the only place last prices
// are ever printed, and only her number can ask for it. A seller
// whose price does not move says so, in numbers (her last price is
// her price) or in words ("non-negotiable"), and she is taken at
// her word: the product is saved fixed price.

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/wants"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// OnboardingFlow is one in-progress "add a product" walk. Step is one
// of: name, description, list, lastprice, measure, measureqty,
// confirm. The measure steps sit between the last price and the
// summary: whether she sells the product by measure, and if so how
// much of it the list price she just gave is for. Fixed marks a
// product whose price does not move: her last price is her price.
type OnboardingFlow struct {
	Step        string
	Name        string
	Description string
	ListPrice   float64
	FloorPrice  float64
	Fixed       bool
	Unit        string
	BaseQty     float64
	// Photo and PhotoRef mark a flow that began as a throw-in: the
	// seller sent a photograph of her goods, with whatever words
	// she had, instead of starting the guided walk. The fields the
	// throw-in already answered are filled before the flow opens,
	// and the walk then asks only for what is missing (see
	// photo.go). A guided flow leaves both unset.
	Photo    bool
	PhotoRef string
	// ColoursSeen are the distinct main colours the photo
	// reads reported across this listing's photos, in the
	// order they were seen. Two or more means the listing is
	// one item in several colours, and the colour rule
	// shapes its name and description at save.
	ColoursSeen []string
	// PhotoOpened records that this listing was opened BY a
	// photo, as opposed to a photo joining a listing the
	// seller opened with her own words (a typed walk or a
	// pasted advert). The photo gate in photo.go applies only
	// to photo-opened listings: their later photos must prove
	// they belong, while a photo she throws at a listing she
	// named herself is taken as deliberate.
	PhotoOpened bool
	// SizeNote is a size lifted out of the seller's typed name
	// line ("BLACK NYLON SHIRT @250GHS SIZE XXL"). It is not part
	// of the name; it rides into the description when she answers
	// that step, and it is never asked about on its own.
	SizeNote string
	// Sizes are the sizes the product comes in, read by the
	// model from her words or the goods at listing time; at
	// save, any path that produced none gets one text read of
	// her name and description (see flowSizes).
	Sizes []string
	// PriceNeedsConfirm and PriceConfirmed are the intake gate.
	// A price harvested from her words that the harvest itself
	// doubts (see harvestDoubt) is read back to her as a question
	// and must be confirmed in her own words before the walk goes
	// on, so a misread can never ride silently to the shelf. A
	// price she typed as a direct answer to the price question
	// is her own statement and is never gated.
	PriceNeedsConfirm bool
	PriceConfirmed    bool
	// PendingFigure is a figure she stated while this listing
	// still had no name. It is deliberately NOT a list price
	// or a last price yet: a price cannot be owned by a
	// product whose identity is not settled, and a later
	// photo must never inherit it by arriving while the name
	// is still missing. It is attached only when she herself
	// supplies the name for this same listing (see
	// settlePendingFigure).
	PendingFigure float64
	// DupName is the shop's existing product that a new listing's
	// name has clashed with, held while she decides between
	// replacing it and renaming hers (step "dupchoice").
	DupName string
	// PhotoCompact marks a listing that began with a photo:
	// the seller gets one compact read-back with one question,
	// and her last price answer saves the product at once,
	// with no summary to confirm and no further questions.
	PhotoCompact bool
	// UnitJudged records that the agent has already made its
	// one smart call on how this product is sold, so the
	// judgement is never repeated and the measure question,
	// when it survives the judgement, is asked exactly once.
	UnitJudged bool
	// MeasureSettled records that the measure step has been
	// answered (including "only as a whole"), so the walk can
	// move past it for good.
	MeasureSettled bool
	// RepeatAckAt stamps when this listing last spoke about
	// its photos: when the photo listing opened, when a photo
	// joined it, or when the still-on-that-photo line was
	// sent. An album arrives as one message after another,
	// seconds apart, all one act of sending, so a repeat
	// inside that window is taken in silently; the line
	// exists for a photo re-sent after the window, when the
	// seller is genuinely checking whether it landed.
	RepeatAckAt time.Time
	// HeldAckAt stamps the one-at-a-time line for a different
	// product held during this listing, so a burst of held
	// products earns it once between them.
	HeldAckAt time.Time
	// PhotoRefs are the media ids of the photos collected for
	// this listing: the first she sent and the further angles
	// that arrived with it. At the save they are downloaded
	// and kept on the product (see photos.go).
	PhotoRefs []string
}

var (
	addProductRe   = regexp.MustCompile(`\b(add|new)\s+(a\s+|an\s+|another\s+)?(new\s+)?(product|item|goods?|stock)\b`)
	myCatalogueRe  = regexp.MustCompile(`\bmy (catalogue|products|items)\b`)
	changePriceRe  = regexp.MustCompile(`^change (?:the )?(?:list )?price of (.+?) to (.+)$`)
	changeFloorRe  = regexp.MustCompile(`^change (?:the )?(?:last|floor)(?: price)? of (.+?) to (.+)$`)
	removeRe       = regexp.MustCompile(`^remove (?:the )?(.+)$`)
	setAllowanceRe = regexp.MustCompile(`\b(set|change|update|make)\b[^0-9]*\ballowance\b|\ballowance\b[^0-9]*\d`)
	// Delivery commands: the fee parses like any price; the note is
	// captured from her original words, case and all, because buyers
	// will read it back exactly as she wrote it.
	setDeliveryFeeRe  = regexp.MustCompile(`^set delivery fee (.+)$`)
	setDeliveryNoteRe = regexp.MustCompile(`(?i)^set delivery note (.+)$`)
	// How the fee is known is her choice too: quoted per order,
	// or set up front; and in a set shop, a named area can carry
	// its own fee ("set area fee Osu 30", or several at once:
	// "set area fees Osu 30, Kasoa 45").
	setDeliveryQuoteRe = regexp.MustCompile(`(?i)^(set delivery per order|quote delivery( fees)?|delivery per order|quote the delivery fee each time)$`)
	setDeliverySetRe   = regexp.MustCompile(`(?i)^(set delivery (fees )?(up front|fixed)|delivery fees up front)$`)
	setAreaFeeRe       = regexp.MustCompile(`(?i)^set area fees? (.+)$`)
	// Measure commands: a product gains a measure ("set measure of
	// <item> to <qty> <unit>") or loses it ("sell <item> whole
	// only", "<item> whole only"), in her own words.
	setMeasureRe  = regexp.MustCompile(`^set measure of (.+?) to (.+)$`)
	wholeOnlyRe   = regexp.MustCompile(`^(?:sell )?(.+?) whole only$`)
	setStockRe    = regexp.MustCompile(`^(?:set|update) stock of (.+?) to (\d+)$`)
	myOrdersRe    = regexp.MustCompile(`^(my )?orders[.!?]?$`)
	cancelOrderRe = regexp.MustCompile(`^cancel order ([a-z0-9-]+)[.!?]?$`)
	// The wants list: what her buyers asked for that she does not
	// stock, kept for her in the want book.
	wantsListRe = regexp.MustCompile(`^(buyer )?wants$|^(buyer )?requests$`)
	// Who takes the delivery money is her decision, in her own
	// plain shapes: the money comes to her, inside her buyers'
	// payment, or her buyers pay whoever brings the item.
	deliveryToMeRe      = regexp.MustCompile(`(delivery money comes to me|i take the delivery money|delivery money to me|i collect the delivery money|delivery money is mine)`)
	deliveryToCarrierRe = regexp.MustCompile(`(buyer pays the delivery|buyer pays delivery|buyers pay the delivery|buyer pays the rider|buyer pays the carrier|delivery money goes to the (rider|carrier|driver|station))`)
)

// controlWord normalises a short reply for comparison: lower case,
// trimmed, with trailing punctuation dropped, so "Yes." and "cancel!"
// read as the words she means.
func controlWord(lower string) string {
	return strings.Trim(lower, " .!")
}

func isClearYes(lower string) bool {
	switch controlWord(lower) {
	case "yes", "yeah", "yep", "yes please", "confirm", "save", "save it", "ok", "okay", "go ahead":
		return true
	}
	return false
}

func isClearNo(lower string) bool {
	switch controlWord(lower) {
	case "no", "nope", "no thanks", "drop it", "do not", "don't":
		return true
	}
	return false
}

// noteSetItRe reads a set-shaped answer to the allowance
// note: a set verb, an optional "it" or "to", and a figure
// on its way ("set it 0", "make it 300", "put 200").
var noteSetItRe = regexp.MustCompile(`^(set|make|change|put)\s+(it\s+)?(to\s+)?\d`)

// noteAllowanceAmount reads the amount in an answer to the
// allowance note, or nil when the answer carries none in a
// set shape: the command's own shape ("set allowance 300"),
// a set verb against the note ("set it 0"), or a bare figure
// and nothing else ("300", "GH₵300") — against the note, a
// bare figure can only be the amount she is setting.
func noteAllowanceAmount(text, lower string) *float64 {
	if setAllowanceRe.MatchString(lower) {
		amount := parseOffer(text)
		if idx := strings.LastIndex(lower, "allowance"); idx >= 0 {
			if after := parseOffer(text[idx:]); after != nil {
				amount = after
			}
		}
		return amount
	}
	if noteSetItRe.MatchString(lower) {
		return parseOffer(text)
	}
	if burstAnswerShape(text) {
		return parseOffer(text)
	}
	return nil
}

// merchantAckRe reads a bare acknowledgement or pleasantry
// as the WHOLE of a merchant message: an ok, a thanks, a
// laugh in words. Anything carrying more — a figure, a
// product, a question — is not a bare ack and never matches.
var merchantAckRe = regexp.MustCompile(`^(ok|okay|k|kk|alright|got it|understood|sure|fine|nice|cool|great|perfect|thanks|thank you|thx|lol|haha|hehe|yes please|no thanks)[.! ]*$`)

// merchantAck reports whether the merchant's whole message
// is a bare acknowledgement, a pleasantry, or emoji alone —
// the turns a person answers with a nod, which in merchant
// chat used to fall through every door into the day answer.
func merchantAck(text string) bool {
	stripped := strings.TrimSpace(stripEmoji(strings.ToLower(text)))
	if stripped == "" {
		// Nothing but emoji (or nothing): an emoji alone is
		// an acknowledgement in modern chat.
		return strings.TrimSpace(text) != ""
	}
	return merchantAckRe.MatchString(stripped)
}

// stripEmoji removes pictographs and their joiners and
// selectors, leaving the words of a message behind.
func stripEmoji(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 0x1F000 && r <= 0x1FAFF, // pictographs, emoticons, symbols
			r >= 0x2600 && r <= 0x27BF, // misc symbols and dingbats
			r >= 0x2B00 && r <= 0x2BFF, // stars, arrows and marks
			r == 0xFE0F,                // variation selector
			r == 0x200D,                // zero-width joiner
			r == 0x2764, r == 0x2763,   // hearts and their kin
			r == 0x203C, r == 0x2049: // double exclamation and friends
			return -1
		}
		return r
	}, s)
}

// answerMerchantAck answers a bare acknowledgement the way
// a person does: briefly, warmly, and with nothing else
// attached — no figures, no report, no reminder.
func answerMerchantAck(from string, s *Session) {
	variants := []string{"Alright.", "Okay.", "Got it.", "Alright, I am here when you need me."}
	whatsapp.SendText(from, variants[s.Acks%len(variants)])
	s.Acks++
}

// handleMerchantMessage is the merchant-mode dispatcher for one
// shop: the shop the sender owns, resolved by the caller before
// anything else, and the only shop her words can touch. Order
// matters: an in-progress flow owns her words; a pending removal
// owns her next reply; then the onboarding trigger; then, on an
// empty shelf, the first-run greeting; then catalogue commands; and
// only then her day questions, answered from the day book as before.
func handleMerchantMessage(from, text, merchantID string) {
	handleMerchantMessageCtx(from, text, merchantID, "")
}

// handleMerchantMessageCtx is the merchant dispatcher with the
// inbound message's quote context: when her message quotes the
// allowance note the agent sent her, she is pointing at the
// note, and the note's figures are never read as a command she
// is giving (the same protection the explain guard gives her
// words, extended to the case where her words alone do not say
// it: the quote says it).
func handleMerchantMessageCtx(from, text, merchantID, contextID string) {
	merchant := catalogue.GetMerchant(merchantID)
	if merchant == nil {
		return
	}
	lower := strings.ToLower(text)
	key := sessionKey(merchantID, from)
	s, ok := sessions[key]
	if !ok {
		s = &Session{Stage: "merchant", MerchantID: merchantID}
		sessions[key] = s
	}
	noteSpeakerWords(s, text)

	if s.Onboarding != nil {
		if serveSellerWalkTurn(from, s, merchant, text, lower) {
			return
		}
		advanceOnboarding(from, s, text, lower)
		return
	}

	// A presented burst batch is waiting on its prices: a
	// reply shaped like its answer belongs to it before
	// anything else reads it (see burst.go). Any other
	// message — no figures, or a sentence that merely
	// contains one — is not its answer and falls through
	// untouched, with the batch still waiting.
	if s.BurstAwaiting && handleBurstAnswer(from, s, text, lower) {
		return
	}

	if s.PendingRemove != "" {
		name := s.PendingRemove
		s.PendingRemove = ""
		if isClearYes(lower) {
			removed, err := catalogue.RemoveProduct(merchantID, name)
			if err != nil {
				whatsapp.SendText(from, "I could not remove it after all: "+err.Error()+". Nothing changed.")
				return
			}
			whatsapp.SendText(from, removed.Name+" is out of your catalogue. Customers will no longer be offered it.")
			return
		}
		whatsapp.SendText(from, "No problem. "+name+" stays in your catalogue.")
		return
	}

	// Her answer to an open per-order delivery fee request
	// (see deliveryfees.go). It sits after her in-progress
	// flows, which own her words while they are open, and
	// before her commands. With no open request in her shop
	// it consumes nothing.
	if handleMerchantFeeQuote(from, merchant, text, lower) {
		return
	}

	// Her word on an open in-hand confirmation (see
	// inhand.go): a sale is waiting on it before its buyer
	// may pay. Same discipline: after her flows, before her
	// commands, and silent when nothing waits.
	if handleMerchantInHand(from, merchant, text, lower) {
		return
	}

	// Her word that she refunded a disputed order (see
	// disputes.go): never taken alone; the buyer is asked to
	// confirm, and her answer is what closes it.
	if handleMerchantRefundClaim(from, merchant, text, lower) {
		return
	}

	// The payment setup retry lives here, rather
	// than with her catalogue commands: a shop whose payment setup
	// failed at signup is usually still empty-shelved, and the
	// empty-shelf help below would swallow the command before the
	// command path ever saw it.
	if retryPaymentSetup(from, merchant, lower) {
		return
	}

	if addProductRe.MatchString(lower) {
		s.Onboarding = &OnboardingFlow{Step: "name"}
		whatsapp.SendText(from, "Let us add a new product. What is it called? You can say cancel at any point and nothing will be saved.")
		return
	}

	// A question about a command is not the command. A seller
	// quoting the allowance line back with "explain this" is
	// asking what it means, and the command path reading her
	// quoted figures as an instruction once set her allowance
	// to a number she never chose. Questions skip the commands
	// and go to the thinking, which explains from the facts.
	// The same holds when the quote itself does the pointing:
	// a reply quoting the note is about the note.
	quotingNote := contextID != "" && s.LastNoteMsgID != "" && contextID == s.LastNoteMsgID
	if turnQuote != nil && turnQuote.Known && s.LastMerchantNote != "" && turnQuote.Body == s.LastMerchantNote {
		// The same pointing, recognised through the message
		// book: whatever id the quote carried, its words are
		// the note's own words.
		quotingNote = true
	}

	// An answer to the allowance note sets the allowance.
	// The note itself invites the reply ("to set it, say:
	// set allowance 300"), and the most natural answer is a
	// quote of the note with the amount in her own shape:
	// "set it 0", "make it 300", a bare "300". Commands are
	// skipped for quotes (a quoted figure was once read as
	// an instruction she never gave), so without this door
	// her answer was only ever drafted about, never done —
	// the agent once confirmed an allowance it had not set.
	// A set shape against the note is unambiguous, and it is
	// the only quote that acts.
	if quotingNote && s.Onboarding == nil && !explainRe.MatchString(lower) && !strings.Contains(lower, "you have not set") && !strings.Contains(lower, "to set it, say") {
		// The note's own clauses in her message mean she is
		// quoting the note itself back — showing the agent
		// its own line, most likely to ask about it — which
		// is never an instruction, however its figures read.
		if amount := noteAllowanceAmount(text, lower); amount != nil {
			if err := catalogue.SetAllowance(merchantID, *amount); err != nil {
				whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
				return
			}
			if *amount <= 0 {
				whatsapp.SendText(from, "Done. Your daily giveaway allowance is now "+money.GHS(*amount)+": I will give nothing away when buyers bargain, and I will not raise it with you again.")
			} else {
				whatsapp.SendText(from, "Done. Your daily giveaway allowance is now "+money.GHS(*amount)+". Every discount I agree comes out of it, and I haggle firmer as it runs down.")
			}
			return
		}
	}

	// Her words are understood once, lazily: the doors below
	// ask the mind only when they reach the point of needing
	// it, so a turn her commands serve never spends the call.
	// The mind goes before the advert door now, because a
	// statement about her shop ("I have sent the bag order")
	// and goods copy for the shelf read alike to a harvest
	// and only meaning tells them apart; the door remains,
	// below, for the days the model cannot be reached.
	var thought *understood
	thoughtDone := false
	think := func() *understood {
		if !thoughtDone {
			thought = understandMerchantMessage(from, merchant, merchantID, text)
			thoughtDone = true
		}
		return thought
	}

	// An advert pasted with no walk open is itself the
	// instruction to list it: she copies the advert she
	// already posts everywhere and sends it once, the way she
	// would hand it to a person, and a person would start the
	// listing from it, not thank her and note it down. It is
	// tried at the two doors where nothing else owns her
	// words: an empty shelf (before its help text, which she
	// is plainly past), and below, after her commands have
	// had their chance, for a stocked shop. A reply quoting
	// the agent's own note is about the note, never an
	// advert, however its lines and figures read.
	if len(merchant.Products) == 0 {
		if !quotingNote {
			flow := &OnboardingFlow{Step: "name"}
			s.Onboarding = flow
			if openWalkFromAdvert(from, s, flow, text) {
				return
			}
			s.Onboarding = nil
		}
		// Her shelf may be empty while her orders still move:
		// an operational word about them is served by the
		// mind before the empty-shelf help.
		if u := think(); u != nil && (u.Intent == intentOrderSent || u.Intent == intentOrderPayment || u.Intent == intentAskBuyer || u.Intent == intentAttention) && actOnMerchantUnderstanding(from, merchant, merchantID, text, u) {
			return
		}
		whatsapp.SendText(from, "Hello. Your shelf at "+merchant.Name+" is empty just now, so there is nothing to sell yet. Say \"add a product\" and I will walk you through it: a name, a short description, the price customers see, and your last price, the lowest you would accept, which customers never see. The moment you confirm, I can sell it.")
		return
	}

	if !explainRe.MatchString(lower) && !quotingNote && handleMerchantCommand(from, s, merchant, text, lower) {
		return
	}

	// A plain question about her day is answered by the day
	// answer itself, not by a thinking draft: the day answer
	// is the consistent one, and it carries the allowance
	// prompt when she has none set, the very line she may
	// next ask to have explained. Its shapes are whole-message
	// anchored, so an advert can never be mistaken for one.
	if dayAskRe.MatchString(lower) {
		answerMerchantDayQuestion(from, text, merchantID)
		return
	}

	// A bare acknowledgement, a pleasantry, or an emoji
	// alone is answered like one, before any thinking is
	// spent on it: it has no question in it, and the day
	// answer is not a reply to a laugh. (A pending yes/no
	// of hers — a removal, an in-hand wait — has already
	// been served above and never reaches here.)
	if merchantAck(text) {
		answerMerchantAck(from, s)
		return
	}

	// The mind reads her words before the advert door does:
	// goods copy it names a new listing opens the same walk
	// the door opens, and everything else it claims is
	// served from her facts.
	if u := think(); u != nil {
		if u.Intent == intentNewListing && s.Onboarding == nil && !quotingNote && !explainRe.MatchString(lower) {
			flow := &OnboardingFlow{Step: "name"}
			s.Onboarding = flow
			if openWalkFromAdvert(from, s, flow, text) {
				return
			}
			s.Onboarding = nil
		} else if actOnMerchantUnderstanding(from, merchant, merchantID, text, u) {
			return
		}
	}

	if s.Onboarding == nil && !quotingNote && !explainRe.MatchString(lower) {
		flow := &OnboardingFlow{Step: "name"}
		s.Onboarding = flow
		if openWalkFromAdvert(from, s, flow, text) {
			return
		}
		s.Onboarding = nil
	}

	answerMerchantDayQuestion(from, text, merchantID)
}

// priceCorrectionRe reads a seller correcting the list price
// inside an open onboarding flow, in her own plain shapes: "price
// is 2500", "the list price should be 2500", "change price to
// 2500". A price read from her throw-in can be wrong (a figure
// that counted something, taken as the price), and a correction
// she can make in one line is what keeps a wrong read from
// trapping the whole flow against a last price it can never take.
var priceCorrectionRe = regexp.MustCompile(`^(?:the\s+)?(?:list\s+)?price\s+(?:is|should\s+be)\s+(.+)$|^(?:change|correct|set)\s+(?:the\s+)?(?:list\s+)?price\s+(?:to\s+)?(.+)$`)

// fixedPriceRe reads a seller saying her price does not move, in
// the shapes sellers use: "non-negotiable", "fixed price", "price
// is final", "no bargaining", or simply "same price" when she is
// asked for her last price. Paired with no figure, or with the
// price itself, it settles the last price step as fixed.
var fixedPriceRe = regexp.MustCompile(`\b(non[-\s]?negotiable|not negotiable|no negotiation|fixed price|price is fixed|price is final|final price|no bargaining|no haggling|same price)\b|^fixed$|^same$`)

var replaceRe = regexp.MustCompile(`^replace\b`)

// enterDupChoice parks a flow whose name is already on the shelf
// and puts the choice to her: this listing replaces the old one,
// or it carries a different name. It returns true when the name
// clashed and the flow is parked, so the caller stops there. A
// name clash is never a dead end: a seller who pastes a corrected
// advert over a bad listing needs a way through, not an order to
// invent a new name for the same goods.
func enterDupChoice(from string, s *Session, flow *OnboardingFlow) bool {
	if flow.Name == "" {
		return false
	}
	existing := catalogue.FindByName(s.MerchantID, flow.Name)
	if existing == nil {
		return false
	}
	flow.DupName = existing.Name
	flow.Step = "dupchoice"
	whatsapp.SendText(from, "You already sell something called "+existing.Name+". Shall this new listing replace the old one? Say replace, or give this one a different name. Say cancel to drop it.")
	return true
}

// walkPriceShape reports whether a message at one of the
// walk's price steps is shaped like a price answer. The
// burst batch learned this first (see burstAnswerShape):
// once the figures are scrubbed out, only currency words,
// price words and the soft lead-ins a person answering a
// price question actually uses may remain ("450", "it is
// 450", "450 cedis only", "fixed price 450"). Any other
// word makes the message a sentence, and a figure inside a
// sentence is usually about something else: "I have 2
// pairs in stock" at the last price step once saved a
// product at GH₵2. A falsely refused answer costs her one
// more message; a falsely accepted one costs a wrongly
// priced product.
func walkPriceShape(text string) bool {
	residue := burstFigureScrub.ReplaceAllString(strings.ToLower(text), " ")
	for _, w := range strings.FieldsFunc(residue, func(r rune) bool {
		return !unicode.IsLetter(r)
	}) {
		switch w {
		case "gh", "ghc", "cedis", "price", "is", "it", "its", "the", "a", "an", "my", "i",
			"list", "last", "at", "for", "selling", "sell", "sold", "goes", "going",
			"be", "will", "that", "this", "only", "fixed", "about", "take":
		default:
			return false
		}
	}
	return true
}

// advanceOnboarding feeds one merchant message to the onboarding flow.
func advanceOnboarding(from string, s *Session, text, lower string) {
	// When this turn ends the listing, a kept burst batch is
	// presented next (see burst.go).
	defer presentBurstIfReady(from, s)
	flow := s.Onboarding

	if controlWord(lower) == "cancel" {
		s.Onboarding = nil
		whatsapp.SendText(from, "Dropped it. Nothing was saved. Say \"add a product\" whenever you are ready.")
		return
	}

	// A price correction owns the message wherever the flow is
	// standing, the list step itself aside (there her message is
	// simply the price) and the duplicate choice aside (there her
	// message is spoken for). Correcting the price clears a last price that
	// no longer sits below it, so she is never left defending a
	// last price against a price she has already replaced. A
	// correction is also her own statement of the price, so it
	// settles the intake gate: a price she has just stated in so
	// many words is never read back to her for confirmation.
	if flow.Step != "list" && flow.Step != "dupchoice" && flow.Name != "" {
		if m := priceCorrectionRe.FindStringSubmatch(lower); m != nil {
			tail := m[1]
			if tail == "" {
				tail = m[2]
			}
			if amount := parseOffer(tail); amount != nil && *amount > 0 {
				flow.ListPrice = *amount
				flow.PriceNeedsConfirm = false
				flow.PriceConfirmed = true
				if flow.FloorPrice >= *amount {
					flow.FloorPrice = 0
					flow.Fixed = false
				}
				whatsapp.SendText(from, "Corrected. The list price for "+flow.Name+" is now "+money.GHS(*amount)+".")
				gotoNextOnboardingStep(from, flow)
				return
			}
		}
	}

	switch flow.Step {
	case "dupchoice":
		// Her answer to the name clash: replace the old listing,
		// keep it and rename this one, or drop the whole thing
		// (cancel is handled before the switch). Anything else
		// she sends is read as the new name she is offering.
		switch {
		case replaceRe.MatchString(controlWord(lower)):
			if _, err := catalogue.RemoveProduct(s.MerchantID, flow.DupName); err != nil {
				whatsapp.SendText(from, "I could not take the old "+flow.DupName+" off the shelf: "+err.Error()+". Give this one a different name instead, or say cancel.")
				return
			}
			flow.DupName = ""
			whatsapp.SendText(from, "Done, the old one is off the shelf. This listing takes its place.")
			if note := settlePendingFigure(flow); note != "" {
				whatsapp.SendText(from, note)
			}
			gotoNextOnboardingStep(from, flow)
		case isClearNo(lower):
			whatsapp.SendText(from, "Alright, the old one stays. What name should this new one carry?")
		default:
			name, _, _ := parseTypedNameLine(text, flow.ListPrice)
			if name == "" {
				whatsapp.SendText(from, "Say replace to swap the old one out, or give this listing a different name.")
				return
			}
			flow.Name = name
			if enterDupChoice(from, s, flow) {
				return
			}
			flow.DupName = ""
			if note := settlePendingFigure(flow); note != "" {
				whatsapp.SendText(from, note)
			}
			gotoNextOnboardingStep(from, flow)
		}

	case "confirmprice":
		// The intake gate. The price below was harvested from her
		// words and the harvest doubts it; nothing moves until
		// she settles it in her own words: yes keeps it, a figure
		// replaces it, no throws it out and asks her to type the
		// price plainly. A correction in the "price is X" shape
		// never reaches this case: the hook above the switch
		// takes it first and settles the gate as it corrects.
		switch {
		case isClearYes(lower):
			flow.PriceConfirmed = true
			flow.PriceNeedsConfirm = false
			gotoNextOnboardingStep(from, flow)
		case isClearNo(lower):
			flow.ListPrice = 0
			flow.PriceNeedsConfirm = false
			flow.Step = "list"
			whatsapp.SendText(from, "Alright, that price is out. What is the list price for "+flow.Name+", in cedis? For example, "+money.GHS(450)+".")
		default:
			if amount := parseOffer(lower); amount != nil && *amount > 0 && walkPriceShape(text) {
				flow.ListPrice = *amount
				flow.PriceConfirmed = true
				flow.PriceNeedsConfirm = false
				if flow.FloorPrice >= *amount {
					flow.FloorPrice = 0
					flow.Fixed = false
				}
				whatsapp.SendText(from, "Noted: the price for "+flow.Name+" is "+money.GHS(*amount)+".")
				gotoNextOnboardingStep(from, flow)
				return
			}
			whatsapp.SendText(from, "I read the price as "+money.GHS(flow.ListPrice)+". Is that right? Say yes, or give me the right price.")
		}

	case "name":
		// A seller can answer before the photo read-back reaches
		// her screen. A bare figure is never a product name, and
		// it is not attached to this listing's prices either:
		// the product has no name yet, so the figure has no
		// settled owner. It is noted as a pending figure, and
		// attached only when she herself names this listing
		// (see settlePendingFigure). Attaching it here let a
		// later photo inherit the price by supplying the
		// missing name, which is how one product's last price
		// landed on another product on the live number.
		if flow.Photo && bareFigureRe.MatchString(text) {
			if amount := parseOffer(lower); amount != nil && *amount > 0 {
				if flow.ListPrice > 0 && *amount > flow.ListPrice {
					whatsapp.SendText(from, "I read that figure, "+money.GHS(*amount)+", as a price, not a product name. It is above the current list price of "+money.GHS(flow.ListPrice)+". If the list price is wrong, say 'price is 500'. What is the product called?")
					return
				}
				flow.PendingFigure = *amount
				whatsapp.SendText(from, "I have noted "+money.GHS(*amount)+", but I will not attach it to a product until I know what this one is called. What is it called?")
				return
			}
		}
		// A whole advert pasted in one message is read whole:
		// she copied the advert she already posts everywhere
		// and sent it once, so the agent does the separating.
		// The first line is the name, the rest is her
		// description, the marked price is taken from the whole
		// message, and the walk then asks only for what the
		// advert did not carry. What was read is told back to
		// her, so nothing is taken from her words in silence.
		if openWalkFromAdvert(from, s, flow, text) {
			return
		}
		// Her line is read the way a person reads it: a price
		// riding in it (@250GHS, 250 cedis) is her list price and
		// is never asked for again, and a size riding in it is
		// lifted out of the name to ride with the description.
		// What was read is told back to her in one short message,
		// so nothing is ever taken from her words in silence.
		name, price, sizeNote := parseTypedNameLine(text, flow.ListPrice)
		if name == "" {
			whatsapp.SendText(from, "What is the product called?")
			return
		}
		hadPrice := flow.ListPrice > 0
		flow.Name = name
		flow.SizeNote = sizeNote
		if price > 0 {
			flow.ListPrice = price
			if !hadPrice {
				if _, span, ok := throwPrice(text); ok {
					flow.PriceNeedsConfirm = harvestDoubt(text, price, span)
					flow.PriceConfirmed = false
				}
			}
		}
		if enterDupChoice(from, s, flow) {
			return
		}
		var read []string
		if note := settlePendingFigure(flow); note != "" {
			read = append(read, note)
		}
		if price > 0 {
			read = append(read, "I took the price from your line: "+money.GHS(price)+".")
		}
		if sizeNote != "" {
			read = append(read, "I have noted the size, "+strings.TrimPrefix(sizeNote, "Size ")+", with the description.")
		}
		if len(read) > 0 {
			whatsapp.SendText(from, strings.Join(read, " "))
		}
		gotoNextOnboardingStep(from, flow)

	case "description":
		description := strings.TrimSpace(text)
		if description == "" {
			whatsapp.SendText(from, "Give me a short description for "+flow.Name+": a line or two customers will read.")
			return
		}
		flow.Description = description
		if flow.SizeNote != "" {
			if !strings.Contains(strings.ToLower(description), "size") {
				flow.Description = strings.TrimSuffix(description, ".") + ". " + flow.SizeNote + "."
			}
			flow.SizeNote = ""
		}
		gotoNextOnboardingStep(from, flow)

	case "list":
		amount := parseOffer(lower)
		if amount == nil || *amount <= 0 {
			whatsapp.SendText(from, "I could not read that as a price. What is the list price for "+flow.Name+", in cedis? For example, "+money.GHS(450)+".")
			return
		}
		if !walkPriceShape(text) {
			whatsapp.SendText(from, "I did not take a price from that: it reads as a sentence, and a figure inside a sentence is usually about something else. What is the list price for "+flow.Name+", in cedis? Just the figure is fine, for example "+money.GHS(450)+".")
			return
		}
		flow.ListPrice = *amount
		gotoNextOnboardingStep(from, flow)

	case "lastprice":
		amount := parseOffer(lower)
		// Her price does not move, said in words with no figure,
		// or with the price itself as the figure: that is a
		// fixed price, and she is taken at her word.
		if fixedPriceRe.MatchString(lower) && (amount == nil || *amount == flow.ListPrice) {
			flow.FloorPrice = flow.ListPrice
			flow.Fixed = true
			whatsapp.SendText(from, "Understood: "+money.GHS(flow.ListPrice)+", fixed. I will not bargain on this one.")
			gotoNextOnboardingStep(from, flow)
			return
		}
		if amount == nil || *amount <= 0 {
			whatsapp.SendText(from, "I could not read that as a price. What is your last price for "+flow.Name+", in cedis? Or say non-negotiable if the price does not move. If the list price itself is wrong, correct it like this: price is 500.")
			return
		}
		if !walkPriceShape(text) {
			whatsapp.SendText(from, "I did not take a last price from that: it reads as a sentence, and a figure inside a sentence is usually about something else. What is your last price for "+flow.Name+", in cedis? Just the figure is fine. Or say non-negotiable if the price does not move.")
			return
		}
		if *amount > flow.ListPrice {
			whatsapp.SendText(from, "Your last price cannot sit above your price of "+money.GHS(flow.ListPrice)+": the last price is the lowest you will go, not a higher one. If "+money.GHS(flow.ListPrice)+" is final, say non-negotiable. Otherwise, what is your last price below "+money.GHS(flow.ListPrice)+"? If the list price itself is wrong, correct it like this: price is 500.")
			return
		}
		if *amount == flow.ListPrice {
			flow.FloorPrice = *amount
			flow.Fixed = true
			whatsapp.SendText(from, "Understood: "+money.GHS(flow.ListPrice)+", fixed. I will not bargain on this one.")
			gotoNextOnboardingStep(from, flow)
			return
		}
		flow.FloorPrice = *amount
		flow.Fixed = false
		gotoNextOnboardingStep(from, flow)

	case "measure":
		// The answer can carry everything at once ("by the yard,
		// 6 yards"): a quantity phrase settles both the unit and
		// how much of it her list price is for.
		if q, ok := parseQuantity(lower); ok {
			flow.Unit, flow.BaseQty = q.Unit, q.Qty
			flow.MeasureSettled = true
			gotoNextOnboardingStep(from, flow)
			return
		}
		if strings.Contains(lower, "whole") || isClearNo(lower) {
			flow.MeasureSettled = true
			gotoNextOnboardingStep(from, flow)
			return
		}
		if unit, ok := unitMentioned(lower); ok {
			flow.Unit = unit
			flow.Step = "measureqty"
			whatsapp.SendText(from, "How much of it is the "+money.GHS(flow.ListPrice)+" price for?")
			return
		}
		whatsapp.SendText(from, "I did not catch that. Do you sell the "+flow.Name+" by measure, like by the yard, litre or kilo, or only as a whole?")

	case "measureqty":
		if q, ok := parseQuantity(lower); ok {
			flow.Unit, flow.BaseQty = q.Unit, q.Qty
			flow.MeasureSettled = true
			gotoNextOnboardingStep(from, flow)
			return
		}
		if amount := parseOffer(lower); amount != nil && *amount > 0 {
			// A bare number takes the unit she already named.
			flow.BaseQty = *amount
			flow.MeasureSettled = true
			gotoNextOnboardingStep(from, flow)
			return
		}
		whatsapp.SendText(from, "How much of it is the "+money.GHS(flow.ListPrice)+" price for? Give it like 6 "+unitWord(flow.Unit, 2)+".")

	case "confirm":
		switch {
		case isClearYes(lower):
			saveName, saveDesc := applyColourRule(flow, flow.Name, flow.Description)
			product, err := catalogue.AddProduct(s.MerchantID, catalogue.Product{
				Name:        saveName,
				Description: saveDesc,
				ListPrice:   flow.ListPrice,
				FloorPrice:  flow.FloorPrice,
				Fixed:       flow.Fixed,
				Stock:       1,
				Unit:        flow.Unit,
				BaseQty:     flow.BaseQty,
				Sizes:       flowSizes(flow),
			})
			s.Onboarding = nil
			if err != nil {
				whatsapp.SendText(from, "I could not save it: "+err.Error()+". Nothing went live.")
				return
			}
			if product.Fixed {
				whatsapp.SendText(from, "Saved. "+product.Name+" is live at "+money.GHS(product.ListPrice)+", fixed price, and I can sell it straight away.")
			} else {
				whatsapp.SendText(from, "Saved. "+product.Name+" is live at "+money.GHS(product.ListPrice)+", and I can sell it straight away. Your last price of "+money.GHS(product.FloorPrice)+" stays between us.")
			}
			attachFlowPhotos(s.MerchantID, product, flow)
			// Buyers who asked for this before it existed are told
			// now, in this moment, not left waiting on an alert
			// that never comes.
			alertWaitingBuyers(s.MerchantID, product)
			// And she gets the words to bring more of them in:
			// a Status caption and a forwardable broadcast,
			// ready the moment the listing is live.
			announceListing(from, s.MerchantID, product)
		case isClearNo(lower):
			s.Onboarding = nil
			whatsapp.SendText(from, "Dropped it. Nothing was saved. Say \"add a product\" whenever you are ready.")
		default:
			whatsapp.SendText(from, "Nothing is saved until you say yes. Shall I add "+flow.Name+" at "+money.GHS(flow.ListPrice)+"? Say yes to save it, or no to drop it.")
		}
	}
}

// settlePendingFigure attaches a figure she stated before
// this listing had a name, now that she has named the
// listing herself. The attachment is judged against the
// settled list price exactly as if she had said it at the
// price steps: below it, it is her last price; equal to it,
// a fixed price; with no list price yet, it is the list
// price; above the list price it is not attached at all.
// It returns the line telling her what was done, or ""
// when there is nothing pending.
func settlePendingFigure(flow *OnboardingFlow) string {
	if flow == nil || flow.PendingFigure <= 0 || flow.Name == "" {
		return ""
	}
	amount := flow.PendingFigure
	flow.PendingFigure = 0
	switch {
	case flow.ListPrice <= 0:
		flow.ListPrice = amount
		flow.PriceNeedsConfirm = false
		flow.PriceConfirmed = true
		return "I have attached the " + money.GHS(amount) + " you gave earlier as the list price for " + flow.Name + "."
	case amount < flow.ListPrice:
		flow.FloorPrice = amount
		flow.Fixed = false
		return "I have attached the " + money.GHS(amount) + " you gave earlier as the last price for " + flow.Name + "."
	case amount == flow.ListPrice:
		flow.FloorPrice = amount
		flow.Fixed = true
		return "I have attached the " + money.GHS(amount) + " you gave earlier as the fixed price for " + flow.Name + "."
	default:
		return "The " + money.GHS(amount) + " you gave earlier is above the list price for " + flow.Name + ", so I have not attached it."
	}
}

// gotoNextOnboardingStep moves a flow to the first step it still
// needs and asks that step's question. A step counts as answered
// when its field is filled, however it got filled: by her answer
// just now in the guided walk, or before the walk even opened by
// the words and the draft that came with a thrown-in photo. That
// is what lets one walk serve both doors: in the guided walk
// nothing is ever pre-filled, so it asks every question in order,
// exactly as it always has; in a throw-in it asks only for what
// the seller did not already give. The last price is never
// pre-filled: no post a seller makes carries it, and a price is
// never guessed for it.
func gotoNextOnboardingStep(from string, flow *OnboardingFlow) {
	switch {
	case flow.Name == "":
		flow.Step = "name"
	case flow.Description == "":
		flow.Step = "description"
	case flow.ListPrice <= 0:
		flow.Step = "list"
	case flow.PriceNeedsConfirm && !flow.PriceConfirmed:
		// The gate stands here, inside the walk itself, before
		// the last price, the measure and the summary: a doubted
		// harvested price is confirmed in her own words first,
		// so it cannot ride through the remaining steps and onto
		// the shelf on the strength of a misread.
		flow.Step = "confirmprice"
		whatsapp.SendText(from, "One check before I go on. I read the price for "+flow.Name+" as "+money.GHS(flow.ListPrice)+", from your own words. Is that the right price? Say yes, or give me the right price.")
		return
	case flow.FloorPrice <= 0:
		flow.Step = "lastprice"
	case !flow.MeasureSettled && flow.BaseQty <= 0 && (flow.Unit == "" || divisibleUnit(flow.Unit)):
		// How the product is sold is settled smartly before it
		// is ever asked about. Her own words already settled it
		// when they carried a measure (the caption flows) or a
		// unit the draft judged certain (the photo flows). What
		// is left gets exactly one model judgement: certain of
		// a countable unit (a piece, a bottle) and the question
		// never exists; certain of a divisible measure and only
		// the piece size remains, asked in the measureqty
		// shape; genuinely untellable, and the seller is asked
		// the measure question, the one case that earns it.
		if flow.Unit == "" && !flow.UnitJudged {
			flow.UnitJudged = true
			// One composition call settles the unit and the
			// sizes together (see model.JudgeShape); the
			// sizes ride along so the save never judges twice.
			unit, certain, sizes := model.JudgeShape(flow.Name, strings.TrimSpace(flow.Description+" "+flow.SizeNote))
			if len(flow.Sizes) == 0 {
				flow.Sizes = sizes
			}
			if certain {
				flow.Unit = unit
				gotoNextOnboardingStep(from, flow)
				return
			}
		}
		if flow.Unit != "" {
			flow.Step = "measureqty"
			whatsapp.SendText(from, "Sold by the "+flow.Unit+". How much of it is the "+money.GHS(flow.ListPrice)+" price for? For example, 6 "+flow.Unit+"s.")
			return
		}
		flow.Step = "measure"
		whatsapp.SendText(from, "Do you sell the "+flow.Name+" by measure, like by the yard, litre or kilo, or only as a whole?")
		return
	default:
		if flow.PhotoCompact {
			savePhotoCompactListing(from, flow)
			return
		}
		enterOnboardingConfirm(from, flow)
		return
	}
	askOnboardingStep(from, flow)
}

// askOnboardingStep asks the question for the flow's current step,
// in the same words the guided walk has always used.
// onboardingQuestionText is the open question for the
// walk's plain guided steps, as one string: askOnboardingStep
// sends it, and the seller stage read reasons against it.
// Steps with their own specialised shapes (the intake gate,
// the duplicate choice, the piece size, the summary) return
// "" here and keep their own handling.
func onboardingQuestionText(flow *OnboardingFlow) string {
	switch flow.Step {
	case "name":
		return "What is it called?"
	case "description":
		return "Tell me about " + flow.Name + " in a few words: what it is, and what makes it worth buying. Customers will read this."
	case "list":
		return "What is the list price for " + flow.Name + "? This is the price customers see first. Give it in cedis, for example 450."
	case "lastprice":
		if flow.PhotoCompact {
			return "What is your last price? If non-negotiable, say 'non-negotiable'. If list price is wrong, correct like 'price is 500'."
		}
		return "Now, if a customer bargains, what is your last price for " + flow.Name + "? That is the lowest I can go. I will never sell below it, and the customer will never see it. If the price does not move at all, just say non-negotiable. If the list price itself is wrong, correct it like this: price is 500."
	case "measure":
		return "Do you sell the " + flow.Name + " by measure, like by the yard, litre or kilo, or only as a whole?"
	case "confirm":
		return "Shall I add it?"
	}
	return ""
}

func askOnboardingStep(from string, flow *OnboardingFlow) {
	if flow.Step == "confirm" {
		enterOnboardingConfirm(from, flow)
		return
	}
	if q := onboardingQuestionText(flow); q != "" {
		whatsapp.SendText(from, q)
	}
}

// enterOnboardingConfirm moves the add-a-product walk to its
// summary: everything she has given, the measure in plain words
// when she sells by one, and the margin she is handing the agent
// to haggle with. Nothing is saved until her clear yes at the
// confirm step, exactly as before measures existed.
func enterOnboardingConfirm(from string, flow *OnboardingFlow) {
	flow.Step = "confirm"
	summary := "Here is what will go live:\n" +
		flow.Name + ": " + flow.Description + "\n"
	if flow.Fixed {
		summary += "Price " + money.GHS(flow.ListPrice) + ", fixed. There is no bargaining on this one.\n"
	} else {
		summary += "Price " + money.GHS(flow.ListPrice) + ", your last price " + money.GHS(flow.FloorPrice) + " (customers never see your last price).\n"
	}
	if flow.Unit != "" && flow.BaseQty > 0 {
		probe := catalogue.Product{ListPrice: flow.ListPrice, FloorPrice: flow.FloorPrice, Fixed: flow.Fixed, Unit: flow.Unit, BaseQty: flow.BaseQty}
		summary += "Sold by the " + flow.Unit + ": the " + money.GHS(flow.ListPrice) + " price is for " + formatQty(flow.BaseQty) + " " + unitWord(flow.Unit, flow.BaseQty) + ", so " + money.GHS(probe.UnitPrice()) + " per " + flow.Unit + ", and buyers can take less.\n"
	}
	if !flow.Fixed {
		summary += "That leaves " + money.GHS(flow.ListPrice-flow.FloorPrice) + " I can give away when customers bargain, always inside your daily allowance.\n"
	}
	summary += "\nShall I add it?"
	sendYesNo(from, summary, "Yes, save it", "No, drop it")
}

// handleMerchantCommand runs one catalogue command outside the
// onboarding flow. It returns true when the message was a command,
// whether it succeeded or not, so the caller does not fall through to
// the day-question path.
var paymentSetupRe = regexp.MustCompile(`\bset up my payment\b|\bsetup my payment\b|\bpayment account\b`)
var paymentNameRe = regexp.MustCompile(`^payment name\s+(.+)$`)

// retryPaymentSetup (re)creates the shop's own Seev subaccount
// from what is already known: the holder name settled at signup,
// or a fresh read of the name on her MoMo number. It answers true
// when it handled the message. A payment problem never touches the
// shop itself; this is a side errand with a plain outcome.
func retryPaymentSetup(from string, merchant *catalogue.Merchant, lower string) bool {
	name := ""
	if m := paymentNameRe.FindStringSubmatch(lower); m != nil {
		name = strings.TrimSpace(m[1])
	} else if !paymentSetupRe.MatchString(lower) {
		return false
	}
	if merchant.SubaccountCode != "" && name == "" {
		whatsapp.SendText(from, "Your payment account is already set up. Money from your payment links settles to your MoMo in the name "+merchant.PayoutHolderName+".")
		return true
	}
	holder := merchant.PayoutHolderName
	if name != "" {
		holder = name
	}
	if holder == "" {
		if resolved, ok := checkout.ResolveAccount(merchant.PayoutPhone); ok {
			holder = resolved
		}
	}
	if holder == "" {
		whatsapp.SendText(from, "I could not read the name on the MoMo account for "+checkout.LocalNumber(merchant.PayoutPhone)+". Tell me the name like this: payment name Ama Serwaa.")
		return true
	}
	code, ok := checkout.CreateSubaccount(merchant.Name, merchant.PayoutPhone, holder)
	if !ok {
		whatsapp.SendText(from, "Seev could not set up the payment account this time either. Your shop is unaffected. We can try again whenever you say \"set up my payment\".")
		return true
	}
	_ = catalogue.SetSubaccount(merchant.ID, code, holder)
	whatsapp.SendText(from, "It is done. Your payment account is set up: money from your payment links settles to your MoMo in the name "+holder+", usually the next day. Of every sale you keep 99%; 1% stays with the platform.")
	return true
}

func handleMerchantCommand(from string, s *Session, merchant *catalogue.Merchant, text, lower string) bool {
	if payoutDetailsRe.MatchString(lower) {
		return offerPayoutDetailsFlow(from, s, merchant)
	}

	if myCatalogueRe.MatchString(lower) {
		lines := []string{"Here is your catalogue:"}
		for _, p := range merchant.Products {
			line := "- " + p.Name + ": list " + money.GHS(p.ListPrice)
			if p.ByMeasure() {
				line += " (" + money.GHS(p.UnitPrice()) + " per " + p.Unit + ", sold by the " + p.Unit + ")"
			}
			if p.Fixed {
				line += ", fixed price"
			} else {
				line += ", your last price " + money.GHS(p.FloorPrice)
			}
			switch {
			case p.Stock == 1:
				line += ", only 1 in stock"
			case p.Stock > 1:
				line += ", " + strconv.Itoa(p.Stock) + " in stock"
			case p.Stock == 0:
				line += ", none in stock"
			}
			lines = append(lines, line)
		}
		lines = append(lines, "Your daily giveaway allowance is "+money.GHS(merchant.DailyGiveawayAllowance)+". Your last prices are yours only; customers never see them.")
		if deliveryStyleOf(merchant) == "quote" {
			line := "Delivery: you quote the fee for each order as it comes, and the buyer sees it inside her full total before she pays."
			if merchant.DeliveryNote != "" {
				line += " " + merchant.DeliveryNote
			}
			lines = append(lines, line)
			if deliveryPaidToOf(merchant) == "merchant" {
				lines = append(lines, "The delivery money comes to you, inside the total your buyers pay here.")
			} else {
				lines = append(lines, "The delivery money is not yours: your buyers pay whoever brings the item, when it arrives.")
			}
		} else if merchant.DeliveryFee > 0 || merchant.DeliveryNote != "" || len(merchant.DeliveryAreas) > 0 {
			line := "Delivery:"
			if merchant.DeliveryFee > 0 {
				line += " " + money.GHS(merchant.DeliveryFee) + " a trip."
			}
			if merchant.DeliveryNote != "" {
				line += " " + merchant.DeliveryNote
			}
			lines = append(lines, line)
			if len(merchant.DeliveryAreas) > 0 {
				parts := make([]string, 0, len(merchant.DeliveryAreas))
				for _, a := range merchant.DeliveryAreas {
					parts = append(parts, a.Area+" "+money.GHS(a.Fee))
				}
				lines = append(lines, "Area fees: "+strings.Join(parts, ", ")+". Everywhere else pays the standard fee.")
			}
			if deliveryPaidToOf(merchant) == "merchant" {
				lines = append(lines, "The delivery money comes to you, inside the total your buyers pay here.")
			} else {
				lines = append(lines, "The delivery money is not yours: your buyers pay whoever brings the item, when it arrives.")
			}
		} else {
			lines = append(lines, "No delivery fee set yet. Say \"set delivery fee 25\" to give buyers one total to pay, \"set area fee Osu 30\" where a place costs different, or \"set delivery per order\" and I will ask you for the fee on each delivery.")
		}
		if merchant.SubaccountCode != "" {
			lines = append(lines, "Your payment account is set up: link money settles to your MoMo in the name "+merchant.PayoutHolderName+".")
		}
		whatsapp.SendText(from, strings.Join(lines, "\n"))
		return true
	}

	if m := setDeliveryFeeRe.FindStringSubmatch(lower); m != nil {
		amount := parseOffer(m[1])
		if amount == nil {
			whatsapp.SendText(from, "I could not read the fee in that. Try: set delivery fee 25.")
			return true
		}
		if err := catalogue.SetDeliveryFee(merchant.ID, *amount); err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		if *amount == 0 {
			whatsapp.SendText(from, "Done. There is no delivery fee now; buyers pay just the item price, and delivery is arranged with you.")
		} else {
			whatsapp.SendText(from, "Done. Delivery now costs your buyers "+money.GHS(*amount)+" a trip, on top of the item price. They see one total before they pay.")
		}
		if *amount > 0 && deliveryStyleOf(merchant) == "quote" {
			whatsapp.SendText(from, "You are quoting delivery per order just now, so this fee is not being used. Say \"set delivery up front\" to charge this fee on every delivery instead.")
		}
		return true
	}

	if setDeliveryQuoteRe.MatchString(strings.TrimSpace(text)) {
		if err := catalogue.SetDeliveryStyle(merchant.ID, "quote"); err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. Now, whenever a buyer chooses delivery, I will ask you for that order's fee and show it to her inside her full total before she pays anything. If you do not answer within a few minutes, she can choose pickup or meet-up instead, so she is never left waiting.")
		return true
	}

	if setDeliverySetRe.MatchString(strings.TrimSpace(text)) {
		if err := catalogue.SetDeliveryStyle(merchant.ID, "set"); err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. Your delivery fees are the ones you set up front: your standard fee, and your area fees where you have set them. Buyers see the fee for their place inside their total before they pay.")
		return true
	}

	if m := setAreaFeeRe.FindStringSubmatch(text); m != nil {
		areas := parseAreaFees(m[1])
		if len(areas) == 0 {
			whatsapp.SendText(from, "I could not read the areas in that. Try: set area fee Osu 30, or set area fees Osu 30, Kasoa 45.")
			return true
		}
		for _, a := range areas {
			if err := catalogue.SetDeliveryArea(merchant.ID, a.Area, a.Fee); err != nil {
				whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
				return true
			}
		}
		wasQuote := deliveryStyleOf(merchant) == "quote"
		if wasQuote {
			_ = catalogue.SetDeliveryStyle(merchant.ID, "set")
		}
		parts := make([]string, 0, len(areas))
		for _, a := range areas {
			parts = append(parts, a.Area+" "+money.GHS(a.Fee))
		}
		line := "Done. Delivery to " + strings.Join(parts, ", ") + " now carries its own fee. Everywhere else pays your standard fee."
		if wasQuote {
			line += " You were quoting per order; your fees are now the ones set up front."
		}
		whatsapp.SendText(from, line)
		return true
	}

	if deliveryToMeRe.MatchString(lower) {
		if err := catalogue.SetDeliveryPaidTo(merchant.ID, "merchant"); err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. The delivery money now comes to you: it sits inside the one total your buyers pay here, and I will tell them so.")
		return true
	}

	if deliveryToCarrierRe.MatchString(lower) {
		if err := catalogue.SetDeliveryPaidTo(merchant.ID, "carrier"); err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. Your buyers now pay the delivery money to whoever brings the item, when it arrives. What comes to you here is the item money only.")
		return true
	}

	if m := setDeliveryNoteRe.FindStringSubmatch(text); m != nil {
		note := strings.TrimSpace(m[1])
		if err := catalogue.SetDeliveryNote(merchant.ID, note); err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. When buyers ask about delivery, they will hear: "+note)
		return true
	}

	if wantsListRe.MatchString(controlWord(lower)) {
		list := wants.Default.List(merchant.ID)
		if len(list) == 0 {
			whatsapp.SendText(from, "No buyer wants written down yet. When a buyer asks for something you do not stock, I keep it here for you.")
			return true
		}
		lines := []string{"Here is what buyers have asked for that you do not stock:"}
		for _, w := range list {
			line := "- \"" + w.Text + "\" from " + w.Buyer + " (" + w.Date + ")"
			if w.Alerted {
				line += " - told them when it landed"
			}
			lines = append(lines, line)
		}
		lines = append(lines, "Stock any of these and I will alert the buyer who asked, there and then.")
		whatsapp.SendText(from, strings.Join(lines, "\n"))
		return true
	}

	if m := changePriceRe.FindStringSubmatch(lower); m != nil {
		amount := parseOffer(m[2])
		if amount == nil {
			whatsapp.SendText(from, "I could not read the new price in that. Try: change price of <name> to 500.")
			return true
		}
		product, err := catalogue.UpdateListPrice(merchant.ID, strings.TrimSpace(m[1]), *amount)
		if err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		if product.Fixed {
			whatsapp.SendText(from, "Done. "+product.Name+" now lists at "+money.GHS(product.ListPrice)+", and it is still fixed price.")
		} else {
			whatsapp.SendText(from, "Done. "+product.Name+" now lists at "+money.GHS(product.ListPrice)+". Its last price stays "+money.GHS(product.FloorPrice)+".")
		}
		return true
	}

	if m := changeFloorRe.FindStringSubmatch(lower); m != nil {
		amount := parseOffer(m[2])
		if amount == nil {
			whatsapp.SendText(from, "I could not read the new last price in that. Try: change last price of <name> to 500.")
			return true
		}
		product, err := catalogue.UpdateFloorPrice(merchant.ID, strings.TrimSpace(m[1]), *amount)
		if err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		if product.Fixed {
			whatsapp.SendText(from, "Done. "+product.Name+" is now fixed at "+money.GHS(product.ListPrice)+": there is no bargaining on it.")
		} else {
			whatsapp.SendText(from, "Done. The last price for "+product.Name+" is now "+money.GHS(product.FloorPrice)+". Its price stays "+money.GHS(product.ListPrice)+".")
		}
		return true
	}

	if myOrdersRe.MatchString(lower) {
		whatsapp.SendText(from, merchantOrdersText(merchant))
		return true
	}

	if m := cancelOrderRe.FindStringSubmatch(lower); m != nil {
		cancelMerchantOrder(from, merchant, strings.ToUpper(m[1]))
		return true
	}

	if m := setStockRe.FindStringSubmatch(lower); m != nil {
		count, convErr := strconv.Atoi(m[2])
		if convErr != nil {
			whatsapp.SendText(from, "I could not read the count in that. Try: set stock of <name> to 5.")
			return true
		}
		product, err := catalogue.SetStock(merchant.ID, strings.TrimSpace(m[1]), count)
		if err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		switch {
		case count == 0:
			whatsapp.SendText(from, "Done. "+product.Name+" is marked as none in stock.")
		case count == 1:
			whatsapp.SendText(from, "Done. "+product.Name+" now has 1 in stock. With only one, I will confirm you have it in hand before each sale.")
		default:
			whatsapp.SendText(from, "Done. "+product.Name+" now has "+strconv.Itoa(count)+" in stock.")
		}
		return true
	}

	if m := setMeasureRe.FindStringSubmatch(lower); m != nil {
		q, ok := parseQuantity(m[2])
		if !ok {
			whatsapp.SendText(from, "I could not read the measure in that. Try: set measure of <name> to 6 yards.")
			return true
		}
		product, err := catalogue.SetMeasure(merchant.ID, strings.TrimSpace(m[1]), q.Unit, q.Qty)
		if err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. "+product.Name+" is now sold by the "+product.Unit+": the "+money.GHS(product.ListPrice)+" price is for "+formatQty(product.BaseQty)+" "+unitWord(product.Unit, product.BaseQty)+", so "+money.GHS(product.UnitPrice())+" per "+product.Unit+". Buyers can take less, priced from the same rate.")
		return true
	}

	if m := wholeOnlyRe.FindStringSubmatch(lower); m != nil {
		name := strings.TrimSpace(m[1])
		product := catalogue.FindByName(merchant.ID, name)
		if product == nil {
			whatsapp.SendText(from, "I cannot find \""+name+"\" in your catalogue. Say \"my catalogue\" to see what is there.")
			return true
		}
		updated, err := catalogue.ClearMeasure(merchant.ID, product.Name)
		if err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. "+updated.Name+" is sold whole only now: "+money.GHS(updated.ListPrice)+" as it comes, and I will not split it.")
		return true
	}

	if setAllowanceRe.MatchString(lower) {
		// The amount is the figure that belongs to the command:
		// the one after the word allowance, read last, so a
		// figure quoted from elsewhere in her message (her own
		// figures read back at her stand at the front of such a
		// line) is never mistaken for the amount she is setting.
		amount := parseOffer(text)
		if idx := strings.LastIndex(lower, "allowance"); idx >= 0 {
			if after := parseOffer(text[idx:]); after != nil {
				amount = after
			}
		}
		if amount == nil {
			whatsapp.SendText(from, "I could not read the amount in that. Try: set allowance 400.")
			return true
		}
		if err := catalogue.SetAllowance(merchant.ID, *amount); err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. Your daily giveaway allowance is now "+money.GHS(*amount)+". Every discount I agree comes out of it, and I haggle firmer as it runs down.")
		return true
	}

	if m := removeRe.FindStringSubmatch(lower); m != nil {
		name := strings.TrimSpace(m[1])
		product := catalogue.FindByName(merchant.ID, name)
		if product == nil {
			whatsapp.SendText(from, "I cannot find \""+name+"\" in your catalogue. Say \"my catalogue\" to see what is there.")
			return true
		}
		s.PendingRemove = product.Name
		sendYesNo(from, "Remove "+product.Name+" from your catalogue? Customers will no longer be offered it.", "Yes, remove it", "No, keep it")
		return true
	}

	return false
}

// explainRe reads a seller asking about a thing rather than
// commanding it: "explain this", "I don't understand", "what
// does the allowance mean". Such a message is a question even
// when it carries a command's words or figures inside it,
// most often because she has quoted the agent's own line back.
var explainRe = regexp.MustCompile(`\b(explain|i don'?t understand|do not understand|not clear|what does|what is (this|that|it|the allowance))\b`)

// dayAskRe reads a plain question about the trading day: how
// today is going, today's sales or figures, the day book. It
// is deliberately narrow, so a question that merely carries
// the word today ("did anyone pay today?") still reaches the
// thinking, which can see her orders.
var dayAskRe = regexp.MustCompile(`^(today[.!?]?|how is today|how'?s today|how is the day|today going|how are (we|sales) doing|today'?s (sales|figures|book)|my day|the day so far)[.!?]?$|^(how is today going|how is business today)[.!?]?$`)

// openWalkFromAdvert reads a whole pasted advert into an
// onboarding walk: the name, the description and the marked
// price are separated out of her one message, told back to
// her, and the walk is moved to the first question the advert
// did not answer. It serves two doors alike: the name step of
// a walk she already started, and a merchant who pastes an
// advert with no walk open at all, where the paste itself is
// the instruction to list it. It returns false when the text
// is not an advert, and the caller carries on as before.
// flowSizes settles a listing's sizes at save: the ones the
// listing's own reads already produced (a photo draft's, a
// throw-in's), or one text judgement over the seller's name
// and description when none did. No sizes anywhere is a
// plain outcome, not an error: most goods have none.
func flowSizes(flow *OnboardingFlow) []string {
	if len(flow.Sizes) > 0 {
		return flow.Sizes
	}
	_, _, sizes := model.JudgeShape(flow.Name, strings.TrimSpace(flow.Description+" "+flow.SizeNote))
	return sizes
}

func openWalkFromAdvert(from string, s *Session, flow *OnboardingFlow, text string) bool {
	draft, note, isAdvert := parsePastedAdvert(text)
	if !isAdvert {
		return false
	}
	flow.Name = draft.Name
	if draft.ListPrice > 0 {
		flow.ListPrice = draft.ListPrice
		flow.PriceNeedsConfirm = draft.PriceDoubted
		flow.PriceConfirmed = false
	}
	if draft.Description != "" {
		flow.Description = draft.Description
	}
	if draft.Unit != "" {
		flow.Unit, flow.BaseQty = draft.Unit, draft.BaseQty
	}
	if len(draft.Sizes) > 0 {
		flow.Sizes = draft.Sizes
	}
	if note != "" {
		if flow.Description != "" {
			flow.Description += "\n" + note + "."
		} else {
			flow.SizeNote = note
		}
	}
	if enterDupChoice(from, s, flow) {
		return true
	}
	var read []string
	if draft.ListPrice > 0 {
		read = append(read, "I took the price from your message: "+money.GHS(draft.ListPrice)+".")
	}
	if draft.Name != "" && draft.Description != "" {
		read = append(read, "I have read it as \""+draft.Name+"\", with the details under it as the description.")
	} else if draft.Name == "" && draft.Description != "" {
		read = append(read, "I have taken your words as the description.")
	}
	if note != "" {
		read = append(read, "I have noted the size, "+strings.TrimPrefix(note, "Size ")+", with the description.")
	}
	if len(read) > 0 {
		whatsapp.SendText(from, strings.Join(read, " "))
	}
	gotoNextOnboardingStep(from, flow)
	return true
}

// divisibleUnit reports whether a unit names something that
// is measured out of a larger whole (a yard off a piece, a
// litre out of a drum), as against a countable unit (a piece,
// a bottle, a pair) that is sold whole per count. It is the
// semantics of the unit word itself, not a judgement about
// any product: which products take which unit is the
// model's call, or the seller's own words.
func divisibleUnit(unit string) bool {
	switch strings.TrimSuffix(unit, "s") {
	case "yard", "metre", "meter", "litre", "liter", "kilo", "kilogram", "kg", "gram", "g", "tonne", "gallon", "foot", "inch":
		return true
	}
	return false
}

// savePhotoCompactListing saves a photo listing the moment
// its last price lands, and answers with the one saved line
// and nothing after it: no summary to confirm, no allowance
// note, no further questions. Buyers who were waiting on the
// product are still told, as with any save.
func savePhotoCompactListing(from string, flow *OnboardingFlow) {
	var s *Session
	for _, candidate := range sessions {
		if candidate.Onboarding == flow {
			s = candidate
			break
		}
	}
	if s == nil {
		return
	}
	saveName, saveDesc := applyColourRule(flow, flow.Name, flow.Description)
	product, err := catalogue.AddProduct(s.MerchantID, catalogue.Product{
		Name:        saveName,
		Description: saveDesc,
		ListPrice:   flow.ListPrice,
		FloorPrice:  flow.FloorPrice,
		Fixed:       flow.Fixed,
		Stock:       1,
		Unit:        flow.Unit,
		BaseQty:     flow.BaseQty,
		Sizes:       flowSizes(flow),
	})
	s.Onboarding = nil
	if err != nil {
		whatsapp.SendText(from, "I could not save it: "+err.Error()+". Nothing went live.")
		return
	}
	attachFlowPhotos(s.MerchantID, product, flow)
	unit := flow.Unit
	if unit == "" {
		unit = "piece"
	}
	whatsapp.SendText(from, "Saved ✅ "+product.Name+" | List: "+money.GHS(product.ListPrice)+" | Last: "+money.GHS(product.FloorPrice)+" | Unit: "+unit)
	alertWaitingBuyers(s.MerchantID, product)
	announceListing(from, s.MerchantID, product)
}
