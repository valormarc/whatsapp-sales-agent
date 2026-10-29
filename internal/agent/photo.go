package agent

// Throw-in onboarding: a seller stocks her shelf the way she
// already posts in the selling groups, by throwing a product in.
// A photograph of her goods, with whatever words she has that
// day: sometimes nothing at all, sometimes one word, sometimes a
// bare figure, sometimes a short line, sometimes a full write-up.
// The agent meets that as it lands instead of teaching her a new
// way to post.
//
// What is read from the throw-in, in order of trust:
//
//   - Her price, read by code from her own figures only. A figure
//     she marks as a price (an @ figure, or a figure carrying a
//     cedi word) is her price wherever it sits; a bare figure on
//     its own is her price; in a short caption one lone figure is
//     her price. Numbers that belong to something else are never
//     read as her price: a size is not a price, a quantity in a
//     measure phrase is not a price, and in a long detailed
//     caption an unmarked number (memory, storage, a screen size)
//     is not a price either. When no figure reads as a price, no
//     price is set and she is asked for one.
//   - A measure, when her words carry one ("6 yards"), with the
//     same reading buyers' quantities get.
//   - Her own words, kept as her description when they describe
//     the goods; a short clean phrase with no numbers in it is
//     read as the product's name. Filler words on their own
//     ("still available", "dm if interested") carry nothing and
//     are dropped.
//   - A model draft from the photograph itself, which fills only
//     what her words left empty, never replaces them, and never
//     carries a price at all. The picture is fetched only in live
//     mode; without it, her words alone drive the draft.
//
// The draft then enters the ordinary onboarding walk with its
// answered fields already filled, so the walk asks only for what
// is missing. One thing is missing almost every time: her last
// price, which no group post ever carries and nothing here ever
// guesses. And as in the guided walk, nothing is saved until she
// confirms the summary with a clear yes.

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// throwDraft is what one thrown-in photo and its caption could be
// read for. Zero values mean "not read", and the walk will ask.
type throwDraft struct {
	Name        string
	Description string
	ListPrice   float64
	Unit        string
	BaseQty     float64
	Sizes       []string
	// PriceDoubted marks a read price the harvest itself does
	// not trust (see harvestDoubt): the walk confirms it with
	// the seller before the listing can go any further.
	PriceDoubted bool
}

// harvestDoubt judges a price read out of the seller's own text
// before the walk trusts it. Two reads are doubted. A price so
// small it is far more likely a fragment of a bigger figure than
// the price of the goods she just described: the laptop read at
// GH₵1 off a "Ghc 1,500" line. And an unmarked figure harvested
// from among other figures, where a model number or a
// specification stands in the same place a price would: the
// 5550 read off a laptop's name line. A figure she marked
// herself (@250, 250 ghs) is her own statement and is trusted,
// and so is an unmarked figure with nothing competing beside it.
func harvestDoubt(caption string, price float64, span []int) bool {
	if price <= 5 {
		return true
	}
	if span == nil || span[0] < 0 || span[1] > len(caption) {
		return false
	}
	marked := strings.ToLower(caption[span[0]:span[1]])
	if strings.ContainsAny(marked, "@₵") || strings.Contains(marked, "gh") || strings.Contains(marked, "cedi") {
		return false
	}
	rest := caption[:span[0]] + " " + caption[span[1]:]
	return len(numbersIn(rest)) > 0
}

// figurePattern is one money figure as sellers write it,
// thousands comma and all: "4500", "1,500", "12,500". The comma
// form comes first in the alternation, so a reader never stops
// at the comma: "Ghc 1,500" is fifteen hundred cedis, and a
// reader that takes only the "1" has priced a laptop at GH₵1.
const figurePattern = `\d{1,3}(?:,\d{3})+(?:\.\d{1,2})?|\d+(?:\.\d{1,2})?`

// parseFigure parses a figure matched by figurePattern: the
// thousands commas are grouping, not decimals, so they come out
// before the parse.
func parseFigure(s string) (float64, error) {
	return strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
}

