package agent

// Question ownership on the merchant's side of the counter.
//
// One pending question owns the merchant's next answer. When
// any question is awaiting her — a confirmation, a
// disambiguation, an either/or — her next message is resolved
// against THAT question before any listing-walk step, stage
// reader or other flow may read it, and the turn that opens a
// question sends no other flow's question alongside it: one
// message, one question. The live failure this answers: the
// agent asked, in one turn, whether she wanted the kept
// photos or the listing cleared AND pushed the walk's list
// price question again; her answers ("Both", "Clear", "Yes
// clear") were then eaten by the price step, which could not
// read them as prices, and the clear never happened.
//
// A quoted reply binds to the question it quotes: the
// question's exact words are kept when it is armed, and a
// turn whose resolved quote (see quotes.go) is that question
// is resolved against it whatever else is open — the same
// binding the dispatch reminder's quote carries (see
// merchantMarkSent), generalised to the merchant's questions.
//
// Two kinds of question arm here. A structured question
// ("clearwork") is asked by a flow with a definite act behind
// it: its answers are read inside its own frame — plain
// shapes first, the model's reading (the seller walk reader)
// for the rest — and it never falls through to another flow:
// it executes, it resumes what she chose to keep, or it is
// asked again. A drafted question ("drafted") is a clarifying
// question a thinking draft asked in an act's reply: it owns
// the next turn's first reading (her words go to the thinking
// against it before any walk step may consume them), and
// only when that reading declines does the message fall
// through to the ordinary dispatch, the question spent.
// The reading is GIVEN the question (see
// understandMerchantMessage): a reading that meets her
// answer cold has nothing to bind it to, and the live
// failure that taught this was a "yes" owed to a catalogue
// question landing on the allowance clarifier, because the
// unset allowance was the loudest fact in a reading that
// never saw the question. A choice she picks inside the
// frame is driven to the same action the platform performs
// when she names that choice outright — her catalogue for
// a "show it" choice, the add path for an "add" choice —
// so the frame settles the matter, not merely discusses
// it.

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// MerchantQuestion is one question awaiting the merchant's
// answer. Body is the question's exact words as sent, so a
// reply quoting it binds by its words the way a quote of the
// allowance note binds (see handleMerchantMessageCtx). The
// remaining fields are the clearwork question's targets,
// named as they stood when the question was armed.
type MerchantQuestion struct {
	Kind        string // "clearwork" or "drafted"
	Body        string
	Listing     bool
	ListingName string
	KeptPhotos  int
	KeptItems   int
}

// noteMerchantActQuestion arms a question an act's drafted
// reply has just asked as the pending merchant question, so
// the merchant's next message is read against it first (see
// servePendingMerchantQuestion). Only the ask and unclear
// acts arm one, and only when the drafted reply itself ends
// in a question: a plain answer arms nothing, and the flow
// that was interrupted keeps its own courtesy of re-asking
// its step under a plain answer — but never under a question,
// which would put two live questions in front of her at once.
func noteMerchantActQuestion(s *Session, u *understood) {
	if s == nil || u == nil || s.PendingMerchantQ != nil {
		return
	}
	if u.Intent != intentAsk && u.Intent != intentUnclear {
		return
	}
	armDraftedQuestion(s, strings.TrimSpace(u.Reply))
}

// armDraftedQuestion registers a question the agent has
// just sent the merchant as the pending drafted question,
// when it is a question and none already stands: every
// question the agent puts to her is registered, so her
// next message is resolved inside its frame (see
// servePendingMerchantQuestion) and never read cold.
func armDraftedQuestion(s *Session, body string) {
	if s == nil || s.PendingMerchantQ != nil {
		return
	}
	if !strings.HasSuffix(body, "?") {
		return
	}
	s.PendingMerchantQ = &MerchantQuestion{Kind: "drafted", Body: body}
}

