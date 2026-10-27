// Package catalogue is the per-merchant catalogue store. A JSON file
// for the hackathon build; the shape mirrors what merchant photo
// onboarding will write once SAM 3.1 lands.
package catalogue

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/money"
)

// Product is one item a merchant sells. Prices are in cedis; FloorPrice
// is the merchant's secret floor, which never reaches the customer.
//
// A product may be sold by measure:
// in the real market, goods move by the yard, the litre, the kilo
// and the olonka as often as by the whole piece. Unit names the
// measure (canonical singular lower case: "yard", "litre", "kilo",
// "metre", "gram", "olonka", "tin") and BaseQty is the quantity
// the ListPrice and FloorPrice are for: a 6-yard piece at GH₵450 is
// GH₵75 a yard, and the seller can cut and sell 3 yards for GH₵225,
// or refuse to split and sell the piece whole. Unit empty means
// sold whole only, as every product was before this field existed.
type Product struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	ListPrice   float64 `json:"listPrice"`
	FloorPrice  float64 `json:"floorPrice"`
	Stock       int     `json:"stock"`
	Unit        string  `json:"unit,omitempty"`
	BaseQty     float64 `json:"baseQty,omitempty"`
}

// ByMeasure reports whether the product is sold by measure: it has
// a unit and a positive base quantity its prices are for.
func (p Product) ByMeasure() bool {
	return p.Unit != "" && p.BaseQty > 0
}

// ScaledList is the list price for qty of the product's measure:
// the base price scaled in proportion, rounded half up to whole
// cedis (money.Round, the JavaScript semantics every amount here
// uses). It never returns less than ScaledFloor: rounding on a
// tiny quantity must not undercut the merchant's proportional
// floor, so the floor wins that tie.
func (p Product) ScaledList(qty float64) float64 {
	list := money.Round(p.ListPrice * qty / p.BaseQty)
	if floor := p.ScaledFloor(qty); list < floor {
		return floor
	}
	return list
}

// ScaledFloor is the floor for qty of the product's measure: the
// base floor scaled in proportion, rounded UP, so rounding can
// never dip the agent's stop below the merchant's proportional
// floor by a fraction of a cedi.
func (p Product) ScaledFloor(qty float64) float64 {
	return math.Ceil(p.FloorPrice * qty / p.BaseQty)
}

// UnitPrice is the price of one unit of the measure, computed the
// same way as any scaled list price: GH₵75 for one yard of a
// GH₵450 six-yard piece. It is the figure a buyer is quoted when
// she asks the per-unit price.
func (p Product) UnitPrice() float64 {
	return p.ScaledList(1)
}

// Merchant is one shop in the catalogue. The real world model is
// one shared agent number with many shops on it, so a shop carries
// the three things that tie it to the world:
//
//   - ShopCode, the shop's unique entry code. Her share link carries
//     it (wa.me prefilled text "SHOP-<code>"), and a buyer who sends
//     it is routed to her shop and only her shop.
//   - OwnerWhatsApp, the merchant's own number. She onboards and runs
//     her shop by chatting the shared number from it; sale notes go
//     back to it.
//   - PayoutPhone, where her money settles. Every order records it,
//     so a payout can never be pointed at another shop's owner.
//
// A shop also carries its delivery terms, because real buyers ask
// about delivery before they ask the item price, and they want one
// total, not two figures to add up themselves (my own buying
// chats):
//
//   - DeliveryFee, in cedis like every other amount in this store:
//     what the buyer pays for delivery. Zero means no fixed fee;
//     delivery is arranged with the seller.
//   - DeliveryNote, how the goods travel, in the merchant's words:
//     "Sent by bus to your station; you collect it the next day."
//   - DeliveryPaidTo, who the delivery money goes to: "carrier"
//     or "merchant". Buyers mostly pay their own delivery,
//     separately, to whoever physically brings the item when it
//     arrives, and the delivery money never joins the payment for
//     the product; some merchants take it themselves, inside the
//     order total. It is the merchant's decision, per shop.
//     Unset means "carrier": the
//     common case is the default, and a shop only leaves it by
//     her own word in chat.
type Merchant struct {
	ID                     string    `json:"id"`
	Name                   string    `json:"name"`
	DailyGiveawayAllowance float64   `json:"dailyGiveawayAllowance"`
	Products               []Product `json:"products"`
	OwnerWhatsApp          string    `json:"ownerWhatsApp,omitempty"`
	ShopCode               string    `json:"shopCode,omitempty"`
	PayoutPhone            string    `json:"payoutPhone,omitempty"`
	DeliveryFee            float64   `json:"deliveryFee,omitempty"`
	DeliveryNote           string    `json:"deliveryNote,omitempty"`
	DeliveryPaidTo         string    `json:"deliveryPaidTo,omitempty"`
}

