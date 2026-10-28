// A shop's shelf: finding products in a buyer's words, and
// the merchant's own edits (add, reprice, remove, measures,
// delivery settings) with their validation.
package catalogue

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

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
