// Shops themselves: finding one by its code, its owner's
// number or its name, and creating a new one with a unique
// shop code when a seller opens her shop in chat.
package catalogue

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var nonDigits = regexp.MustCompile(`\D`)

func digitsOnly(v string) string {
	return nonDigits.ReplaceAllString(v, "")
}

// MerchantByShopCode finds a shop by its unique entry code, ignoring
// case. It returns nil when no shop carries that code: a buyer who
// names a code we do not know is never silently routed somewhere.
func MerchantByShopCode(code string) *Merchant {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil
	}
	c := LoadCatalogue()
	for _, id := range sortedIDs(c) {
		m := c.Merchants[id]
		if m.ShopCode != "" && strings.EqualFold(m.ShopCode, code) {
			return &m
		}
	}
	return nil
}

// MerchantByOwner finds the shop a phone number owns, comparing on
// digits so +233 and spacing differences never misroute her. A
// number owns at most one shop in this build.
func MerchantByOwner(phone string) *Merchant {
	d := digitsOnly(phone)
	if d == "" {
		return nil
	}
	c := LoadCatalogue()
	for _, id := range sortedIDs(c) {
		m := c.Merchants[id]
		if m.OwnerWhatsApp != "" && digitsOnly(m.OwnerWhatsApp) == d {
			return &m
		}
	}
	return nil
}

// MerchantByShopName resolves a shop from the words a buyer typed:
// the full shop name appearing in the text, or a short reply that is
// itself part of a shop name ("Efua" for "Efua's Store"). It is the
// fallback for a buyer who arrived with no shop code; the agent
// never defaults her to a shop without one of the two.
func MerchantByShopName(text string) *Merchant {
	q := strings.ToLower(strings.TrimSpace(text))
	if len(q) < 3 {
		return nil
	}
	c := LoadCatalogue()
	for _, id := range sortedIDs(c) {
		m := c.Merchants[id]
		name := strings.ToLower(m.Name)
		if strings.Contains(q, name) || strings.Contains(name, q) {
			return &m
		}
	}
	return nil
}

// ErrEmptyShopName is returned when a shop is created with no name.
var ErrEmptyShopName = errors.New("a shop needs a name")

// shopCodeFor derives a shop code from a shop name: the first word,
// letters only, upper case, made unique against the shops already in
// the catalogue (EFUA, then EFUA-2, and so on).
func shopCodeFor(c Catalogue, name string) string {
	base := ""
	for _, w := range wordSplit.Split(strings.ToLower(name), -1) {
		if w != "" {
			base = w
			break
		}
	}
	letters := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r
		}
		return -1
	}, base)
	code := strings.ToUpper(letters)
	if len(code) > 10 {
		code = code[:10]
	}
	if code == "" {
		code = "SHOP"
	}
	taken := func(candidate string) bool {
		for _, m := range c.Merchants {
			if strings.EqualFold(m.ShopCode, candidate) {
				return true
			}
		}
		return false
	}
	if !taken(code) {
		return code
	}
	// A clash is settled with a hyphen and a counting number:
	// BELORA, then BELORA-2, then BELORA-3. The hyphen keeps the
	// number off her name, where a melted suffix (BELORA2) reads
	// as if the shop itself were named "Belora 2". The number
	// counts from 2 with no fixed width: it stays one digit while
	// one digit suffices and grows only when a name is common
	// enough to need it. Her shop's name itself never carries it;
	// the code is the address her customers enter by, nothing more.
	for i := 2; ; i++ {
		candidate := code + "-" + strconv.Itoa(i)
		if !taken(candidate) {
			return candidate
		}
	}
}

// CreateMerchant opens a new shop in the catalogue for a merchant who
// signed up in chat: a unique id slugged from the name, a unique shop
// code, and the owner's number recorded so the agent recognises her
// from then on. The shelf starts empty; the add-a-product flow fills
// it.
func CreateMerchant(name, ownerWhatsApp string) (Merchant, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Merchant{}, ErrEmptyShopName
	}
	c := LoadCatalogue()
	id := slugify(name)
	if _, taken := c.Merchants[id]; taken {
		base := id
		for i := 2; ; i++ {
			id = base + "-" + strconv.Itoa(i)
			if _, taken := c.Merchants[id]; !taken {
				break
			}
		}
	}
	m := Merchant{
		ID:            id,
		Name:          name,
		ShopCode:      shopCodeFor(c, name),
		OwnerWhatsApp: ownerWhatsApp,
		// Her money goes to the number she chats from unless she
		// says otherwise: that is the number her payment account is
		// opened on and the number buyers are told to pay directly.
		PayoutPhone: ownerWhatsApp,
		Products:    []Product{},
	}
	if c.Merchants == nil {
		c.Merchants = map[string]Merchant{}
	}
	c.Merchants[id] = m
	SaveCatalogue(c)
	return m, nil
}