// Catalogue is the whole store file: merchants by id.
type Catalogue struct {
	Merchants map[string]Merchant `json:"merchants"`
}

// seed is the synthetic demo data, written when no catalogue file
// exists yet. Two shops, so two-shop behaviour on the one shared
// number is provable out of the box.
var seed = Catalogue{
	Merchants: map[string]Merchant{
		"ama": {
			ID:   "ama",
			Name: "Ama's Stall",
			// Ama is the second seeded shop. Her owner number is empty
			// in the seed; tests and simulations set owners explicitly.
			DailyGiveawayAllowance: 200,
			ShopCode:               "AMA",
			DeliveryFee:            15,
			DeliveryNote:           "Rider brings it to you in town, same day.",
			DeliveryPaidTo:         "carrier",
			Products: []Product{
				{
					ID:          "smoked-tilapia",
					Name:        "Smoked Tilapia",
					Description: "Smoked tilapia from Lake Volta, well dried.",
					ListPrice:   180,
					FloorPrice:  140,
					Stock:       10,
				},
				{
					ID:          "groundnut-paste",
					Name:        "Groundnut Paste (1kg)",
					Description: "Stone-ground groundnut paste, nothing added.",
					ListPrice:   95,
					FloorPrice:  75,
					Stock:       25,
				},
			},
		},
		"demo": {
			ID:   "demo",
			Name: "Efua's Store",
			// The demo shop's owner number is empty in the seed: the
			// MERCHANT_WHATSAPP environment variable owns it, as
			// before, so the single-shop path is unchanged.
			ShopCode:       "EFUA",
			DeliveryFee:    25,
			DeliveryNote:   "Sent by bus to your station; you collect it the next day.",
			DeliveryPaidTo: "carrier",
			// Total discount (in GH₵) the agent may give away across a
			// whole day. As it drains, the agent haggles firmer. See
			// package daybook.
			DailyGiveawayAllowance: 300,
			Products: []Product{
				{
					ID:          "ankara",
					Name:        "Ankara Fabric (6 yards)",
					Description: "Bright wax-print cotton, 6 yards.",
					ListPrice:   450,
					FloorPrice:  360,
					Stock:       12,
					Unit:        "yard",
					BaseQty:     6,
				},
				{
					ID:          "beaded-bag",
					Name:        "Handmade Beaded Bag",
					Description: "Hand-beaded evening bag with a gold clasp.",
					ListPrice:   850,
					FloorPrice:  680,
					Stock:       3,
				},
				{
					ID:          "shea",
					Name:        "Raw Shea Butter (500g)",
					Description: "Unrefined shea butter from Tamale, for skin and hair; keeps the moisture in.",
					ListPrice:   120,
					FloorPrice:  95,
					Stock:       40,
				},
			},
		},
	},
}

// FilePath returns the path of the catalogue file.
func FilePath() string {
	return datadir.Path("catalogue.json")
}

func marshalJSON(v any) ([]byte, error) {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(buf.String(), "\n")), nil
}

