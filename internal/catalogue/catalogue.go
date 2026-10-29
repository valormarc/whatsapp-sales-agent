// Package catalogue is the per-merchant catalogue store. A JSON file
// for the hackathon build; the shape mirrors what merchant photo
// onboarding will write once SAM 3.1 lands.
package catalogue

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/money"
)

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
	Unit        string  `json:"unit,omitempty"`
	BaseQty     float64 `json:"baseQty,omitempty"`
	// Photos are the seller's own pictures of this product,
	// kept on the listing (paths under the data directory)
	// so a buyer who asks for the pic can be sent them. One
	// product holds many photos: the angles and colours she
	// sent of the same goods.
	Photos []string `json:"photos,omitempty"`
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
	SubaccountCode         string    `json:"subaccountCode,omitempty"`
	PayoutHolderName       string    `json:"payoutHolderName,omitempty"`
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
