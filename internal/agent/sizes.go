package agent

// Sizes on an order. A product whose listing carries sizes
// is never sold without the buyer choosing one: the choice
// is asked once, inside the package details, verified
// against the listing's own list, and carried onto the
// order, the recap and the seller's notice, so the parcel
// that ships is the size she asked for. A size she stated
// herself, labelled as a size in her own words at any point
// in the chat, is remembered and confirmed in the recap
// instead of asked for again; an unlabelled number is never
// read as a size, because in a haggle a number is money
// until she says otherwise.

import (
	"regexp"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// anySizedLine reports whether any line of the pending
// order, parked or in hand, comes in sizes.
func anySizedLine(s *Session) bool {
	if s.Product != nil && len(s.Product.Sizes) > 0 {
		return true
	}
	for _, it := range s.Basket {
		if len(it.Product.Sizes) > 0 {
			return true
		}
	}
	return false
}

// sizesLine renders a product's size list the way a person
// lists them: "37, 38, 39, 40, 41 and 42".
func sizesLine(p *catalogue.Product) string {
	if p == nil || len(p.Sizes) == 0 {
		return ""
	}
	if len(p.Sizes) == 1 {
		return p.Sizes[0]
	}
	return strings.Join(p.Sizes[:len(p.Sizes)-1], ", ") + " and " + p.Sizes[len(p.Sizes)-1]
}

// matchListedSize reads the size a buyer means in her reply
// against the product's own list: a listed size appearing as
// a whole word or phrase in what she said. Anything else is
// not a size, however number-shaped it looks.
func matchListedSize(p *catalogue.Product, msg string) string {
	if p == nil || len(p.Sizes) == 0 {
		return ""
	}
	lower := " " + strings.ToLower(strings.TrimSpace(msg)) + " "
	for _, sz := range p.Sizes {
		needle := " " + strings.ToLower(strings.TrimSpace(sz)) + " "
		if strings.Contains(lower, needle) {
			return sz
		}
	}
	return ""
}

var sizeLabelRe = regexp.MustCompile(`(?i)\bsize\s*(?:is\s+)?[:\s]*([a-z0-9]+(?:\.[0-9]+)?)\b`)

// statedSize reads a size the buyer labelled as one in her
// own words ("I take size 40", "size 38 please") and checks
// it against the product's list. Only her label makes a
// number a size; the check keeps a price offer from ever
// being filed as one.
func statedSize(p *catalogue.Product, words string) string {
	if p == nil || len(p.Sizes) == 0 {
		return ""
	}
	for _, m := range sizeLabelRe.FindAllStringSubmatch(words, -1) {
		for _, sz := range p.Sizes {
			if strings.EqualFold(strings.TrimSpace(sz), strings.TrimSpace(m[1])) {
				return sz
			}
		}
	}
	return ""
}