// servePendingMerchantQuestion resolves the merchant's turn
// against the pending question, when one stands. It reports
// whether the turn was the question's to settle. A reply
// quoting the question binds to it outright; otherwise a bare
// typed yes or no owed to a different armed button question
// keeps that question's ownership (the armed-question rule in
// handleMerchantMessageCtx runs first for those words), and
// the pending question stands for her next message.
func servePendingMerchantQuestion(from string, s *Session, merchant *catalogue.Merchant, merchantID, text, lower string, think func() *understood) bool {
	q := s.PendingMerchantQ
	if q == nil {
		return false
	}
	quoted := false
	if tq := turnQuoteOf(from); tq != nil && tq.Known && tq.Author == "agent" && strings.TrimSpace(tq.Body) == strings.TrimSpace(q.Body) {
		quoted = true
	}
	if !quoted && s.YesNoQ != nil && (isClearYes(lower) || isClearNo(lower)) {
		return false
	}
	switch q.Kind {
	case "clearwork":
		return resolveClearWorkQuestion(from, s, merchant, q, text, lower)
	case "drafted":
		// A taught command in its exact shape is never a
		// drafted question's answer: the question is spent
		// and the command is served by the ordinary
		// dispatch below, as an open walk's commands are.
		if walkCommandOutranks(text) {
			s.PendingMerchantQ = nil
			return false
		}
		if u := think(); u != nil {
			// A choice she picks inside the frame is
			// driven to the action that choice names
			// outright elsewhere in the platform: seeing
			// her catalogue is the catalogue the "my
			// catalogue" command serves, and choosing to
			// add opens the add path — from the goods in
			// her own message when it carries them, else
			// at the walk's first question, the answer
			// the taught "add a product" gets. Read
			// without the question, these same words fell
			// out of the frame entirely: a "new" owed to
			// an add-or-update question was answered with
			// a fresh greeting.
			if u.Intent == intentShelf {
				s.PendingMerchantQ = nil
				serveMerchantCatalogue(from, merchant)
				return true
			}
			if u.Intent == intentNewListing && s.Onboarding == nil {
				s.PendingMerchantQ = nil
				flow := &OnboardingFlow{Step: "name"}
				s.Onboarding = flow
				if openWalkFromAdvert(from, s, flow, text) {
					return true
				}
				s.Onboarding = flow
				whatsapp.SendText(from, "Let us add a new product. What is it called? You can say cancel at any point and nothing will be saved.")
				return true
			}
			if actOnMerchantUnderstanding(from, merchant, merchantID, text, u) {
				s.PendingMerchantQ = nil
				noteMerchantActQuestion(s, u)
				return true
			}
		}
		// The frame's own reading declines: the question
		// is spent and her words fall through to the
		// ordinary dispatch untouched, the walk included —
		// she may be answering the walk's question after
		// all, and ownership must not strand her figure.
		s.PendingMerchantQ = nil
		return false
	}
	return false
}

// merchantClearWorkRe reads the merchant's word to clear her
// standing unfinished work in its plain shapes: a clearing
// verb against the whole standing set, or against one named
// part of it. It is the merchant side of the discipline the
// buyer's basket clear is read with (see basketClearRe): the
// canonical shapes act at once, and the model's reading of
// the same instruction in her other words (the clear_work
// act) opens the same confirmation.
var merchantClearWorkRe = regexp.MustCompile(`^(please\s+)?(clear|drop|delete|forget|remove)\s+(them|it|that|these|those|all|everything|all of them|all of it|the photos?|the kept photos?|the listing|my listing)\b`)

// serveMerchantClearAsk opens the clear confirmation when
// the merchant's message is her word to clear her standing
// work and there is standing work to clear. It reports
// whether it served the turn.
func serveMerchantClearAsk(from string, s *Session, merchant *catalogue.Merchant, text, lower string) bool {
	if !merchantClearWorkRe.MatchString(lower) {
		return false
	}
	return openClearWorkConfirmation(from, s)
}

// clearWorkStanding names the merchant's standing unfinished
// work a clear could take: the listing she is mid-way
// through, the photos kept waiting to be listed, the items
// kept from her photos waiting for their prices.
func clearWorkStanding(s *Session) (listing bool, listingName string, photos, items int) {
	if s == nil {
		return false, "", 0, 0
	}
	if s.Onboarding != nil {
		listing, listingName = true, s.Onboarding.Name
	}
	return listing, listingName, len(s.BurstHeld), len(s.BurstBatch)
}

