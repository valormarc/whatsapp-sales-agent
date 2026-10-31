// Package catalogue is the per-merchant catalogue store. A JSON file
// for the hackathon build; the shape mirrors what merchant photo
// onboarding will write once SAM 3.1 lands.
package catalogue

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/money"
)

// fileMu serialises reads and writes of the catalogue file
// itself: the file is never torn between two writers and
// never read mid-write. It does NOT make a load, modify,
// save cycle atomic — two cycles can interleave their
// loads and saves and lose one another's changes whole,
// which for a stock take means selling the same last unit
// twice. cycleMu is the lock for that: every composer of a
// load-modify-save cycle in this package holds it from
// load to save. The third audit demonstrated the gap by
// taking 46 units against a stock of 20 through the
// unlocked cycle. Lock order is cycleMu before fileMu,
// never the reverse.
var fileMu sync.Mutex
var cycleMu sync.Mutex

// writeAtomic writes data over path by way of a temporary
// file in the same directory, renamed into place: a crash
// leaves the old file or the new one, never a torn half.
// The mode is owner-only; the catalogue holds every shop's
// payout number and owner phone.
func writeAtomic(path string, data []byte) {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err == nil {
		_ = os.Rename(tmp, path)
	}
}

// Product is one item a merchant sells. Prices are in cedis; FloorPrice
// is the merchant's secret last price, which never reaches the customer.
//
// A product may be fixed price: the seller's price does not move, so
// there is nothing to bargain. Fixed is set when her last price is
// the price itself, or when she says the price is non-negotiable.
// A fixed product carries FloorPrice equal to ListPrice, and the
// agent sells it at that price only.
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
	Fixed       bool    `json:"fixed,omitempty"`
	Stock       int     `json:"stock"`
	// StockUnits is the exact remaining quantity of a
	// by-measure product, in its base unit (yards, litres):
	// a partly cut piece still counts as one in Stock, so
	// Stock alone would overstate what can still be sold.
	// Zero means "not tracked" for plain products and for
	// listings saved before this field existed; there the
	// piece count and base quantity carry the figure.
	StockUnits  float64 `json:"stockUnits,omitempty"`
	Unit        string  `json:"unit,omitempty"`
	BaseQty     float64 `json:"baseQty,omitempty"`
	// Photos are the seller's own pictures of this product,
	// kept on the listing (paths under the data directory)
	// so a buyer who asks for the pic can be sent them. One
	// product holds many photos: the angles and colours she
	// sent of the same goods.
	Photos []string `json:"photos,omitempty"`
	// Sizes are the sizes this product comes in, in the
	// seller's own terms ("37", "UK 8", "M"), read by the
	// model from her words or the goods at listing time and
	// stored as facts. A product with sizes is never sold
	// without the buyer choosing one; the choice is verified
	// against this list and lands on the order.
	Sizes []string `json:"sizes,omitempty"`
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
//   - DeliveryStyle, how the fee itself is known: "set" (the
//     fees below are given up front) or "quote" (the seller
//     names the fee for each delivery order as it comes, and
//     the buyer sees it, inside her full total, before she
//     pays anything). Unset means "set".
//   - DeliveryAreas, the exceptions to the standard fee in a
//     "set" shop: a named area and its own fee. A buyer whose
//     stated place names the area pays that area's fee; every
//     other place pays the standard DeliveryFee. She sets them
//     one message at a time, never sixteen questions.
type Merchant struct {
	ID                     string  `json:"id"`
	Name                   string  `json:"name"`
	DailyGiveawayAllowance float64 `json:"dailyGiveawayAllowance"`
	// AllowanceSet records that the merchant chose the
	// allowance herself, through the set command. Zero is a
	// real choice — a shop that gives nothing away — and
	// must read differently from never having chosen: only
	// a shop that never chose is ever reminded to choose.
	AllowanceSet     bool      `json:"allowanceSet,omitempty"`
	Products         []Product `json:"products"`
	OwnerWhatsApp    string    `json:"ownerWhatsApp,omitempty"`
	ShopCode         string    `json:"shopCode,omitempty"`
	PayoutPhone      string    `json:"payoutPhone,omitempty"`
	SubaccountCode   string    `json:"subaccountCode,omitempty"`
	// SubaccountPhone is the payout number the recorded
	// subaccount was minted for. A link may route through
	// the code only while the two agree (see the agent's
	// subaccountFor): the binding is recorded at mint time
	// so a payout change can never leave links settling
	// through an account opened for another number.
	SubaccountPhone  string    `json:"subaccountPhone,omitempty"`
	PayoutHolderName string    `json:"payoutHolderName,omitempty"`
	DeliveryFee      float64   `json:"deliveryFee,omitempty"`
	DeliveryNote     string    `json:"deliveryNote,omitempty"`
	DeliveryPaidTo   string    `json:"deliveryPaidTo,omitempty"`
	DeliveryStyle    string    `json:"deliveryStyle,omitempty"`
	DeliveryAreas    []AreaFee `json:"deliveryAreas,omitempty"`
}

