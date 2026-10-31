// A shop's shelf: finding products in a buyer's words, and
// the merchant's own edits (add, reprice, remove, measures,
// delivery settings) with their validation.
package catalogue

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/datadir"
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
// as-is ("Nothing changed: ..."). They are worded the way the seller
// speaks: her FloorPrice is her last price. Validation lives here,
// in the store, so no caller can write a shop the haggling engine
// would trip on: prices positive, a last price below its price (or
// equal to it, for a fixed-price product), item names unique within
// the shop.
var (
	ErrMerchantNotFound  = errors.New("that shop is not in the catalogue")
	ErrEmptyName         = errors.New("a product needs a name")
	ErrDuplicateName     = errors.New("you already sell something with that name")
	ErrProductNotFound   = errors.New("there is no product with that name in the catalogue")
	ErrListPrice         = errors.New("the price must be more than zero")
	ErrStock             = errors.New("the stock count cannot be below zero")
	ErrListBelowFloor    = errors.New("the price must stay above the last price")
	ErrFloorPrice        = errors.New("the last price must be more than zero and not above the price")
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
// The cycle lock is held from load to save, so two cycles
// can never interleave and lose one another's changes.
func updateMerchant(merchantID string, fn func(*Merchant) error) error {
	cycleMu.Lock()
	defer cycleMu.Unlock()
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
	// Her last price sits below her price, so there is room to
	// bargain. The one case where it equals the price is a
	// fixed-price product, and that must be said outright.
	if p.FloorPrice <= 0 || p.FloorPrice > p.ListPrice ||
		(p.FloorPrice == p.ListPrice && !p.Fixed) {
		return Product{}, ErrFloorPrice
	}
	if p.Unit != "" && p.BaseQty > 0 && p.StockUnits == 0 && p.Stock > 0 {
		p.StockUnits = float64(p.Stock) * p.BaseQty
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

// UpdateListPrice corrects a product's price. The new price must
// stay above the product's last price. A fixed-price product has
// no gap to protect: its last price is its price, so the last
// price moves with the new price and it stays fixed.
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
		if m.Products[i].Fixed {
			m.Products[i].ListPrice = listPrice
			m.Products[i].FloorPrice = listPrice
			updated = m.Products[i]
			return nil
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

// UpdateFloorPrice corrects a product's last price. A last price
// below the price leaves room to bargain, and clears any fixed
// flag. A last price equal to the price is her saying the price
// does not move: the product becomes fixed price.
func UpdateFloorPrice(merchantID, name string, floorPrice float64) (Product, error) {
	var updated Product
	err := updateMerchant(merchantID, func(m *Merchant) error {
		i := productIndex(m, name)
		if i < 0 {
			return ErrProductNotFound
		}
		if floorPrice <= 0 || floorPrice > m.Products[i].ListPrice {
			return ErrFloorPrice
		}
		m.Products[i].FloorPrice = floorPrice
		m.Products[i].Fixed = floorPrice == m.Products[i].ListPrice
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
	if err == nil {
		// The product's photo files go with it: a removed
		// product's pictures are never sent again and never
		// left orphaned on the disk (see deleteOrphanedPhoto).
		for _, rel := range removed.Photos {
			deleteOrphanedPhoto(rel)
		}
	}
	return removed, err
}

// ExtraPhotoRefs, when the agent sets it, reports stored
// photo paths held OUTSIDE the catalogue — session-held
// photo lists, burst staging, an open listing's kept
// photos — that are still live. The catalogue cannot see
// those stores, and the second audit (needs-validation)
// found orphan deletion scanning only products: a file a
// session still names could be deleted from under a send
// that re-resolves it. The agent registers its stores at
// startup; until it does, product references alone decide.
var ExtraPhotoRefs func() []string

// PhotoReferenced reports whether anything still names
// this stored photo path: any product in any shop, or a
// live reference the agent registered. A legacy FLAT path
// (photos/<file>, or a bare <file> from the oldest
// records) is one file under several spellings, so a flat
// path counts as referenced while any flat reference
// shares its file name; scoped paths (photos/<shop>/…)
// are distinct files and match exactly.
func PhotoReferenced(rel string) bool {
	flat := filepath.Dir(rel) == "." || filepath.Dir(rel) == "photos"
	for _, m := range ListMerchants() {
		for _, p := range m.Products {
			for _, r := range p.Photos {
				if r == rel {
					return true
				}
				if flat && (filepath.Dir(r) == "." || filepath.Dir(r) == "photos") && filepath.Base(r) == filepath.Base(rel) {
					return true
				}
			}
		}
	}
	if ExtraPhotoRefs != nil {
		for _, r := range ExtraPhotoRefs() {
			if r == rel {
				return true
			}
		}
	}
	return false
}

func photoReferenced(rel string) bool {
	return PhotoReferenced(rel)
}

// deleteOrphanedPhoto removes a stored photo file once
// nothing references it. A path still named by any product
// (a legacy flat path can be shared by two shops' listings
// from before paths were shop-scoped) is left alone.
func deleteOrphanedPhoto(rel string) {
	if rel == "" || photoReferenced(rel) {
		return
	}
	// A flat reference — the oldest records' spelling, a
	// bare file name or one under photos/ — names its
	// file at photos/<base>, the location the photo
	// readers resolve it to. Removing the verbatim path
	// deleted nothing real and left the actual file
	// orphaned on disk forever (the fifth audit). Scoped
	// references keep the verbatim removal.
	if dir := filepath.Dir(rel); dir == "." || dir == "photos" {
		_ = os.Remove(datadir.Path(filepath.Join("photos", filepath.Base(rel))))
		return
	}
	_ = os.Remove(datadir.Path(rel))
}

// SetAllowance sets the merchant's daily giveaway allowance: the pool
// every haggle draws from. The day book reads it from the shop, so a
// change here is what new negotiations firm up against. Setting it —
// zero included — marks the choice as hers, so the agent stops
// reminding her to make it.
func SetAllowance(merchantID string, amount float64) error {
	if amount < 0 {
		return ErrNegativeAllowance
	}
	return updateMerchant(merchantID, func(m *Merchant) error {
		m.DailyGiveawayAllowance = amount
		m.AllowanceSet = true
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

// SetSubaccount records the shop's own payment account: the name
// on the MoMo account its link money settles to, and the Seev
// subaccount code once creation succeeds. The holder name is kept
// even when creation fails, so a retry needs nothing re-asked.
// The phone the code was minted for is recorded with it, so the
// binding between code and number is a fact of the record, not
// an assumption (see SubaccountPhone).
func SetSubaccount(merchantID, code, holderName, phone string) error {
	return updateMerchant(merchantID, func(m *Merchant) error {
		if code != "" {
			m.SubaccountCode = code
			m.SubaccountPhone = phone
		}
		if holderName != "" {
			m.PayoutHolderName = holderName
		}
		return nil
	})
}

// SetPayoutDetails switches where a shop's money settles,
// all of it in one write: the MoMo number, the name on that
// account, and the Seev subaccount code that pays the
// number. The three move together or not at all — a record
// showing a new number while links still route through an
// old account is a mismatch money can fall into, so the
// caller creates and verifies the replacement account
// first and calls this only with a code in hand.
func SetPayoutDetails(merchantID, phone, holderName, subaccountCode string) error {
	return updateMerchant(merchantID, func(m *Merchant) error {
		m.PayoutPhone = phone
		m.PayoutHolderName = holderName
		m.SubaccountCode = subaccountCode
		m.SubaccountPhone = phone
		return nil
	})
}

// SetDeliveryStyle sets how the shop's delivery fees are
// known: "set" (given up front) or "quote" (the seller names
// the fee for each delivery order as it comes).
func SetDeliveryStyle(merchantID, style string) error {
	if style != "set" && style != "quote" {
		return errors.New("the delivery style must be set or quote")
	}
	return updateMerchant(merchantID, func(m *Merchant) error {
		m.DeliveryStyle = style
		return nil
	})
}

// SetDeliveryArea sets one named area's own delivery fee in
// a "set" shop, replacing any fee already recorded for that
// area (matched by name, case-insensitively). A fee of zero
// removes the area, back to the standard fee.
func SetDeliveryArea(merchantID, area string, fee float64) error {
	area = strings.TrimSpace(area)
	if area == "" {
		return errors.New("the area needs a name")
	}
	if fee < 0 {
		return ErrNegativeDeliveryFee
	}
	return updateMerchant(merchantID, func(m *Merchant) error {
		out := m.DeliveryAreas[:0]
		for _, a := range m.DeliveryAreas {
			if !strings.EqualFold(a.Area, area) {
				out = append(out, a)
			}
		}
		m.DeliveryAreas = out
		if fee > 0 {
			m.DeliveryAreas = append(m.DeliveryAreas, AreaFee{Area: area, Fee: fee})
		}
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

// AvailableUnits is how much of the product can still be
// sold, counted in the product's own measure: pieces for a
// plain product, base units (yards, litres) for a by-measure
// one. It is the figure every sale is checked against.
func (p Product) AvailableUnits() float64 {
	if p.Unit != "" && p.BaseQty > 0 {
		if p.StockUnits > 0 {
			return p.StockUnits
		}
		return float64(p.Stock) * p.BaseQty
	}
	return float64(p.Stock)
}

// applyTake deducts units from a product in place, keeping
// the piece count honest for by-measure goods: the count is
// the whole pieces the remaining units still make, so a
// partly cut last piece still reads as one in stock.
func applyTake(p *Product, units float64) {
	if p.Unit != "" && p.BaseQty > 0 {
		rem := p.AvailableUnits() - units
		if rem <= 0 {
			p.Stock, p.StockUnits = 0, 0
			return
		}
		p.StockUnits = rem
		p.Stock = int(math.Ceil(rem / p.BaseQty))
		return
	}
	p.Stock -= int(units)
	if p.Stock < 0 {
		p.Stock = 0
	}
}

// applyGive returns units to a product in place: a sale
// that died unpaid puts its goods back on the shelf.
func applyGive(p *Product, units float64) {
	if p.Unit != "" && p.BaseQty > 0 {
		rem := p.AvailableUnits() + units
		p.StockUnits = rem
		p.Stock = int(math.Ceil(rem / p.BaseQty))
		return
	}
	// A plain product's stock counts whole pieces: a
	// fractional give is never truncated into the shelf
	// (the sixth audit) — it is refused whole, the mirror
	// of TakeStock's plain-product validation. Lines
	// recorded at a sale carry their take-time measure,
	// so a legitimate by-measure give-back never arrives
	// here as a bare fraction.
	if units != math.Trunc(units) {
		return
	}
	p.Stock += int(units)
}

// applyGiveMeasured restores units in the denomination
// they were taken in (the sixth audit): the line's
// recorded measure governs, even when the product's own
// measure has since been cleared or re-based, so the
// give-back lands whole instead of being judged — and
// truncated — against the product's current shape.
func applyGiveMeasured(p *Product, units float64, baseQty float64) {
	current := p.StockUnits
	if !(p.Unit != "" && p.BaseQty > 0) {
		current = float64(p.Stock) * baseQty
	}
	rem := current + units
	p.StockUnits = rem
	p.Stock = int(math.Ceil(rem / baseQty))
}

// TakeStock deducts units from a product's shelf, reporting
// whether the shelf could cover them. Nothing is deducted
// when it cannot: a sale either takes all its lines or none.
func TakeStock(merchantID, name string, units float64) bool {
	// A take is a removal: a non-positive quantity is
	// refused at the boundary. Without the guard a
	// negative take ADDED stock (the fourth audit's
	// parent check took -3 and watched the shelf rise
	// 10 -> 13); every production caller is gated to
	// positive journal quantities today, and the
	// boundary is where that gate belongs.
	if units <= 0 {
		return false
	}
	took := false
	_ = updateMerchant(merchantID, func(m *Merchant) error {
		i := productIndex(m, name)
		if i < 0 {
			return ErrProductNotFound
		}
		// A plain product is counted in whole pieces: a
		// fractional take on one passed the float
		// sufficiency check and then deducted only its
		// truncated whole part — 0.5 against a stock of
		// 1 deducted nothing and still reported success
		// (the fifth audit). By-measure products take
		// fractions by their nature and are untouched.
		p := &m.Products[i]
		if (p.Unit == "" || p.BaseQty <= 0) && units != math.Trunc(units) {
			return nil
		}
		if p.AvailableUnits() < units {
			return nil
		}
		applyTake(p, units)
		took = true
		return nil
	})
	return took
}

// GiveStock returns units to a product's shelf. A product
// that has left the catalogue takes nothing back.
func GiveStock(merchantID, name string, units float64) {
	GiveStockMeasured(merchantID, name, units, "", 0)
}

// GiveStockMeasured returns units taken in a recorded
// measure (unit/baseQty from the sale's stock line): the
// restore is denominated by the take-time measure, not
// the product's current one (the sixth audit). A line
// with no recorded measure takes the plain road.
func GiveStockMeasured(merchantID, name string, units float64, unit string, baseQty float64) {
	_ = updateMerchant(merchantID, func(m *Merchant) error {
		i := productIndex(m, name)
		if i < 0 {
			return nil
		}
		if unit != "" && baseQty > 0 {
			applyGiveMeasured(&m.Products[i], units, baseQty)
			return nil
		}
		applyGive(&m.Products[i], units)
		return nil
	})
}

// SetStock sets how many of a product the shop has. The
// count is a fact she owns like her prices: it is what the
// in-hand gate reads (a count of one is a one-off, confirmed
// with her before each sale), and what her catalogue readback
// shows her. Zero is a real count: she has none just now.
func SetStock(merchantID, name string, count int) (Product, error) {
	if count < 0 {
		return Product{}, ErrStock
	}
	var updated Product
	err := updateMerchant(merchantID, func(m *Merchant) error {
		i := productIndex(m, name)
		if i < 0 {
			return ErrProductNotFound
		}
		m.Products[i].Stock = count
		if m.Products[i].Unit != "" && m.Products[i].BaseQty > 0 {
			m.Products[i].StockUnits = float64(count) * m.Products[i].BaseQty
		}
		updated = m.Products[i]
		return nil
	})
	return updated, err
}

// SetProductPhotos records the kept photo paths on a saved
// product, after the product exists to hold them.
func SetProductPhotos(merchantID, productID string, photos []string) error {
	var dropped []string
	err := updateMerchant(merchantID, func(m *Merchant) error {
		for i := range m.Products {
			if m.Products[i].ID == productID {
				for _, old := range m.Products[i].Photos {
					kept := false
					for _, n := range photos {
						if n == old {
							kept = true
							break
						}
					}
					if !kept {
						dropped = append(dropped, old)
					}
				}
				m.Products[i].Photos = photos
				return nil
			}
		}
		return ErrProductNotFound
	})
	if err == nil {
		for _, rel := range dropped {
			deleteOrphanedPhoto(rel)
		}
	}
	return err
}

// FindByID returns the product with this exact id inside a
// shop, or nil. Ids are how a shown discovery result is
// picked back up without re-reading names.
func FindByID(merchantID, productID string) *Product {
	for _, p := range ListProducts(merchantID) {
		if p.ID == productID {
			found := p
			return &found
		}
	}
	return nil
}