// LoadCatalogue reads the catalogue file, writing and returning the
// seed first when the file does not exist.
func LoadCatalogue() Catalogue {
	path := FilePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if b, err := marshalJSON(seed); err == nil {
			_ = os.WriteFile(path, b, 0o644)
		}
		return seed
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Catalogue{Merchants: map[string]Merchant{}}
	}
	var c Catalogue
	if err := json.Unmarshal(b, &c); err != nil {
		return Catalogue{Merchants: map[string]Merchant{}}
	}
	if c.Merchants == nil {
		c.Merchants = map[string]Merchant{}
	}
	return c
}

// SaveCatalogue writes the catalogue back to its file.
func SaveCatalogue(c Catalogue) {
	path := FilePath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if b, err := marshalJSON(c); err == nil {
		_ = os.WriteFile(path, b, 0o644)
	}
}

// GetMerchant returns the merchant with the given id, or nil.
func GetMerchant(merchantID string) *Merchant {
	c := LoadCatalogue()
	m, ok := c.Merchants[merchantID]
	if !ok {
		return nil
	}
	return &m
}

// sortedIDs returns the catalogue's merchant ids in a stable order,
// so lookups that scan shops resolve the same way on every call.
func sortedIDs(c Catalogue) []string {
	ids := make([]string, 0, len(c.Merchants))
	for id := range c.Merchants {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

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
// the catalogue (EFUA, then EFUA2, and so on).
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
	for i := 2; ; i++ {
		candidate := code + strconv.Itoa(i)
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
		Products:      []Product{},
	}
	if c.Merchants == nil {
		c.Merchants = map[string]Merchant{}
	}
	c.Merchants[id] = m
	SaveCatalogue(c)
	return m, nil
}

// ListProducts returns the merchant's products, or none.
func ListProducts(merchantID string) []Product {
	m := GetMerchant(merchantID)
	if m == nil {
		return nil
	}
	return m.Products
}

// Validation errors from the store's write operations. They are
// worded as plain fragments so the chat layer can surface them almost
// as-is ("Nothing changed: ..."). Validation lives here, in the
// store, so no caller can write a shop the haggling engine would
// trip on: prices positive, a floor strictly below its list price,
// item names unique within the shop.
var (
	ErrMerchantNotFound  = errors.New("that shop is not in the catalogue")
	ErrEmptyName         = errors.New("a product needs a name")
	ErrDuplicateName     = errors.New("you already sell something with that name")
	ErrProductNotFound   = errors.New("there is no product with that name in the catalogue")
	ErrListPrice         = errors.New("the list price must be more than zero")
	ErrListBelowFloor    = errors.New("the list price must stay above the floor price")
	ErrFloorPrice        = errors.New("the floor price must be more than zero and below the list price")
	ErrNegativeAllowance = errors.New("the allowance cannot be negative")
)

var wordSplit = regexp.MustCompile(`[^a-z0-9]+`)

// FindProduct loosely matches a product against customer text: any
// significant word from the product name appearing in the customer's
// text counts as a mention. Good enough until the model takes over.
func FindProduct(merchantID, text string) *Product {
	q := strings.ToLower(text)
	for _, p := range ListProducts(merchantID) {
		if strings.Contains(q, p.ID) {
			pp := p
			return &pp
		}
		for _, w := range wordSplit.Split(strings.ToLower(p.Name), -1) {
			if len(w) > 3 && strings.Contains(q, w) {
				pp := p
				return &pp
			}
		}
	}
	return nil
}

// indexByName resolves a name against a product slice the way the
// merchant herself means it: an exact match ignoring case first,
// then a product whose name contains what she typed (so "beaded bag"
// finds "Handmade Beaded Bag"). It returns -1 when nothing matches.
func indexByName(products []Product, name string) int {
	q := strings.ToLower(strings.TrimSpace(name))
	if q == "" {
		return -1
	}
	for i, p := range products {
		if strings.ToLower(p.Name) == q {
			return i
		}
	}
	for i, p := range products {
		if strings.Contains(strings.ToLower(p.Name), q) {
			return i
		}
	}
	return -1
}

// FindByName finds a product by the name the merchant herself uses.
// Merchant commands resolve through this, never through the looser
// customer matcher.
func FindByName(merchantID, name string) *Product {
	products := ListProducts(merchantID)
	if i := indexByName(products, name); i >= 0 {
		return &products[i]
	}
	return nil
}

func slugify(name string) string {
	slug := strings.Trim(wordSplit.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		slug = "item"
	}
	return slug
}

func uniqueID(products []Product, base string) string {
	taken := map[string]bool{}
	for _, p := range products {
		taken[p.ID] = true
	}
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !taken[candidate] {
			return candidate
		}
	}
}

