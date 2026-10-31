// Reading the line a seller types at the name step the way a
// person reads it: one line often carries the product, its price
// and its size all together, because that is how sellers write
// their goods up everywhere else.
package agent

import (
	"regexp"
	"strings"
	"unicode"
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
			d := throwDraftFromCaption(trimmed)
			// The one-line shape gets the advert's own
			// disciplines, which the early return used to
			// skip (the fifth audit): competing DISTINCT
			// marked figures doubt the price here too —
			// the first marked figure may be a delivery
			// fee — and the measure settles only when
			// every quantity phrase in the text agrees
			// with the one the caption read, exactly as
			// the multi-line shape holds it.
			distinct := map[float64]bool{}
			for _, v := range markedFigureValues(trimmed) {
				distinct[v] = true
			}
			if len(distinct) > 1 {
				d.PriceDoubted = true
			}
			if d.Unit != "" {
				for _, q := range quantitiesIn(trimmed) {
					if q.Unit != d.Unit || q.Qty != d.BaseQty {
						d.Unit, d.BaseQty = "", 0
						break
					}
				}
			}
			// A sole quantity that sits in a cut or
			// service clause does not settle the base
			// measure either (the sixth audit): "Cut
			// 50 yards, GH₵500" offers fifty yards cut
			// from the piece — the walk must ask for the
			// piece's own measure instead of adopting
			// the cut amount as conclusive.
			if d.Unit != "" {
				qs := quantitiesIn(trimmed)
				if len(qs) == 1 {
					if i := strings.Index(strings.ToLower(trimmed), strings.ToLower(qs[0].Text)); i >= 0 {
						start := strings.LastIndex(trimmed[:i], ",") + 1
						end := len(trimmed)
						if j := strings.Index(trimmed[i:], ","); j >= 0 {
							end = i + j
						}
						clause := strings.ToLower(trimmed[start:end])
						for _, w := range strings.FieldsFunc(clause, func(r rune) bool { return !unicode.IsLetter(r) }) {
							if w == "cut" || w == "cuts" || w == "cutting" {
								d.Unit, d.BaseQty = "", 0
								break
							}
						}
					}
				}
			}
			return d, "", true
		}
		return throwDraft{}, "", false
	}
	rest := trimmed
	var priceSpan []int
	if price, span, found := throwPrice(trimmed); found {
		draft.ListPrice = price
		draft.PriceDoubted = harvestDoubt(trimmed, price, span)
		priceSpan = span
		rest = trimmed[:span[0]] + " " + trimmed[span[1]:]
		// An advert is not a caption: it can carry several
		// marked figures (the price, a delivery fee, a
		// deposit), and the first is not necessarily the
		// product's. The third audit showed a GH₵50
		// delivery line beating the product's GH₵500
		// because marked figures were exempt from doubt
		// however many competed. Competing DISTINCT
		// marked figures doubt the read, and the walk
		// confirms the price before anything saves.
		distinct := map[float64]bool{}
		for _, v := range markedFigureValues(trimmed) {
			distinct[v] = true
		}
		if len(distinct) > 1 {
			draft.PriceDoubted = true
		}
		// And the short-caption lone-figure rule, applied
		// to a whole structured advert, binds figures no
		// price context supports: a sole trailing figure
		// on a description line — a year, a count, a phone
		// fragment — became the list price undoubted.
		// An unmarked figure harvested from beyond the
		// name line, or shaped like a year, is doubted
		// the same way.
		marked := strings.ContainsAny(strings.ToLower(trimmed[span[0]:span[1]]), "@₵") ||
			strings.Contains(strings.ToLower(trimmed[span[0]:span[1]]), "gh") ||
			strings.Contains(strings.ToLower(trimmed[span[0]:span[1]]), "cedi")
		beyondFirstLine := strings.Contains(trimmed[:span[0]], "\n")
		if !marked {
			yearShaped := price == float64(int(price)) && price >= 1900 && price <= 2099
			// A lone unmarked figure on the name line
			// itself is doubted too (the fourth audit):
			// "HP EliteBook 840" carries a model number,
			// not a price, and the short-caption rule
			// bound it undoubted because the next line
			// said "Available". On the name line a bare
			// figure is name material until the seller
			// confirms it is the price.
			loneFigure := len(figureTokenRe.FindAllString(trimmed, -1)) == 1
			if beyondFirstLine || yearShaped || (!beyondFirstLine && loneFigure) {
				draft.PriceDoubted = true
			}
		}
		// Competition is judged per line, not only among
		// marked figures (the fourth audit): when the
		// bound price was harvested from another line and
		// the item's own line carries a different figure —
		// "Leather Bag 500" over "Delivery GH₵50" — the
		// line's figure is the price-shaped rival, marked
		// or not, and the read is doubted. The same holds
		// for a figure on a line that speaks of delivery
		// or price in words, wherever the price bound.
		// lineFigures lists a line's price-shaped figures:
		// its digit figures minus the ones that belong to
		// quantity phrases ("6 yards" is a measure, not a
		// rival price) and minus the ones fused into an
		// alphanumeric token ("x360" is a model, not a
		// price).
		lineFigures := func(ln string) []float64 {
			spans := quantityNumberSpans(ln)
			var out []float64
			for _, loc := range figureTokenRe.FindAllStringIndex(ln, -1) {
				skip := false
				for _, sp := range spans {
					if loc[0] >= sp[0] && loc[1] <= sp[1] {
						skip = true
					}
				}
				if loc[0] > 0 && unicode.IsLetter(rune(ln[loc[0]-1])) {
					skip = true
				}
				if loc[1] < len(ln) && unicode.IsLetter(rune(ln[loc[1]])) {
					skip = true
				}
				if skip {
					continue
				}
				if v, err := parseFigure(ln[loc[0]:loc[1]]); err == nil {
					out = append(out, v)
				}
			}
			return out
		}
		if beyondFirstLine && len(lines) > 0 {
			// On the item's own line, a price-shaped
			// figure standing in a PRICE POSITION is a
			// rival wherever it stands: trailing the
			// line, or set off by a comma as its own
			// clause ("Leather Bag 500, was 650" — the
			// fifth audit found the run-4 last-token
			// test bound that line's figures undoubted
			// when the marked price came from a later
			// line, because only one of them stood
			// last). A figure woven into the name phrase
			// itself — flowing between the name's own
			// words, with no clause break — is name
			// material, the way "1030" sits inside "HP
			// EliteBook 1030 G8 x360"; quantity spans
			// ("6 yards") and figures fused into an
			// alphanumeric token ("x360") are excluded
			// before position is even judged.
			ln := lines[0]
			spans := quantityNumberSpans(ln)
			for _, loc := range figureTokenRe.FindAllStringIndex(ln, -1) {
				excluded := false
				for _, sp := range spans {
					if loc[0] >= sp[0] && loc[1] <= sp[1] {
						excluded = true
					}
				}
				if loc[0] > 0 && unicode.IsLetter(rune(ln[loc[0]-1])) {
					excluded = true
				}
				if loc[1] < len(ln) && unicode.IsLetter(rune(ln[loc[1]])) {
					excluded = true
				}
				if excluded {
					continue
				}
				// Price positions (the sixth audit
				// widens the fifth's): trailing the
				// line; set off by a comma; LEADING
				// the line; or set off by a clause
				// delimiter — a parenthesis, a dash,
				// a slash — on either side. A figure
				// in none of those positions, flowing
				// between the name's own words, is
				// name material.
				isDelimiter := func(r rune) bool {
					switch r {
					case '(', ')', ',', '-', '/', '–', '—':
						return true
					}
					return false
				}
				before := strings.TrimRight(ln[:loc[0]], " ")
				afterRaw := strings.TrimLeft(ln[loc[1]:], " ")
				pricePosition := false
				if strings.Trim(ln[loc[1]:], " .;:)") == "" {
					pricePosition = true // trails the line
				}
				if before == "" {
					pricePosition = true // leads the line
				}
				if strings.HasPrefix(afterRaw, ",") {
					pricePosition = true
				}
				if before != "" && isDelimiter(rune(before[len(before)-1])) {
					pricePosition = true
				}
				if afterRaw != "" && isDelimiter(rune(afterRaw[0])) {
					pricePosition = true
				}
				if !pricePosition {
					continue // woven into the name phrase
				}
				if v, err := parseFigure(ln[loc[0]:loc[1]]); err == nil && v != price {
					draft.PriceDoubted = true
				}
			}
		}
		for _, ln := range lines[1:] {
			ll := strings.ToLower(ln)
			if !strings.Contains(ll, "deliver") && !strings.Contains(ll, "price") && !strings.Contains(ll, "fee") && !strings.Contains(ll, "deposit") {
				continue
			}
			for _, v := range lineFigures(ln) {
				if v != price {
					draft.PriceDoubted = true
				}
			}
		}
	}
	// The measure binds to the price, not to the first
	// quantity phrase anywhere in the advert: a "cut 3
	// yards" note under a 6-yard piece settled the piece's
	// measure at 3 (third audit). The price's own line
	// speaks first; a conflicting quantity phrase anywhere
	// else leaves the measure unsettled and the walk asks.
	// With no quantity on the price's line, the fallback
	// scope is the item's own lines — the price's line and
	// the name line — and nowhere else: the fourth audit
	// found the whole-advert fallback settling the measure
	// from a cut note on a description line ("We can also
	// cut 3 yards"), a service the shop offers, not the
	// piece's measure. Inside that scope a single distinct
	// quantity settles it, and several settle nothing.
	{
		var priceLine string
		if priceSpan != nil {
			start := strings.LastIndex(trimmed[:priceSpan[0]], "\n") + 1
			end := strings.Index(trimmed[priceSpan[1]:], "\n")
			if end < 0 {
				priceLine = trimmed[start:]
			} else {
				priceLine = trimmed[start : priceSpan[1]+end]
			}
		}
		settle := func(q quantity) {
			draft.Unit, draft.BaseQty = q.Unit, q.Qty
		}
		if q, found := parseQuantity(priceLine); found {
			conflict := false
			for _, other := range quantitiesIn(trimmed) {
				if other.Unit != q.Unit || other.Qty != q.Qty {
					conflict = true
				}
			}
			if !conflict {
				settle(q)
			}
		} else {
			scope := priceLine
			if len(lines) > 0 {
				scope += "\n" + lines[0]
			}
			if all := quantitiesIn(scope); len(all) > 0 {
				one := all[0]
				single := true
				for _, other := range all[1:] {
					if other.Unit != one.Unit || other.Qty != one.Qty {
						single = false
					}
				}
				// The run-3 discipline survives the
				// narrower scope: a conflicting quantity
				// ANYWHERE in the advert — the cut note
				// included — leaves the measure
				// unsettled, because the two phrases
				// cannot both be the piece's measure and
				// the walk, not a guess, resolves it.
				if single {
					for _, other := range quantitiesIn(trimmed) {
						if other.Unit != one.Unit || other.Qty != one.Qty {
							single = false
						}
					}
				}
				if single {
					settle(one)
				}
			}
		}
	}
	restLines := advertLines(rest)
	if len(restLines) == 0 {
		return draft, "", true
	}
	// A slogan is not a name. Adverts open with a shout line,
	// a single decorated word over the goods ("*🔥OTUMFOU😂*"),
	// and taking it as the product's name puts the shout on
	// the shelf and in front of every buyer. When the first
	// line is a decorated single word and a later line reads
	// like a product, the shout is dropped and the product
	// line becomes the name.
	if len(restLines) > 1 && isSloganLine(restLines[0], restLines[1:]) {
		restLines = restLines[1:]
	}
	name, _, note := parseTypedNameLine(restLines[0], draft.ListPrice)
	// A name line carrying its details comma by comma gives
	// the name alone to the shelf: the first part is the name,
	// the rest belongs with the description, where a buyer can
	// read it as the detail it is.
	descLines := []string{}
	if idx := strings.Index(name, ","); idx >= 0 {
		for _, seg := range strings.Split(name[idx+1:], ",") {
			if seg = strings.TrimSpace(seg); seg != "" {
				descLines = append(descLines, seg)
			}
		}
		name = strings.TrimSpace(name[:idx])
	}
	draft.Name = name
	sizeNote = note
	for _, line := range restLines[1:] {
		line = trimDecorEdges(line)
		lowerLine := strings.ToLower(strings.TrimSpace(line))
		// A copied Stall Status includes an ordering link as its
		// final line. That is distribution copy, not product
		// description, and must not be shown to buyers as a feature.
		if strings.HasPrefix(lowerLine, "order here:") ||
			strings.HasPrefix(lowerLine, "to order,") ||
			strings.HasPrefix(lowerLine, "order here ") {
			continue
		}
		descLines = append(descLines, line)
	}
	if len(descLines) > 0 {
		draft.Description = strings.Join(descLines, "\n")
	}
	return draft, sizeNote, true
}