func keptPhotoPhrase(n int) string {
	if n == 1 {
		return "1 kept photo waiting to be listed"
	}
	return strconv.Itoa(n) + " kept photos waiting to be listed"
}

func keptItemPhrase(n int) string {
	if n == 1 {
		return "1 kept item from your photos waiting for its last price"
	}
	return strconv.Itoa(n) + " kept items from your photos waiting for their last prices"
}

func joinAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
}

// openClearWorkConfirmation asks the merchant, once, whether
// her standing work is to be cleared or carried on, naming
// every part of it, and arms the question as the pending
// one: her next answer is read inside this frame and no
// other flow asks her anything in the same turn — the walk
// included, whose question waits until this one resolves.
// It reports whether there was standing work to ask about.
func openClearWorkConfirmation(from string, s *Session) bool {
	listing, listingName, photos, items := clearWorkStanding(s)
	if !listing && photos == 0 && items == 0 {
		return false
	}
	var parts []string
	if listing {
		if listingName != "" {
			parts = append(parts, "the "+listingName+" listing you are making")
		} else {
			parts = append(parts, "the listing you are making")
		}
	}
	if photos > 0 {
		parts = append(parts, keptPhotoPhrase(photos))
	}
	if items > 0 {
		parts = append(parts, keptItemPhrase(items))
	}
	q := &MerchantQuestion{
		Listing: listing, ListingName: listingName,
		KeptPhotos: photos, KeptItems: items,
		Kind: "clearwork",
	}
	if len(parts) > 1 {
		q.Body = "Shall I clear them? Standing now: " + joinAnd(parts) + ". Say clear to clear everything named, name one part to clear only that, or say continue and we carry on where we left off."
	} else {
		q.Body = "Shall I clear " + parts[0] + "? Say clear, or say continue and we carry on where we left off."
	}
	s.PendingMerchantQ = q
	sessions.put(sessionKey(s.MerchantID, from), s)
	whatsapp.SendText(from, q.Body)
	return true
}

// clearWorkContinueRe reads her word to carry on, in the
// shapes that stand as the whole of an answer to the clear
// question: a continue or carry-on at the head of the
// message, or one of the plain keeping words alone.
var clearWorkContinueRe = regexp.MustCompile(`^(please\s+)?(continue|carry on)\b`)

// clearWorkAllRe reads her word to clear everything the
// question named, in its plain whole-message shapes: the
// clearing verb alone or against the whole set, a yes
// carrying the verb, or the set words alone ("both", "all").
var clearWorkAllRe = regexp.MustCompile(`^((yes|yeah)\s+)?(clear|drop|delete|forget)(\s+(them|it|that|all|both|everything|all of them|all of it))?[.!]?$|^yes\s+(clear|drop|delete)[.!]?$`)