// updateMerchant loads the catalogue, applies fn to one merchant and
// saves the file back, with the same write discipline as the day book
// and the order book: the whole JSON file, indented, in one write.
func updateMerchant(merchantID string, fn func(*Merchant) error) error {
	c := LoadCatalogue()
	m, ok := c.Merchants[merchantID]
	if !ok {
		return ErrMerchantNotFound
	}
	if err := fn(&m); err != nil {
		return err
	}
	c.Merchants[merchantID] = m
	SaveCatalogue(c)
	return nil
}

func productIndex(m *Merchant, name string) int {
	return indexByName(m.Products, name)
}

// AddProduct validates and adds a product to the merchant's shop and
// persists it. An empty ID is filled with a slug of the name, made
// unique within the shop.
func AddProduct(merchantID string, p Product) (Product, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return Product{}, ErrEmptyName
	}
	if p.ListPrice <= 0 {
		return Product{}, ErrListPrice
	}
	if p.FloorPrice <= 0 || p.FloorPrice >= p.ListPrice {
		return Product{}, ErrFloorPrice
	}
	stored := p
	err := updateMerchant(merchantID, func(m *Merchant) error {
		for _, existing := range m.Products {
			if strings.EqualFold(existing.Name, p.Name) {
				return ErrDuplicateName
			}
		}
		stored = p
		if stored.ID == "" {
			stored.ID = slugify(p.Name)
		}
		stored.ID = uniqueID(m.Products, stored.ID)
		m.Products = append(m.Products, stored)
		return nil
	})
	if err != nil {
		return Product{}, err
	}
	return stored, nil
}

// UpdateListPrice corrects a product's list price. The new list price
// must stay above the product's floor.
func UpdateListPrice(merchantID, name string, listPrice float64) (Product, error) {
	if listPrice <= 0 {
		return Product{}, ErrListPrice
	}
	var updated Product
	err := updateMerchant(merchantID, func(m *Merchant) error {
		i := productIndex(m, name)
		if i < 0 {
			return ErrProductNotFound
		}
		if listPrice <= m.Products[i].FloorPrice {
			return ErrListBelowFloor
		}
		m.Products[i].ListPrice = listPrice
		updated = m.Products[i]
		return nil
	})
	return updated, err
}

// UpdateFloorPrice corrects a product's floor price. The new floor
// must stay below the product's list price.
func UpdateFloorPrice(merchantID, name string, floorPrice float64) (Product, error) {
	var updated Product
	err := updateMerchant(merchantID, func(m *Merchant) error {
		i := productIndex(m, name)
		if i < 0 {
			return ErrProductNotFound
		}
		if floorPrice <= 0 || floorPrice >= m.Products[i].ListPrice {
			return ErrFloorPrice
		}
		m.Products[i].FloorPrice = floorPrice
		updated = m.Products[i]
		return nil
	})
	return updated, err
}

// RemoveProduct takes a product out of the merchant's shop and
// returns what was removed.
func RemoveProduct(merchantID, name string) (Product, error) {
	var removed Product
	err := updateMerchant(merchantID, func(m *Merchant) error {
		i := productIndex(m, name)
		if i < 0 {
			return ErrProductNotFound
		}
		removed = m.Products[i]
		m.Products = append(m.Products[:i], m.Products[i+1:]...)
		return nil
	})
	return removed, err
}