// cleanNameText tidies a product name for the shelf: symbol
// characters (emoji, stars, decorative marks) come out, the
// spacing is collapsed, and her own words stay exactly hers.
// A name is what a buyer reads first; it is never presented
// wearing the advert's decoration.
func cleanNameText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '-' || r == '&' || r == '\'' || r == '.' || r == '/' || r == ',' {
			b.WriteRune(r)
		} else if unicode.IsSpace(r) {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// isSloganLine reads a decorated shout line: after cleaning
// it is a single word, the raw line carries decoration the
// clean form lost, and a later line reads like a product (two
// or more words). A bare one-word name with no decoration is
// a name, and a decorated word with no product line under it
// is kept as the name, cleaned.
func isSloganLine(raw string, later []string) bool {
	cleaned := cleanNameText(raw)
	if cleaned == "" || strings.Contains(cleaned, " ") || raw == cleaned {
		return false
	}
	for _, line := range later {
		if len(strings.Fields(cleanNameText(line))) >= 2 {
			return true
		}
	}
	return false
}

// trimDecorEdges takes decoration off the ends of a
// description line (stars, emoji, rules) while leaving her
// words and their own punctuation inside the line untouched.
func trimDecorEdges(line string) string {
	return strings.TrimFunc(line, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
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
	if p, span, ok := throwPrice(rest); ok {
		// When the flow already has a list price, preserve bare
		// figures in the name ("EliteBook 840"), but still remove
		// an explicitly marked price ("Rack — GH₵270") from the
		// product name. The settled price remains authoritative.
		marked := span != nil && span[0] >= 0 && span[1] <= len(rest) &&
			(strings.ContainsAny(rest[span[0]:span[1]], "@₵") ||
				strings.Contains(strings.ToLower(rest[span[0]:span[1]]), "gh") ||
				strings.Contains(strings.ToLower(rest[span[0]:span[1]]), "cedi"))
		if settled <= 0 || marked {
			if settled <= 0 {
				price = p
			}
			rest = rest[:span[0]] + " " + rest[span[1]:]
		}
	}
	if m := sizePhraseRe.FindStringSubmatchIndex(rest); m != nil {
		sizeNote = "Size " + strings.ToUpper(rest[m[2]:m[3]])
		rest = rest[:m[0]] + " " + rest[m[1]:]
	}
	name = cleanNameText(rest)
	name = strings.Trim(name, " ,.-")
	return name, price, sizeNote
}
