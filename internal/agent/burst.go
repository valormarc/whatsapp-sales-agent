package agent

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// A burst batch: the products a seller forwarded while one
// listing was already open. Sellers do not send stock one
// photo at a time; they forward a whole range at once, each
// photo captioned with its price. Dropping every photo after
// the first lost most of her stock, and walking her through
// each one separately would take the whole morning. So a
// photo that reads as a different product, with its name
// and price, is kept here as its own item with every detail
// its read carried — name, description, sizes, how it is
// sold, its colours, its photos. When the open listing
// finishes, the batch is presented in one message and she
// prices the whole batch in one reply; the items then save
// together, each with its own details and its own photos.
// A photo whose read is incomplete (no name, or no price)
// cannot be batched: there is nothing truthful to present,
// and it keeps the older one-at-a-time road.

// burstItem is one kept product of a burst, as read from its
// photo and caption. In the batch it waits only for its last
// price; in the held queue it may also be waiting for its
// identity or its price to be settled in its own listing,
// and Caption is kept so that listing can re-read the
// seller's own words when it opens.
type burstItem struct {
	Name         string
	Description  string
	ListPrice    float64
	PriceDoubted bool
	Unit         string
	BaseQty      float64
	Sizes        []string
	Colours      []string
	PhotoRefs    []string
	Caption      string
	// VisionDesc is what the model's own read of this
	// item's photo said it is — construction detail the
	// seller's caption shorthand may not carry. It is the
	// reference a later photo's identity verdict is judged
	// against: a caption line like "with gold studs" names
	// one shared feature and two different designs can
	// both match it, while the read of the item's own
	// photo says how the thing is actually made.
	VisionDesc string
}

// burstIdentityVerdict asks the model's eyes whether the
// photo in hand is the same product as a kept item, judged
// against that item's own name and description — the same
// against-judgement a photo gets against an open listing.
// It returns "same", "different" or "unsure" when the
// model judged, and "" when there was no photo to judge or
// no model read. Only a positive "same" verdict folds (see
// addToBurst): two different products can share a name and
// a price (two rack designs both read as "Metal Rack" at
// GH₵270), and folding those on words alone keeps the
// first and loses the second's details entirely.
// burstFoldOK decides whether a new photo folds into a
// kept burst item. An explicit "different" verdict is the
// strongest identity signal there is and is never
// overridden: the third audit found fact equality (same
// description, price, unit) folding photos the judge had
// called different — two similar products collapsed into
// one listing. Fact equality folds only the UNJUDGED
// case, where no verdict exists to override.
func burstFoldOK(dataURI, caption string, item burstItem, draft throwDraft, turn string) bool {
	switch burstIdentityVerdict(dataURI, caption, item, turn) {
	case "same":
		return true
	case "different":
		return false
	}
	// No verdict — the photo could not be judged, or the
	// model was unsure — is not an identity. Folding on
	// caption fact equality alone merged unjudged photos
	// into a kept item they may never have belonged to
	// (the fifth audit); an unjudged photo stands as its
	// own item, as the burst's contract promises, and
	// the seller separates or merges it with her own
	// eyes on the batch.
	return false
}

func burstIdentityVerdict(dataURI, caption string, item burstItem, turn string) string {
	if dataURI == "" || item.Name == "" {
		return ""
	}
	reference := item.VisionDesc
	if reference == "" {
		reference = item.Description
	}
	md := draftListingFn(dataURI, caption, item.Name, reference, turn)
	if md == nil {
		return ""
	}
	return md.SameAsOpen
}

// sameBurstFacts reports whether a fresh draft carries
// exactly the kept item's facts — same description, price,
// unit, base quantity and sizes. Folding an exact
// re-read discards nothing: there is no second product's
// distinct fact to lose, which is the harm the positive
// verdict requirement guards. Any difference at all, and
// only the model's "same" verdict can fold the photo.
func sameBurstFacts(item burstItem, draft throwDraft) bool {
	if strings.TrimSpace(item.Description) != strings.TrimSpace(draft.Description) ||
		item.ListPrice != draft.ListPrice ||
		item.Unit != draft.Unit || item.BaseQty != draft.BaseQty {
		return false
	}
	// Colours are not compared: a colour is a variant of
	// the product, and the fold itself records the new
	// colour on the item — another colourway of identical
	// facts is an angle, not another product.
	if len(item.Sizes) != len(draft.Sizes) {
		return false
	}
	for i := range item.Sizes {
		if item.Sizes[i] != draft.Sizes[i] {
			return false
		}
	}
	return true
}

