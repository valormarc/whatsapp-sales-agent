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
}

// addToBurst keeps one read photo as a burst item, or folds
// it into the item it belongs to when the batch already
// holds that product: a later angle of a batched item joins
// it exactly as an angle joins an open listing.
func addToBurst(s *Session, draft throwDraft, imageID, caption, colour string) {
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
		if sameProductName(s.BurstBatch[i].Name, draft.Name) && priceCompatible {
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
func parkPendingPhoto(s *Session, draft throwDraft, imageID, caption, colour string) {
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
			(draft.ListPrice <= 0 || s.BurstHeld[i].ListPrice <= 0 || draft.ListPrice == s.BurstHeld[i].ListPrice) {
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
			whatsapp.SendText(from, "The rest of your photos are already on your shelf: "+strings.Join(excluded, ", ")+". Nothing new to add.")
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
		line := strconv.Itoa(i+1) + ". *" + item.Name + "* — " + money.GHS(item.ListPrice)
		if item.Unit != "" && item.BaseQty > 0 {
			line += " for " + formatQty(item.BaseQty) + " " + unitWord(item.Unit, item.BaseQty)
		}
		if len(item.Sizes) > 0 {
			line += " — sizes " + strings.Join(item.Sizes, ", ")
		}
		if item.Description != "" {
			line += "\n" + item.Description
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", "What is the last price for each? Give them in one message, in that order, like: 170, 75, 2900. One figure for all of them works too, like: all 75. Or say fixed price, and each stays at its list price.")
	return lines
}

// burstFixedPhrase reports whether her words set every price
// as final, the batch form of a seller's fixed price.
func burstFixedPhrase(lower string) bool {
	return strings.Contains(lower, "non-negotiable") ||
		strings.Contains(lower, "non negotiable") ||
		strings.Contains(lower, "fixed price") ||
		strings.Contains(lower, "price is final")
}

// allFigures reads every figure in a message, in order. A
// batch answer is a list of figures, where the single-figure
// readers used elsewhere in the walk would see only one.
func allFigures(text string) []float64 {
	var out []float64
	for _, tok := range figureTokenRe.FindAllString(text, -1) {
		if v, err := strconv.ParseFloat(strings.ReplaceAll(tok, ",", ""), 64); err == nil && v > 0 {
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

// burstFigureScrub matches a figure as allFigures reads
// one, so the shape test below sees the same numbers the
// answer parser would claim.
var burstFigureScrub = regexp.MustCompile(`[0-9][0-9,]*(\.[0-9]+)?`)

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
		line := "Added ✅ " + product.Name + " | List: " + money.GHS(product.ListPrice) + " | Last: " + money.GHS(product.FloorPrice)
		if product.Fixed {
			line += " | Fixed price"
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