var (
	// An @ figure is the groups' own price mark: "@250ghc". The
	// cedi word that rides on the figure is part of the price
	// expression, so the span covers it and it leaves her words
	// with the figure.
	atPriceRe = regexp.MustCompile(`@\s*(` + figurePattern + `)(?:\s*(?:ghs|ghc|ghana\s+cedis|cedis|gh))?`)
	// A figure with a cedi word beside it, on either side:
	// "ghs 400", "450gh", "GH₵450", "450 cedis".
	cediBeforeRe = regexp.MustCompile(`(?i)(?:gh₵|ghs|ghc|ghana\s+cedis|cedis|₵)\s*(` + figurePattern + `)`)
	cediAfterRe  = regexp.MustCompile(`(?i)(` + figurePattern + `)\s*(?:ghs|ghc|gh|ghana\s+cedis|cedis)\b`)
	// A size names its number: "size 44" is a size, never a price.
	sizeNumberRe = regexp.MustCompile(`(?i)\bsize\s*(\d+(?:\.\d+)?)`)
	// A caption that is nothing but a figure is a price on its own.
	bareFigureRe = regexp.MustCompile(`^\s*@?\s*(?:(?:gh₵|ghs|ghc|cedis|₵)\s*)?(` + figurePattern + `)\s*(?:gh|ghs|ghc|cedis)?\s*\.?\s*$`)
	// throwFigureRe finds figures in a short caption for the
	// lone-figure rule, commas and all.
	throwFigureRe = regexp.MustCompile(`(` + figurePattern + `)`)
)

// throwPrice reads the seller's price out of her caption and
// returns it with the span of the whole price expression, so the
// caller can take the expression out of her words and keep the
// rest. The rules are the file's header in code: marked figures
// first, then a bare caption, then one lone figure in a short
// caption. Anything else reads no price at all.
func throwPrice(caption string) (float64, []int, bool) {
	if m := atPriceRe.FindStringSubmatchIndex(caption); m != nil {
		if f, err := parseFigure(caption[m[2]:m[3]]); err == nil && f > 0 {
			return f, []int{m[0], m[1]}, true
		}
	}
	type hit struct {
		value float64
		span  []int
		at    int
	}
	var hits []hit
	for _, re := range []*regexp.Regexp{cediBeforeRe, cediAfterRe} {
		if m := re.FindStringSubmatchIndex(caption); m != nil {
			if f, err := parseFigure(caption[m[2]:m[3]]); err == nil && f > 0 {
				hits = append(hits, hit{f, []int{m[0], m[1]}, m[0]})
			}
		}
	}
	if len(hits) > 0 {
		first := hits[0]
		for _, h := range hits[1:] {
			if h.at < first.at {
				first = h
			}
		}
		return first.value, first.span, true
	}
	if m := bareFigureRe.FindStringSubmatchIndex(caption); m != nil {
		if f, err := parseFigure(caption[m[2]:m[3]]); err == nil && f > 0 {
			return f, []int{m[0], m[1]}, true
		}
	}
	// One lone figure in a short caption. Figures that belong to
	// a quantity phrase or a size are not candidates, and two or
	// more candidates mean the caption's figures cannot be told
	// apart, so none of them is read as the price.
	if len(strings.Fields(caption)) <= 7 {
		spans := quantityNumberSpans(caption)
		for _, m := range sizeNumberRe.FindAllStringSubmatchIndex(caption, -1) {
			spans = append(spans, [2]int{m[2], m[3]})
		}
		var found *hit
		count := 0
		for _, m := range throwFigureRe.FindAllStringSubmatchIndex(caption, -1) {
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
			// A figure with a plain word hard against it is
			// counting or describing that word, not naming a
			// price: "3 doors", "3-door wardrobe", "2 seater".
			// A price figure stands at the end of her words or
			// is followed only by price talk ("250 negotiable",
			// "250 for it", "250 available").
			if w := wordAfterFigure(caption[m[1]:]); w != "" && !priceTalkWords[w] {
				continue
			}
			f, err := parseFigure(caption[m[2]:m[3]])
			if err != nil || f <= 0 {
				continue
			}
			count++
			h := hit{f, []int{m[0], m[1]}, m[0]}
			found = &h
		}
		if count == 1 && found != nil {
			return found.value, found.span, true
		}
	}
	return 0, nil, false
}

// priceTalkWords are the words that can stand right after a price
// figure without turning it into a count: currency words and the
// groups' price talk. Any other word hard against a figure makes
// the figure a count of that word.
var priceTalkWords = map[string]bool{
	"gh": true, "ghs": true, "ghc": true, "cedis": true,
	"available": true, "negotiable": true, "only": true,
	"each": true, "per": true, "for": true, "flat": true,
	"fixed": true, "last": true, "now": true, "today": true,
}