// addToBurst keeps one read photo as a burst item, or folds
// it into the item it belongs to when the batch already
// holds that product: a later angle of a batched item joins
// it exactly as an angle joins an open listing.
func addToBurst(from string, s *Session, draft throwDraft, imageID, caption, colour, dataURI, visionDesc string) {
	// Media identity dedupes before any verdict is
	// consulted (the sixth audit): a redelivery of a
	// photo the batch already holds is the same item,
	// whatever the re-judgement says — the identity fold
	// below correctly refuses to fold without a positive
	// 'same', and without this guard the redelivery
	// became a second batch item for one photograph.
	for i := range s.BurstBatch {
		for _, ref := range s.BurstBatch[i].PhotoRefs {
			if ref == imageID && imageID != "" {
				return
			}
		}
	}
	for i := range s.BurstBatch {
		// A photo folds into a kept item only when the name
		// matches AND the priced facts are compatible: two
		// products can share a name ("Metal Rack") at
		// different prices, and folding those together would
		// silently keep the first price and lose the second
		// product. A conflicting price makes it a separate
		// item of the batch, even under an alike name — the
		// same compatibility rule the held-photo path uses.
		priceCompatible := draft.ListPrice <= 0 || s.BurstBatch[i].ListPrice <= 0 ||
			draft.ListPrice == s.BurstBatch[i].ListPrice
		// And the fold needs the model's eyes to judge the
		// photo the SAME product as the kept item: identity
		// is judged, not spelled, and the absence of a
		// verdict is not a match — the second audit found
		// caption-only drafts (vision read or download
		// failed, every verdict empty) folding distinct
		// products together and discarding the second's
		// description, sizes and unit whole. An unjudged
		// photo stands as its own item of the batch, where
		// she can see it and decide.
		if sameProductName(s.BurstBatch[i].Name, draft.Name) && priceCompatible &&
			burstFoldOK(dataURI, caption, s.BurstBatch[i], draft, turnTokenFor(from)) {
			refs := append([]string{}, s.BurstBatch[i].PhotoRefs...)
			probe := &OnboardingFlow{PhotoRefs: refs}
			if collectFlowPhoto(probe, imageID) {
				s.BurstBatch[i].PhotoRefs = probe.PhotoRefs
			}
			if colour != "" {
				seen := false
				for _, c := range s.BurstBatch[i].Colours {
					if c == colour {
						seen = true
						break
					}
				}
				if !seen {
					s.BurstBatch[i].Colours = append(s.BurstBatch[i].Colours, colour)
				}
			}
			if s.BurstBatch[i].Description == "" {
				s.BurstBatch[i].Description = draft.Description
			}
			if s.BurstBatch[i].VisionDesc == "" {
				s.BurstBatch[i].VisionDesc = visionDesc
			}
			return
		}
	}
	item := burstItem{
		Name:         draft.Name,
		Description:  draft.Description,
		ListPrice:    draft.ListPrice,
		PriceDoubted: draft.PriceDoubted,
		Unit:         draft.Unit,
		BaseQty:      draft.BaseQty,
		Sizes:        draft.Sizes,
		Caption:      caption,
		VisionDesc:   visionDesc,
	}
	if colour != "" {
		item.Colours = []string{colour}
	}
	if imageID != "" {
		item.PhotoRefs = []string{imageID}
	}
	s.BurstBatch = append(s.BurstBatch, item)
}