// AreaFee is one named area's own delivery fee in a "set"
// shop, an exception to the shop's standard DeliveryFee.
type AreaFee struct {
	Area string  `json:"area"`
	Fee  float64 `json:"fee"`
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

// cataloguePoisoned is set under fileMu when a load met a
// catalogue that would not parse and no last-known-good
// copy could be restored: saves are then refused rather
// than cementing the empty read over every shop on the
// shelf (the fifth audit: this store still had the
// preserve-only behaviour the fourth audit condemned in
// the order book, and a routine new-shop signup was the
// save that cemented the loss).
var cataloguePoisoned bool

// LoadCatalogue reads the catalogue file, writing and returning the
// seed first when the file does not exist. A file that exists but
// does not parse is preserved byte for byte under a single
// .corrupt name and shouted about; the load then restores from
// the last-known-good copy every save leaves beside the file and
// repairs the main file with those bytes, and only when both
// copies fail does the catalogue read empty — poisoned, with
// saves refused until a load parses again. Read as silently
// empty without the poison, the next save would wipe every shop.
func LoadCatalogue() Catalogue {
	fileMu.Lock()
	defer fileMu.Unlock()
	path := FilePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		if b, err := marshalJSON(seed); err == nil {
			writeAtomic(path, b)
			writeAtomic(path+".bak", b)
		}
		cataloguePoisoned = false
		return seed
	}
	// restore tries the last known good copy, repairing
	// the main file with its bytes. Both failure roads —
	// unparseable bytes and an existing file that cannot
	// be read — go through it (the sixth audit).
	restore := func() (Catalogue, bool) {
		bak, berr := os.ReadFile(path + ".bak")
		if berr != nil {
			return Catalogue{}, false
		}
		var restored Catalogue
		if uerr := json.Unmarshal(bak, &restored); uerr != nil {
			return Catalogue{}, false
		}
		writeAtomic(path, bak)
		if restored.Merchants == nil {
			restored.Merchants = map[string]Merchant{}
		}
		cataloguePoisoned = false
		return restored, true
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if restored, ok := restore(); ok {
			fmt.Fprintf(os.Stderr, "catalogue: %s could not be read (%v); restored from the last known good copy\n", path, err)
			return restored
		}
		fmt.Fprintf(os.Stderr, "catalogue: %s could not be read (%v) and no good copy could be restored; read as empty; saves are refused until the book loads again\n", path, err)
		cataloguePoisoned = true
		return Catalogue{Merchants: map[string]Merchant{}}
	}
	var c Catalogue
	if err := json.Unmarshal(b, &c); err != nil {
		bad := path + ".corrupt"
		_ = os.WriteFile(bad, b, 0o600)
		if restored, ok := restore(); ok {
			fmt.Fprintf(os.Stderr, "catalogue: %s does not parse (%v); preserved as %s and restored from the last known good copy\n", path, err, bad)
			return restored
		}
		fmt.Fprintf(os.Stderr, "catalogue: %s does not parse (%v); preserved as %s and read as empty; saves are refused until the book parses again\n", path, err, bad)
		cataloguePoisoned = true
		return Catalogue{Merchants: map[string]Merchant{}}
	}
	if c.Merchants == nil {
		c.Merchants = map[string]Merchant{}
	}
	cataloguePoisoned = false
	return c
}

// SaveCatalogue writes the catalogue back to its file and
// refreshes the last-known-good copy with the same bytes. A
// poisoned catalogue refuses the save outright.
func SaveCatalogue(c Catalogue) {
	fileMu.Lock()
	defer fileMu.Unlock()
	if cataloguePoisoned {
		fmt.Fprintf(os.Stderr, "catalogue: refusing to save over %s: the last load did not parse and no good copy could be restored\n", FilePath())
		return
	}
	path := FilePath()
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if b, err := marshalJSON(c); err == nil {
		writeAtomic(path, b)
		writeAtomic(path+".bak", b)
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