var wordAfterRe = regexp.MustCompile(`^[\s\-]*([a-z]+)`)

// wordAfterFigure returns the first plain word following a figure
// in the caption, lower case, or "" when the figure stands at the
// end of her words or against punctuation that closes them.
func wordAfterFigure(rest string) string {
	m := wordAfterRe.FindStringSubmatch(strings.ToLower(rest))
	if m == nil {
		return ""
	}
	return m[1]
}

// throwFillerWords are caption words that carry no product detail
// on their own: the groups' availability and contact filler. A
// caption made only of these reads as no words at all.
var throwFillerWords = map[string]bool{
	"available": true, "still": true, "back": true, "in": true,
	"stock": true, "dm": true, "if": true, "interested": true,
	"pls": true, "please": true, "now": true, "for": true,
	"sale": true, "selling": true,
}

var throwWordSplit = regexp.MustCompile(`[^a-z0-9]+`)

// throwDraftFromCaption reads one caption the deterministic way:
// price, measure, and her own words sorted into a name or a
// description. No model takes part, so this reading is the same
// in tests, in dry runs and live.
func throwDraftFromCaption(caption string) throwDraft {
	d := throwDraft{}
	caption = strings.TrimSpace(caption)
	if caption == "" {
		return d
	}
	rest := caption
	if price, span, ok := throwPrice(caption); ok {
		d.ListPrice = price
		d.PriceDoubted = harvestDoubt(caption, price, span)
		rest = strings.TrimSpace(caption[:span[0]] + " " + caption[span[1]:])
	}
	if q, ok := parseQuantity(caption); ok {
		d.Unit, d.BaseQty = q.Unit, q.Qty
	}
	// What is left of her words, tidied: doubled spaces and the
	// separators a price used to hang on are tidied away, and a
	// caption of pure filler leaves nothing behind.
	rest = strings.Join(strings.Fields(rest), " ")
	rest = strings.Trim(rest, " /-|.")
	if rest == "" {
		return d
	}
	words := throwWordSplit.Split(strings.ToLower(rest), -1)
	meaningful := 0
	for _, w := range words {
		if w != "" && !throwFillerWords[w] {
			meaningful++
		}
	}
	if meaningful == 0 {
		return d
	}
	// A short clean phrase with no number in it is a name ("Heels",
	// "Leather bag"). Anything longer, or anything carrying its
	// own numbers (a size, a specification), is her description.
	if len(strings.Fields(rest)) <= 3 && len(numbersIn(rest)) == 0 {
		d.Name = rest
		return d
	}
	d.Description = rest
	return d
}

// throwReadBack is the one piece that tells the seller what the
// agent made of her throw-in, from the flow's filled fields: the
// name and description when there are any, the price she gave,
// the measure she named. Empty when nothing could be read.
func throwReadBack(flow *OnboardingFlow) string {
	var lines []string
	switch {
	case flow.Name != "" && flow.Description != "":
		lines = append(lines, flow.Name+": "+flow.Description)
	case flow.Name != "":
		lines = append(lines, "Name: "+flow.Name)
	case flow.Description != "":
		lines = append(lines, "Description: "+flow.Description)
	}
	if flow.ListPrice > 0 {
		lines = append(lines, "List price "+money.GHS(flow.ListPrice)+", from your own words.")
	}
	if flow.Unit != "" && flow.BaseQty > 0 {
		lines = append(lines, "Sold by the "+flow.Unit+": that price is for "+formatQty(flow.BaseQty)+" "+unitWord(flow.Unit, flow.BaseQty)+".")
	}
	if len(lines) == 0 {
		return ""
	}
	return "Here is what I have from it:\n" + strings.Join(lines, "\n")
}