// parkPendingPhoto keeps one photo that could not be
// positively attached to the open listing — ambiguous,
// incompletely read, or priced against the listing's own
// facts — as a held item of its own. It is never collected
// onto the listing and never dropped: when the listing
// finishes, it opens as its own listing (see
// openHeldBurstItem), where its identity and price are
// settled on their own evidence. A photo whose media id is
// already held is not kept twice, and a further angle that
// positively reads as a held item's own product joins that
// item, exactly as an angle joins a listing.
func parkPendingPhoto(from string, s *Session, draft throwDraft, imageID, caption, colour, dataURI, visionDesc string) {
	if imageID == "" {
		return
	}
	for i := range s.BurstHeld {
		for _, ref := range s.BurstHeld[i].PhotoRefs {
			if ref == imageID {
				return
			}
		}
		if draft.Name != "" && s.BurstHeld[i].Name != "" &&
			sameProductName(s.BurstHeld[i].Name, draft.Name) &&
			(draft.ListPrice <= 0 || s.BurstHeld[i].ListPrice <= 0 || draft.ListPrice == s.BurstHeld[i].ListPrice) &&
			burstFoldOK(dataURI, caption, s.BurstHeld[i], draft, turnTokenFor(from)) {
			refs := append([]string{}, s.BurstHeld[i].PhotoRefs...)
			probe := &OnboardingFlow{PhotoRefs: refs}
			if collectFlowPhoto(probe, imageID) {
				s.BurstHeld[i].PhotoRefs = probe.PhotoRefs
			}
			if colour != "" {
				seen := false
				for _, c := range s.BurstHeld[i].Colours {
					if c == colour {
						seen = true
						break
					}
				}
				if !seen {
					s.BurstHeld[i].Colours = append(s.BurstHeld[i].Colours, colour)
				}
			}
			if s.BurstHeld[i].Description == "" {
				s.BurstHeld[i].Description = draft.Description
			}
			if s.BurstHeld[i].VisionDesc == "" {
				s.BurstHeld[i].VisionDesc = visionDesc
			}
			return
		}
	}
	item := burstItem{
		Name:         draft.Name,
		Description:  draft.Description,
		ListPrice:    draft.ListPrice,
		PriceDoubted: draft.PriceDoubted,
		Unit:         draft.Unit,
		BaseQty:      draft.BaseQty,
		Sizes:        draft.Sizes,
		PhotoRefs:    []string{imageID},
		Caption:      caption,
		VisionDesc:   visionDesc,
	}
	if colour != "" {
		item.Colours = []string{colour}
	}
	s.BurstHeld = append(s.BurstHeld, item)
}

// presentBurstIfReady presents the batch when nothing else
// is being listed: the open listing has just saved or been
// dropped, and the kept items are next. It is called on the
// way out of the merchant turn handlers, guarded by the
// state itself, so a batch can never interrupt a listing
// still under way. Once the batch itself is done, any held
// items (below) open in their turn.
func presentBurstIfReady(from string, s *Session) {
	if s == nil || s.Onboarding != nil {
		return
	}
	if len(s.BurstBatch) > 0 {
		presentBurstBatch(from, s)
		return
	}
	if len(s.BurstHeld) > 0 {
		openHeldBurstItem(from, s)
	}
}

// presentBurstBatch shows the kept items in one message and
// asks for their last prices together. Items she already
// sells are set aside with a line of their own; they are
// never priced or saved twice. An item whose read price the
// harvest itself doubted is likewise set aside, into the
// held queue: the batch only ever prices figures she can
// see are sound, and a doubted figure goes through the
// intake gate in its own listing instead.
func presentBurstBatch(from string, s *Session) {
	var excluded []string
	kept := s.BurstBatch[:0]
	for _, item := range s.BurstBatch {
		if catalogue.FindByName(s.MerchantID, item.Name) != nil {
			excluded = append(excluded, item.Name)
			continue
		}
		if item.PriceDoubted {
			s.BurstHeld = append(s.BurstHeld, item)
			continue
		}
		kept = append(kept, item)
	}
	s.BurstBatch = kept
	if len(s.BurstBatch) == 0 {
		s.BurstAwaiting = false
		if len(excluded) > 0 {
			// The ones already on her shelf are listed, one
			// per line, not strung into the sentence.
			lines := []string{"The rest of your photos are already on your shelf:"}
			lines = append(lines, excluded...)
			lines = append(lines, "Nothing new to add.")
			whatsapp.SendText(from, strings.Join(lines, "\n"))
		}
		if len(s.BurstHeld) > 0 {
			openHeldBurstItem(from, s)
		}
		return
	}
	s.BurstAwaiting = true
	whatsapp.SendText(from, strings.Join(burstLines(s), "\n"))
}