// resolveClearWorkQuestion reads the merchant's answer
// inside the clear question's own frame and settles it: a
// part she names is cleared alone, the whole set is cleared
// on her clear word, and a word to carry on clears nothing
// and hands the turn back to what she kept — the open walk
// asks its standing question again, now as the only live
// question. An answer the plain shapes cannot read is read
// by the thinking inside the same frame; one it cannot read
// either is met with the same question again, never with
// another flow's question and never with a price reader's
// refusal. It always reports the turn served: while this
// question stands, no other reader may have the message.
func resolveClearWorkQuestion(from string, s *Session, merchant *catalogue.Merchant, q *MerchantQuestion, text, lower string) bool {
	if clearWorkContinueRe.MatchString(lower) {
		return continueAfterClearQuestion(from, s)
	}
	switch controlWord(lower) {
	case "no", "keep", "keep them", "keep it", "keep going", "not now", "leave it", "leave them", "leave everything":
		return continueAfterClearQuestion(from, s)
	}
	mentionsPhotos := strings.Contains(lower, "photo")
	mentionsItems := strings.Contains(lower, "item")
	mentionsListing := strings.Contains(lower, "listing") ||
		(q.ListingName != "" && strings.Contains(lower, strings.ToLower(q.ListingName)))
	clearListing, clearPhotos, clearItems := false, false, false
	switch {
	case mentionsPhotos && mentionsListing:
		clearListing, clearPhotos = q.Listing, q.KeptPhotos > 0
	case clearWorkAllRe.MatchString(lower) || controlWord(lower) == "both" || controlWord(lower) == "all" ||
		controlWord(lower) == "everything" || controlWord(lower) == "all of them" || controlWord(lower) == "both of them" ||
		isClearYes(lower):
		clearListing, clearPhotos, clearItems = q.Listing, q.KeptPhotos > 0, q.KeptItems > 0
	case mentionsPhotos && q.KeptPhotos > 0:
		clearPhotos = true
	case mentionsItems && q.KeptItems > 0:
		clearItems = true
	case mentionsListing && q.Listing:
		clearListing = true
	default:
		// The model's reading, inside this question's
		// frame alone: a call-off or a plain assent is
		// her word to clear, a question about the
		// question is answered and the question asked
		// again, and anything else leaves the question
		// standing, asked again as it was.
		reading := thinkSellerWalkIntent(from, s, "deciding whether to clear her standing work", q.Body, "", text, nil)
		switch reading.Intent {
		case stageIntentCancel, stageIntentAnswer:
			clearListing, clearPhotos, clearItems = q.Listing, q.KeptPhotos > 0, q.KeptItems > 0
		case stageIntentAsk:
			if reading.Reply != "" && voiceClean(reading.Reply) && figuresClean(reading.Reply) {
				whatsapp.SendText(from, reading.Reply)
				whatsapp.SendText(from, q.Body)
				return true
			}
			whatsapp.SendText(from, q.Body)
			return true
		default:
			whatsapp.SendText(from, q.Body)
			return true
		}
	}
	executeClearWork(from, s, clearListing, clearPhotos, clearItems)
	return true
}

// continueAfterClearQuestion settles the clear question with
// nothing cleared: the question is disarmed, she is told
// plainly that everything stands, and the work she kept
// resumes — an open listing walk asks its standing question
// again, as the turn's one question.
func continueAfterClearQuestion(from string, s *Session) bool {
	s.PendingMerchantQ = nil
	sessions.put(sessionKey(s.MerchantID, from), s)
	whatsapp.SendText(from, "Alright, nothing is cleared.")
	if s.Onboarding != nil {
		askOnboardingStep(from, s.Onboarding)
	}
	return true
}

// executeClearWork carries out a settled clear: the parts
// she chose go — the listing dropped unsaved, the kept
// photos gone, the kept items gone — and she hears what was
// cleared in one line, from what actually stood.
func executeClearWork(from string, s *Session, clearListing, clearPhotos, clearItems bool) {
	s.PendingMerchantQ = nil
	var parts []string
	dropped := false
	if clearListing && s.Onboarding != nil {
		name := s.Onboarding.Name
		s.Onboarding = nil
		s.clearYesNo("listing")
		dropped = true
		if name != "" {
			parts = append(parts, "the "+name+" listing is dropped")
		} else {
			parts = append(parts, "the listing you were making is dropped")
		}
	}
	if clearPhotos && len(s.BurstHeld) > 0 {
		n := len(s.BurstHeld)
		s.BurstHeld = nil
		if n == 1 {
			parts = append(parts, "the kept photo is gone")
		} else {
			parts = append(parts, "the "+strconv.Itoa(n)+" kept photos are gone")
		}
	}
	if clearItems && len(s.BurstBatch) > 0 {
		n := len(s.BurstBatch)
		s.BurstBatch = nil
		s.BurstAwaiting = false
		if n == 1 {
			parts = append(parts, "the kept item from your photos is gone")
		} else {
			parts = append(parts, "the "+strconv.Itoa(n)+" kept items from your photos are gone")
		}
	}
	sessions.put(sessionKey(s.MerchantID, from), s)
	if len(parts) == 0 {
		whatsapp.SendText(from, "That work is no longer standing, so there is nothing to clear.")
		return
	}
	line := "Cleared: " + joinAnd(parts) + "."
	if dropped {
		line += " Nothing was saved."
	}
	whatsapp.SendText(from, line)
}
