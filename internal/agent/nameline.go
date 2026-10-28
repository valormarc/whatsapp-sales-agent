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

// parseTypedNameLine reads the seller's name line and returns the
// clean product name, the price her line carried if it carried
// one, and the size note if it named a size. The price is read by
// the same reader the photo captions use (throwPrice), so both
// doors understand her the same way: an @ figure or a cedi-marked
// figure is a price, a figure riding on a count word is not. What
// is read is taken out of the name, the spacing is tidied, and
// her own words for the product itself are kept exactly as she
// typed them.
func parseTypedNameLine(text string) (name string, price float64, sizeNote string) {
	rest := strings.TrimSpace(text)
	if p, span, ok := throwPrice(rest); ok {
		price = p
		rest = rest[:span[0]] + " " + rest[span[1]:]
	}
	if m := sizePhraseRe.FindStringSubmatchIndex(rest); m != nil {
		sizeNote = "Size " + strings.ToUpper(rest[m[2]:m[3]])
		rest = rest[:m[0]] + " " + rest[m[1]:]
	}
	name = strings.Join(strings.Fields(rest), " ")
	name = strings.Trim(name, " ,.-")
	return name, price, sizeNote
}