// openHeldBurstItem opens one held item as an ordinary
// photo listing standing at the intake gate: the price the
// harvest doubted is put to her directly, in the gate's own
// words, and the listing proceeds only on her confirmed
// figure. The item's photos and read details ride the flow
// exactly as if she had sent the photo on its own.
func openHeldBurstItem(from string, s *Session) {
	if len(s.BurstHeld) == 0 || s.Onboarding != nil {
		return
	}
	item := s.BurstHeld[0]
	// A doubted price with a settled identity opens at the
	// intake gate, in the gate's own words, and proceeds
	// only on her confirmed figure.
	if item.PriceDoubted && item.Name != "" && item.ListPrice > 0 {
		s.BurstHeld = s.BurstHeld[1:]
		flow := &OnboardingFlow{
			Step:              "confirmprice",
			Name:              item.Name,
			Description:       item.Description,
			ListPrice:         item.ListPrice,
			PriceNeedsConfirm: true,
			Unit:              item.Unit,
			BaseQty:           item.BaseQty,
			Sizes:             item.Sizes,
			Photo:             true,
			PhotoOpened:       true,
			PhotoCompact:      true,
			PhotoRefs:         item.PhotoRefs,
			ColoursSeen:       item.Colours,
		}
		if len(item.PhotoRefs) > 0 {
			flow.PhotoRef = item.PhotoRefs[0]
		}
		s.Onboarding = flow
		whatsapp.SendText(from, "Got it ✅ "+item.Name+" - "+money.GHS(item.ListPrice)+"?\n"+item.Description+"\nI read that price from your words. Is it right? Say yes, or give me the right price.")
		return
	}
	// Every other held item — ambiguous, unnamed, unpriced —
	// opens as a fresh photo listing from its own photo and
	// caption, where the ordinary walk settles what its read
	// could not. Its further kept photos and colours rejoin
	// the listing it opens.
	if len(item.PhotoRefs) == 0 {
		s.BurstHeld = s.BurstHeld[1:]
		return
	}
	shop := catalogue.GetMerchant(s.MerchantID)
	if shop == nil {
		return
	}
	s.BurstHeld = s.BurstHeld[1:]
	startPhotoOnboarding(from, shop, item.PhotoRefs[0], item.Caption)
	if s.Onboarding != nil {
		for _, ref := range item.PhotoRefs[1:] {
			collectFlowPhoto(s.Onboarding, ref)
		}
		for _, colour := range item.Colours {
			noteFlowColour(s.Onboarding, colour)
		}
	}
}