// SetAllowance sets the merchant's daily giveaway allowance: the pool
// every haggle draws from. The day book reads it from the shop, so a
// change here is what new negotiations firm up against.
func SetAllowance(merchantID string, amount float64) error {
	if amount < 0 {
		return ErrNegativeAllowance
	}
	return updateMerchant(merchantID, func(m *Merchant) error {
		m.DailyGiveawayAllowance = amount
		return nil
	})
}

// ErrNegativeDeliveryFee is returned when a delivery fee below zero
// is set.
var ErrNegativeDeliveryFee = errors.New("the delivery fee cannot be negative")

// SetDeliveryFee sets what the shop's buyers pay for delivery, on top
// of the agreed item price. Zero clears it: no fixed fee, delivery is
// arranged with the seller. The fee is a pass-through, never a
// discount: haggling never touches it.
func SetDeliveryFee(merchantID string, amount float64) error {
	if amount < 0 {
		return ErrNegativeDeliveryFee
	}
	return updateMerchant(merchantID, func(m *Merchant) error {
		m.DeliveryFee = amount
		return nil
	})
}

// SetDeliveryNote sets how the shop's goods travel, in the merchant's
// own words. Buyers hear it exactly as written when they ask about
// delivery. An empty note clears it.
func SetDeliveryNote(merchantID, note string) error {
	return updateMerchant(merchantID, func(m *Merchant) error {
		m.DeliveryNote = strings.TrimSpace(note)
		return nil
	})
}

// ErrBadMeasure is returned when a product is given a measure
// with no unit or a base quantity of zero or less: a measure the
// scaling maths would divide by, or quote nonsense from.
var ErrBadMeasure = errors.New("a measure needs a unit and a quantity above zero")

// SetMeasure makes a product sold by measure: unit is the measure's
// canonical name ("yard", "litre", "kilo") and baseQty is how much
// of it the product's existing list and floor prices are for (the
// GH₵450 price is for 6 yards). Prices themselves do not move; the
// agent scales them per quantity from here.
func SetMeasure(merchantID, name, unit string, baseQty float64) (Product, error) {
	unit = strings.TrimSpace(strings.ToLower(unit))
	if unit == "" || baseQty <= 0 {
		return Product{}, ErrBadMeasure
	}
	var updated Product
	err := updateMerchant(merchantID, func(m *Merchant) error {
		i := productIndex(m, name)
		if i < 0 {
			return ErrProductNotFound
		}
		m.Products[i].Unit = unit
		m.Products[i].BaseQty = baseQty
		updated = m.Products[i]
		return nil
	})
	return updated, err
}

// ClearMeasure takes a product back to sold whole only: the measure
// fields are emptied, and buyers are quoted and haggled on the
// whole item at its list price, as before it had a measure.
func ClearMeasure(merchantID, name string) (Product, error) {
	var updated Product
	err := updateMerchant(merchantID, func(m *Merchant) error {
		i := productIndex(m, name)
		if i < 0 {
			return ErrProductNotFound
		}
		m.Products[i].Unit = ""
		m.Products[i].BaseQty = 0
		updated = m.Products[i]
		return nil
	})
	return updated, err
}

// ErrInvalidDeliveryPaidTo is returned when a delivery-money
// destination other than "merchant" or "carrier" is set.
var ErrInvalidDeliveryPaidTo = errors.New("delivery money goes to \"merchant\" or \"carrier\"")

// SetDeliveryPaidTo sets who takes the shop's delivery money:
// "merchant" (the fee rides inside the total her buyers pay here)
// or "carrier" (the buyer pays whoever brings the item, on
// arrival, and the fee never joins her money). It is her decision
// alone, changed in her own chat; see DeliveryPaidTo's default.
func SetDeliveryPaidTo(merchantID, who string) error {
	if who != "merchant" && who != "carrier" {
		return ErrInvalidDeliveryPaidTo
	}
	return updateMerchant(merchantID, func(m *Merchant) error {
		m.DeliveryPaidTo = who
		return nil
	})
}