// startPhotoOnboarding opens onboarding from a seller's thrown-in
// photo. It runs on her own session in her own shop, so the walk
// that follows is the ordinary one, with her answers owned by the
// flow exactly as in merchant mode. Fields the throw-in already
// answered are merged over anything an earlier in-progress flow
// had collected, so sending a photo mid-walk loses nothing.
func startPhotoOnboarding(from string, shop *catalogue.Merchant, imageID, caption string) {
	// A buyer is waiting on a picture from this shop: the
	// owner's next photo is that picture, not a new listing.
	if servePendingPicRequest(from, shop, imageID) {
		return
	}
	key := sessionKey(shop.ID, from)
	s, ok := sessions[key]
	if !ok {
		s = &Session{Stage: "merchant", MerchantID: shop.ID}
		sessions[key] = s
	}
	flow := s.Onboarding
	if flow == nil {
		flow = &OnboardingFlow{PhotoOpened: true}
	}
	// A guided walk she started by typing ("add a product")
	// and has not yet given a single fact to keeps its own
	// shape when a photo joins it; every other door (a fresh
	// photo, a photo joining her advert) takes the compact
	// photo shape below.
	virginGuided := s.Onboarding != nil && !flow.Photo &&
		flow.Name == "" && flow.Description == "" && flow.ListPrice <= 0

	draft := throwDraftFromCaption(caption)
	// A forwarded Status caption already contains the product
	// name and list price: it is the seller's own advert in
	// text, riding on the photo. Read it as an advert before
	// relying on the pixels: the image is the product photo,
	// while the caption is already her words.
	if advert, _, isAdvert := parsePastedAdvert(caption); isAdvert {
		if advert.Name != "" {
			draft.Name = advert.Name
		}
		if advert.Description != "" {
			draft.Description = advert.Description
		}
		if advert.ListPrice > 0 {
			draft.ListPrice = advert.ListPrice
			draft.PriceDoubted = advert.PriceDoubted
		}
		if advert.Unit != "" {
			draft.Unit, draft.BaseQty = advert.Unit, advert.BaseQty
		}
		if len(advert.Sizes) > 0 {
			draft.Sizes = advert.Sizes
		}
	}
	// The model's draft from the photograph fills only the
	// gaps her own words left, including its judgement of how
	// the goods are sold when that judgement is certain. It
	// needs the picture, so it happens only in live mode; her
	// words never wait on it.
	var visionDesc string
	if dataURI, ok := whatsapp.DownloadMedia(imageID); ok {
		if md := model.DraftListing(dataURI, caption); md != nil {
			visionDesc = md.Description
			if draft.Name == "" {
				draft.Name = md.Name
			}
			if draft.Description == "" {
				draft.Description = md.Description
			}
			if draft.Unit == "" && md.UnitCertain {
				draft.Unit = md.Unit
			}
			if len(draft.Sizes) == 0 {
				draft.Sizes = md.Sizes
			}
		}
	}
	// A forwarded advert riding as a caption is raw material,
	// not a description: copied whole onto the shelf it reads
	// as the emoji-littered spec dump it is, and no buyer
	// reads it. The model's summarised line replaces it; with
	// no model to summarise, the dump is not carried at all
	// and the walk asks her for a description in her own
	// words instead.
	if isAdvertDump(caption) {
		if visionDesc != "" {
			draft.Description = visionDesc
		} else {
			draft.Description = ""
		}
	}
	// A photo of a DIFFERENT product while a photo listing is
	// already open (the second item in an album she forwarded)
	// does not merge into the listing under way and does not
	// disturb it: listings are taken one at a time, and she is
	// told what the open one is waiting for.
	if s.Onboarding != nil && flow.Photo && flow.Name != "" && draft.Name != "" &&
		!sameProductName(flow.Name, draft.Name) {
		if photoAckCovered(flow.HeldAckAt) {
			return
		}
		flow.HeldAckAt = time.Now()
		whatsapp.SendText(from, "One at a time: I am still listing "+flow.Name+". "+photoStepReminder(flow))
		return
	}
	// The gate for a photo-opened listing that already has an
	// identity: a new photo joins it only when this photo's
	// own read is consistent with the listing. A read carrying
	// a different price contradicts it, and a read carrying
	// nothing at all — no name, no price, the picture itself
	// unread — does not identify the photo as the same goods.
	// Different products were merged exactly that way on the
	// live number: a forwarded burst of separately priced
	// items landed on the first listing as its "angles", and
	// it was saved wearing all of them. A held photo earns
	// one clear message per burst and is not collected; the
	// seller sends it again, with its name, once this listing
	// is finished. A listing opened by text is not gated: its
	// photos are thrown in deliberately against a listing she
	// named herself.
	if s.Onboarding != nil && flow.PhotoOpened && flow.Name != "" && imageID != "" {
		already := false
		for _, ref := range flow.PhotoRefs {
			if ref == imageID {
				already = true
				break
			}
		}
		contradicts := draft.ListPrice > 0 && flow.ListPrice > 0 && draft.ListPrice != flow.ListPrice
		unidentified := draft.Name == "" && draft.ListPrice <= 0
		if !already && len(flow.PhotoRefs) > 0 && (contradicts || unidentified) {
			if photoAckCovered(flow.HeldAckAt) {
				return
			}
			flow.HeldAckAt = time.Now()
			if contradicts {
				whatsapp.SendText(from, "One at a time: I am still listing "+flow.Name+". "+photoStepReminder(flow))
			} else {
				whatsapp.SendText(from, "One at a time: I am still listing "+flow.Name+". I could not identify this new photo, so I have not attached it. "+photoStepReminder(flow)+" If it is a different item, send it again with its name once this listing is finished.")
			}
			return
		}
	}
	// The same photo sent again while its listing is still
	// open (a double tap, a redelivery, a seller checking
	// whether it landed) changes nothing: one short line says
	// the agent is still on it, and nothing is asked again.
	// The same judgement governs every later photo in the
	// burst: one that is positively read as a DIFFERENT
	// product is held (below); anything else — the same
	// product under a variant of its name, or a photo the
	// platform cannot name at all — is another angle of the
	// goods being listed, collected silently under the album
	// rule. An album is never interrogated photo by photo,
	// and an unidentified angle is never, by itself, proof
	// of a different product.
	// A nameless open listing is not covered by this branch
	// when the new photo carries a name: that name is the
	// listing's own, still unsettled, and the fill below is
	// what adopts it.
	sameName := draft.Name == "" || (flow.Name != "" && sameProductName(draft.Name, flow.Name))
	if s.Onboarding != nil && flow.Photo && sameName &&
		(draft.Description == "" || draft.Description == flow.Description) &&
		(draft.ListPrice <= 0 || draft.ListPrice == flow.ListPrice) &&
		(draft.Unit == "" || draft.Unit == flow.Unit) {
		// Another angle of the same goods: it joins the
		// listing's kept photos even though it earns no reply.
		// A media id already collected is not collected
		// twice (collectFlowPhoto keeps each id once).
		collectFlowPhoto(flow, imageID)
		if photoAckCovered(flow.RepeatAckAt) {
			return
		}
		flow.RepeatAckAt = time.Now()
		whatsapp.SendText(from, "I am still on that same photo.")
		return
	}
	flow.Photo = true
	flow.PhotoRef = imageID
	collectFlowPhoto(flow, imageID)
	if flow.Name == "" {
		flow.Name = draft.Name
	}
	if flow.Description == "" {
		flow.Description = draft.Description
	}
	if flow.ListPrice <= 0 {
		flow.ListPrice = draft.ListPrice
		if draft.ListPrice > 0 {
			flow.PriceNeedsConfirm = draft.PriceDoubted
			flow.PriceConfirmed = false
		}
	}
	if flow.Unit == "" && draft.Unit != "" {
		flow.Unit, flow.BaseQty = draft.Unit, draft.BaseQty
	}
	if len(flow.Sizes) == 0 {
		flow.Sizes = draft.Sizes
	}
	// The same replacement where the dump arrived earlier,
	// as a pasted advert now joined by its photo: the model's
	// clean line takes the dump's place on the listing.
	if visionDesc != "" && isAdvertDump(flow.Description) {
		flow.Description = visionDesc
	}
	// A drafted name she already sells cannot be saved under,
	// so it is dropped here and the walk asks her for a name,
	// where the duplicate is explained if she offers it again.
	if flow.Name != "" && catalogue.FindByName(shop.ID, flow.Name) != nil {
		flow.Name = ""
	}
	s.Onboarding = flow

	// The compact photo shape: one message carrying what was
	// read and the single question that remains, and her
	// answer to the last price question saves the product at
	// once (see savePhotoCompactListing). It applies whenever
	// the photo and her words together gave a name and a
	// description; anything less falls back to the walk.
	if !virginGuided && flow.Name != "" && flow.Description != "" {
		flow.PhotoCompact = true
		flow.RepeatAckAt = time.Now()
		// A photo joining a listing whose last price question
		// is already outstanding (her advert opened it a
		// moment ago) is acknowledged, not answered with the
		// question all over again: the question stands, and
		// her answer to it now saves the product at once.
		if s.Onboarding != nil && flow.Step == "lastprice" && flow.FloorPrice <= 0 {
			flow.RepeatAckAt = time.Now()
			whatsapp.SendText(from, "Got it ✅ Photo added to "+flow.Name+".")
			return
		}
		switch {
		case flow.PriceNeedsConfirm && !flow.PriceConfirmed:
			flow.Step = "confirmprice"
			whatsapp.SendText(from, "Got it ✅ "+flow.Name+" - "+money.GHS(flow.ListPrice)+"?\n"+flow.Description+"\nI read that price from your words. Is it right? Say yes, or give me the right price.")
		case flow.ListPrice <= 0:
			flow.Step = "list"
			whatsapp.SendText(from, "Got it ✅ "+flow.Name+"\n"+flow.Description+"\nWhat is the list price, in cedis?")
		default:
			flow.Step = "lastprice"
			whatsapp.SendText(from, "Got it ✅ "+flow.Name+" - "+money.GHS(flow.ListPrice)+"\n"+flow.Description+"\nWhat is your last price? If non-negotiable, say 'non-negotiable'. If list price is wrong, correct like 'price is 500'.")
		}
		return
	}

	flow.RepeatAckAt = time.Now()
	whatsapp.SendText(from, "Got your photo.")
	if read := throwReadBack(flow); read != "" {
		whatsapp.SendText(from, read)
	}
	gotoNextOnboardingStep(from, flow)
}