// burstLines renders the batch presentation: each item with
// the details its read carried, then the one question that
// completes them all.
func burstLines(s *Session) []string {
	n := len(s.BurstBatch)
	head := "I have kept " + strconv.Itoa(n) + " more items from your photos:"
	if n == 1 {
		head = "I have kept 1 more item from your photos:"
	}
	lines := []string{head}
	for i, item := range s.BurstBatch {
		line := strconv.Itoa(i+1) + ". *" + item.Name + "* - " + money.GHS(item.ListPrice)
		if item.Unit != "" && item.BaseQty > 0 {
			line += " for " + formatQty(item.BaseQty) + " " + unitWord(item.Unit, item.BaseQty)
		}
		if len(item.Sizes) > 0 {
			line += ", sizes " + strings.Join(item.Sizes, ", ")
		}
		if item.Description != "" {
			line += "\n" + item.Description
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", "What is the last price for each? Give them in one message, in that order, like: 170, 75, 2900. One figure for all of them works too, like: all 75. Or say fixed price, and each stays at its list price.")
	return lines
}

// burstFixedTriggers are the phrases that set every price
// as final, the batch form of a seller's fixed price.
var burstFixedTriggers = []string{"non-negotiable", "non negotiable", "fixed price", "price is final"}

// burstFixedNegations are the cues that take a fixed-price
// phrase back in the clause that carries it: "I don't want
// a fixed price; customers can bargain" contains the
// trigger words and means the opposite of them.
var burstFixedNegations = []string{"don't", "do not", "not ", "no ", "never", "isn't", "is not", "aren't", "won't", "can't", "cannot"}

// burstFixedPhrase reports whether her words set every
// price as final. Phrase containment alone is not enough:
// the message is read clause by clause (split on sentence
// punctuation and "but"), and a trigger inside a clause
// that also carries a negation is the negation that
// counts, not the trigger. A clean clause anywhere —
// "no bargaining, fixed price" — still sets them fixed.
func burstFixedPhrase(lower string) bool {
	clauses := strings.FieldsFunc(strings.ReplaceAll(lower, " but ", ";"),
		func(r rune) bool { return r == ';' || r == ',' || r == '.' || r == '\n' })
	for _, clause := range clauses {
		hit := false
		for _, trig := range burstFixedTriggers {
			if strings.Contains(clause, trig) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		negated := false
		for _, neg := range burstFixedNegations {
			if strings.Contains(clause, neg) {
				negated = true
				break
			}
		}
		if !negated {
			return true
		}
	}
	return false
}

// batchQualifier is the distinguishing detail a
// same-named batch item is saved under: the colour its
// own photo was read as, when it has one, else the
// opening words of its own description. It is composed
// only from the item's own read facts.
func batchQualifier(item burstItem) string {
	if len(item.Colours) > 0 {
		return item.Colours[0]
	}
	words := strings.Fields(item.Description)
	if len(words) > 3 {
		words = words[:3]
	}
	return strings.Trim(strings.Join(words, " "), " .,;")
}

// allFigures reads every figure in a message, in order. A
// batch answer is a list of figures, where the single-figure
// readers used elsewhere in the walk would see only one.
func allFigures(text string) []float64 {
	var out []float64
	for _, tok := range figureTokenRe.FindAllString(text, -1) {
		if v, err := parseFigure(tok); err == nil && v > 0 {
			out = append(out, v)
		}
	}
	return out
}

// handleBurstAnswer takes her reply to the batch
// presentation: figures in order, one figure for all, or
// fixed price throughout. Assigned items save at once, each
// with its own details and photos; a figure above an item's
// list price is refused for that item alone, and whatever
// remains is presented again. It reports whether the
// message was the batch's answer at all: a message with no
// figures in it is hers to spend elsewhere, and the batch
// simply keeps waiting.

// burstFigureScrub matches a figure as the figure
// tokeniser reads one, so the shape test below sees the
// same numbers the answer parser would claim. The k
// counts only at a figure boundary (the sixth audit):
// the old unconditional k scrubbed "900k5" as one figure
// where the tokeniser reads two — 900, then 5 — and the
// shape test priced the message on a reading the parser
// never made.
var burstFigureScrub = regexp.MustCompile(`[0-9][0-9,]*(?:\.[0-9]+)?[kK]\b|[0-9][0-9,]*(?:\.[0-9]+)?`)

// burstAnswerShape reports whether a message is shaped
// like a price answer: once its figures are removed, only
// list punctuation, currency words ("gh", "ghc", "cedis")
// and the scope words the batch question offers ("all",
// "each", "fixed") may remain. Any other word makes the
// message a sentence, not an answer.
func burstAnswerShape(text string) bool {
	residue := burstFigureScrub.ReplaceAllString(strings.ToLower(text), " ")
	for _, w := range strings.FieldsFunc(residue, func(r rune) bool {
		return !unicode.IsLetter(r)
	}) {
		switch w {
		case "gh", "ghc", "cedis", "all", "each", "fixed":
		default:
			return false
		}
	}
	return true
}

func handleBurstAnswer(from string, s *Session, text, lower string) bool {
	if controlWord(lower) == "cancel" {
		s.BurstBatch = nil
		s.BurstAwaiting = false
		whatsapp.SendText(from, "Dropped the kept items. Nothing was added.")
		return true
	}
	figures := allFigures(text)
	fixedAll := len(figures) == 0 && burstFixedPhrase(lower)
	if len(figures) == 0 && !fixedAll {
		return false
	}
	// A batch answer must have the shape of one: figures and
	// nothing beyond list punctuation, currency words and
	// the scope words the question itself offers ("all",
	// "each"). A sentence that merely contains a number —
	// "I have 2 racks in stock" — is not a price answer, and
	// reading its number as a last price would save a
	// product at a stray figure. Such a message is not
	// claimed here at all: it falls through to the ordinary
	// merchant read, and the batch stays waiting. A falsely
	// refused answer costs her one more message; a falsely
	// accepted one costs a wrongly priced product.
	if len(figures) > 0 && !burstAnswerShape(text) {
		return false
	}
	type assignment struct {
		floor float64
		fixed bool
	}
	assign := map[int]assignment{}
	switch {
	case fixedAll:
		for i, item := range s.BurstBatch {
			assign[i] = assignment{floor: item.ListPrice, fixed: true}
		}
	case len(figures) == 1 && len(s.BurstBatch) > 1 &&
		(strings.Contains(lower, " all") || strings.Contains(lower, " each") || strings.HasPrefix(lower, "all ")):
		for i := range s.BurstBatch {
			assign[i] = assignment{floor: figures[0]}
		}
	default:
		for i, fig := range figures {
			if i >= len(s.BurstBatch) {
				break
			}
			assign[i] = assignment{floor: fig}
		}
	}
	var saved []catalogue.Product
	var savedLines, notes []string
	var remaining []burstItem
	for i, item := range s.BurstBatch {
		a, ok := assign[i]
		if !ok {
			remaining = append(remaining, item)
			continue
		}
		if a.floor > item.ListPrice {
			notes = append(notes, money.GHS(a.floor)+" is above the list price for "+item.Name+", so I have not set it.")
			remaining = append(remaining, item)
			continue
		}
		fixed := a.fixed || a.floor == item.ListPrice
		name, desc := applyColourRuleFor(item.Colours, item.Name, item.Description)
		// Two different products can share the name she
		// captioned them with — this batch may hold both,
		// and the first of them saved a moment ago in this
		// very loop. The shelf keeps one product per name
		// (buyers ask for goods by name), so a colliding
		// item is qualified with its own distinguishing
		// detail and saves under that; she sees the name
		// it took in the Added line below. Without this the
		// second product could never save at all: priced,
		// refused, presented again, forever.
		if catalogue.FindByName(s.MerchantID, name) != nil {
			if q := batchQualifier(item); q != "" {
				name = name + " (" + q + ")"
			}
		}
		product, err := catalogue.AddProduct(s.MerchantID, catalogue.Product{
			Name:        name,
			Description: desc,
			ListPrice:   item.ListPrice,
			FloorPrice:  a.floor,
			Fixed:       fixed,
			Stock:       1,
			Unit:        item.Unit,
			BaseQty:     item.BaseQty,
			Sizes:       item.Sizes,
		})
		if err != nil {
			notes = append(notes, "I could not save "+item.Name+": "+err.Error())
			remaining = append(remaining, item)
			continue
		}
		attachPhotos(s.MerchantID, product, item.PhotoRefs)
		saved = append(saved, product)
		line := "Added ✅ " + product.Name + " - " + money.GHS(product.ListPrice)
		if product.Fixed {
			line += ", fixed price"
		} else {
			line += ", last price " + money.GHS(product.FloorPrice)
		}
		savedLines = append(savedLines, line)
	}
	s.BurstBatch = remaining
	out := append(savedLines, notes...)
	if len(remaining) > 0 {
		s.BurstAwaiting = true
		if len(out) > 0 {
			whatsapp.SendText(from, strings.Join(out, "\n"))
		}
		whatsapp.SendText(from, strings.Join(burstLines(s), "\n"))
	} else {
		s.BurstAwaiting = false
		if len(out) > 0 {
			whatsapp.SendText(from, strings.Join(out, "\n"))
		}
	}
	for _, product := range saved {
		alertWaitingBuyers(s.MerchantID, product)
		announceListing(from, s.MerchantID, product)
	}
	// The batch is finished; any held items open next, in
	// their own gated listings.
	if len(s.BurstBatch) == 0 && len(s.BurstHeld) > 0 {
		openHeldBurstItem(from, s)
	}
	return true
}
