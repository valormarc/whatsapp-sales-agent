// Reading the line a seller types at the name step the way a
// person reads it: one line often carries the product, its price
// and its size all together, because that is how sellers write
// their goods up everywhere else.
package agent

import (
	"regexp"
	"strings"
)

// sizePhraseRe lifts a size out of a product line: "SIZE XXL",
// "size: 44", "Size L". The size rides into the description,
// where it belongs; it is not part of the product's name, and a
// customer should never meet it there.
var sizePhraseRe = regexp.MustCompile(`(?i)\bsize[:\s]+([a-z0-9]+)\b`)

var lineContentRe = regexp.MustCompile(`[a-zA-Z0-9]`)

// advertLines splits a pasted message into the lines that carry
// words or figures. Blank lines and decoration-only lines (the
// rows of arrows and emojis an advert ends with) carry nothing a
// listing needs and are dropped.
func advertLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && lineContentRe.MatchString(line) {
			lines = append(lines, line)
		}
	}
	return lines
}

// parsePastedAdvert reads a whole advert pasted into the name
// step in one message, the way sellers actually post: the name
// on the first line, the details under it, the price at the end.
// Not everybody has time to feed an agent one answer at a time;
// she copies the advert she already has and sends it once, and
// the agent does the separating.
//
// Two shapes count as an advert: a message of two or more real
// lines, or one long line carrying its details comma by comma
// (the write-up shape a photo caption takes). Anything smaller
// is an ordinary name line and returns ok=false, so the caller
// reads it with parseTypedNameLine as before.
//
// The reading mirrors a photo throw-in: the marked price is read
// from the whole message and taken out of her words, a measure
// rides along when she named one, the first line is the name
// (read with the settled price beside it, so a model number in
// it stays part of the name), and the remaining lines are her
// description in her own line breaks. The size note is returned
// separately: it rides into the description the caller settles.
func parsePastedAdvert(text string) (draft throwDraft, sizeNote string, ok bool) {
	trimmed := strings.TrimSpace(text)
	lines := advertLines(trimmed)
	if len(lines) < 2 {
		// One line: only a long write-up, details carried comma
		// by comma, is an advert. It is read exactly the way a
		// long photo caption is read: her words become the
		// description, her marked figure the price, and the
		// walk asks her for the name alone.
		if len(strings.Fields(trimmed)) > 8 && strings.Contains(trimmed, ",") {
			return throwDraftFromCaption(trimmed), "", true
		}
		return throwDraft{}, "", false
	}
	rest := trimmed
	if price, span, found := throwPrice(trimmed); found {
		draft.ListPrice = price
		draft.PriceDoubted = harvestDoubt(trimmed, price, span)
		rest = trimmed[:span[0]] + " " + trimmed[span[1]:]
	}
	if q, found := parseQuantity(trimmed); found {
		draft.Unit, draft.BaseQty = q.Unit, q.Qty
	}
	restLines := advertLines(rest)
	if len(restLines) == 0 {
		return draft, "", true
	}
	name, _, note := parseTypedNameLine(restLines[0], draft.ListPrice)
	draft.Name = name
	sizeNote = note
	if len(restLines) > 1 {
		draft.Description = strings.Join(restLines[1:], "\n")
	}
	return draft, sizeNote, true
}

// parseTypedNameLine reads the seller's name line and returns the
// clean product name, the price her line carried if it carried
// one, and the size note if it named a size. The price is read by
// the same reader the photo captions use (throwPrice), so both
// doors understand her the same way: an @ figure or a cedi-marked
// figure is a price, a figure riding on a count word is not. What
// is read is taken out of the name, the spacing is tidied, and
// her own words for the product itself are kept exactly as she
// typed them.
//
// settled is a price the flow already carries, read from a photo
// caption before this line was typed. When a price is already
// settled, a figure in this line is part of the product's name
// (the 840 in "HP EliteBook 840" is a model number), never a new
// price: it is kept in the name and the settled price stands.
func parseTypedNameLine(text string, settled float64) (name string, price float64, sizeNote string) {
	rest := strings.TrimSpace(text)
	if settled <= 0 {
		if p, span, ok := throwPrice(rest); ok {
			price = p
			rest = rest[:span[0]] + " " + rest[span[1]:]
		}
	}
	if m := sizePhraseRe.FindStringSubmatchIndex(rest); m != nil {
		sizeNote = "Size " + strings.ToUpper(rest[m[2]:m[3]])
		rest = rest[:m[0]] + " " + rest[m[1]:]
	}
	name = strings.Join(strings.Fields(rest), " ")
	name = strings.Trim(name, " ,.-")
	return name, price, sizeNote
}