// photoNameKey normalises a drafted product name for the
// loose comparisons the photo doors make: case and spacing
// aside, and decoration stripped the way listing names are.
func photoNameKey(name string) string {
	return strings.Join(strings.Fields(cleanNameText(strings.ToLower(name))), " ")
}

// sameProductName judges whether two drafted names plausibly
// name the same goods: exactly alike, one inside the other,
// or sharing a real word. It keeps a second angle of the same
// product (read slightly differently) merging into its
// listing, while a genuinely different product in the same
// album is held for its own turn.
func sameProductName(a, b string) bool {
	ka, kb := photoNameKey(a), photoNameKey(b)
	if ka == "" || kb == "" {
		return false
	}
	if ka == kb || strings.Contains(ka, kb) || strings.Contains(kb, ka) {
		return true
	}
	seen := map[string]bool{}
	for _, w := range strings.Fields(ka) {
		if len(w) > 3 {
			seen[w] = true
		}
	}
	for _, w := range strings.Fields(kb) {
		if seen[w] {
			return true
		}
	}
	return false
}

// photoStepReminder names, shortly, the one thing a repeated
// photo's open walk is waiting for, so the reminder can stand
// in for the full question that was already asked.
func photoStepReminder(flow *OnboardingFlow) string {
	switch flow.Step {
	case "name":
		return "What is it called?"
	case "description":
		return "Tell me about it in a few words, whenever you are ready."
	case "list":
		return "What is the list price for " + flow.Name + "?"
	case "confirmprice":
		return "I just need you to confirm the price I read, or give me the right one."
	case "lastprice":
		return "What is your last price for " + flow.Name + "?"
	case "measure":
		return "Do you sell it by measure, or only as a whole?"
	default:
		return "Say yes to save it whenever you are ready, or tell me what to change."
	}
}

// isAdvertDump reports whether a text has the shape of a
// forwarded advert rather than a person's own short words:
// many lines, or a long unbroken run of them. It judges the
// shape only, never the content.
func isAdvertDump(text string) bool {
	return strings.Count(text, "\n") >= 3 || len(strings.Fields(text)) > 25
}

// photoAckWindow is the span one photo acknowledgement
// covers: an album's items land seconds apart, and a seller
// re-sending a photo to check it landed does so at once too.
// Past the window, a fresh photo earns a fresh line.
const photoAckWindow = 90 * time.Second

// photoAckCovered reports whether the given acknowledgement
// stamp falls inside the burst window, in which case the
// item is taken in silently.
func photoAckCovered(stamp time.Time) bool {
	return !stamp.IsZero() && time.Since(stamp) < photoAckWindow
}
