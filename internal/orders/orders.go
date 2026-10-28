// Package orders is the order book: the small ledger between a deal
// agreed in chat and the money actually arriving. A pending order is
// recorded under our reference (and Seev's session reference when
// there is one); the Seev webhook settles it. JSON file, same pattern
// as the day book.
package orders

import (
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/datadir"
)

// Order is one recorded order. ProviderRef is Seev's session reference,
// or nil when the checkout was a dry-run placeholder. MerchantID is
// the shop the sale belongs to: settlement resolves the shop from it,
// so the money story (owner notified, payout destination) is always
// the selling shop's own. PayoutPhone records that shop's payout
// destination at the moment of the sale.
//
// Price is the agreed item price, the figure the haggle settled and
// the day book books. DeliveryFee is the shop's delivery charge on
// top of it, a pass-through the buyer pays so the goods can travel;
// Total is Price plus DeliveryFee, the amount the checkout actually
// charges. Orders recorded before delivery terms existed carry a
// zero Total and charge at Price (see Charged).
//
// Fulfilment records how the goods reach the buyer, settled in chat
// after the price was agreed: "delivery" (sent to Location, fee in
// DeliveryFee), "pickup" (the buyer collects, or a Collector she
// named does), "meet" (a meet-up at Location, the seller to agree
// the place herself) or "arranged" (the shop has no delivery terms;
// the seller arranges it with the buyer, who is in Location when
// she gave one). Pickup and meet-ups never carry a delivery fee.
//
// DeliveryPaidTo records who the quoted DeliveryFee goes to on a
// delivery order, the shop's own setting at the moment of the sale:
// "carrier" (the buyer pays whoever physically brings the item, on
// arrival; the fee is never part of Total and never the merchant's
// money) or "merchant" (the fee rides inside Total, as charged).
//
// PaymentPath is how the buyer pays the charged total: "" while
// she has not chosen, "seev" when the hosted link settles, "direct"
// when she sends MoMo straight to the merchant's own number.
// SettledVia records whose word settled a direct order:
// "merchant-confirmed", the merchant's own yes in chat. A Seev
// settlement needs no SettledVia; PaymentPath "seev" says it.
//
// The Claim fields are a direct payment's claim state. A
// screenshot, or the buyer's word alone, is a CLAIM, never proof:
// ClaimStatus runs "open" (with the merchant to confirm),
// "confirmed" or "rejected", and nothing in these fields can move
// an order to paid by itself. ClaimAmount is what the buyer said
// she sent (the order total when she named nothing); ClaimMismatch
// flags that it differs from the total she owed. ScreenshotRef is
// the inbound media reference of her screenshot, when one came.
type Order struct {
	Reference   string  `json:"reference"`
	Status      string  `json:"status"`
	From        string  `json:"from"`
	ProductName string  `json:"productName"`
	Price       float64 `json:"price"`
	DeliveryFee float64 `json:"deliveryFee,omitempty"`
	Total       float64 `json:"total,omitempty"`
	ProviderRef *string `json:"providerRef"`
	MerchantID  string  `json:"merchantId,omitempty"`
	PayoutPhone string  `json:"payoutPhone,omitempty"`
	Fulfilment  string  `json:"fulfilment,omitempty"`
	Location    string  `json:"location,omitempty"`
	Collector   string  `json:"collector,omitempty"`

	// PackageName is the name the buyer asked to have written on
	// the package (hers, or the person the goods are going to);
	// Note is a message she added for the seller with the order.
	// Both are collected after she chooses how the goods reach
	// her, and both ride to the merchant in her order notice.
	PackageName string `json:"packageName,omitempty"`
	Note        string `json:"note,omitempty"`

	// Quantity and Unit record a sale by measure: how much of the
	// product's measure the buyer took (3 of "yard"), against a
	// Price that is the settled price for that quantity, scaled
	// from the product's base quantity. Zero Quantity means the
	// whole item, as every order before this field existed.
	Quantity float64 `json:"quantity,omitempty"`
	Unit     string  `json:"unit,omitempty"`

	DeliveryPaidTo string  `json:"deliveryPaidTo,omitempty"`
	PaymentPath    string  `json:"paymentPath,omitempty"`
	SettledVia     string  `json:"settledVia,omitempty"`
	ClaimStatus    string  `json:"claimStatus,omitempty"`
	ClaimAmount    float64 `json:"claimAmount,omitempty"`
	ClaimMismatch  bool    `json:"claimMismatch,omitempty"`
	ClaimedAt      string  `json:"claimedAt,omitempty"`
	ClaimShot      bool    `json:"claimScreenshot,omitempty"`
	ScreenshotRef  string  `json:"screenshotRef,omitempty"`

	// CreatedAt is when the order was placed, RFC3339. Orders
	// placed before this field existed carry none; the unpaid
	// watch stamps those on first sight rather than treating
	// them as ancient.
	CreatedAt string `json:"createdAt,omitempty"`
	// UnpaidAlerted marks that the unpaid watch has already
	// told both sides about this order sitting unpaid, so it
	// alerts once and never nags.
	UnpaidAlerted bool `json:"unpaidAlerted,omitempty"`
}

// All returns every order in the book, in reference order, for
// the sweeps that walk the whole book (the unpaid watch).
func (b *Book) All() []Order {
	all := b.loadAll()
	refs := make([]string, 0, len(all))
	for ref := range all {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	out := make([]Order, 0, len(refs))
	for _, ref := range refs {
		out = append(out, all[ref])
	}
	return out
}

// Charged is the amount the buyer was actually charged for this
// order: the recorded total when there is one, else the item price.
func (o Order) Charged() float64 {
	if o.Total > 0 {
		return o.Total
	}
	return o.Price
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

// Book is an order book stored in one JSON file.
type Book struct {
	file string
}

// New returns an order book backed by the given file.
func New(file string) *Book {
	return &Book{file: file}
}

// Default is the shared order book, backed by data/orders.json.
var Default = New(datadir.Path("orders.json"))

func (b *Book) loadAll() map[string]Order {
	data, err := os.ReadFile(b.file)
	if err != nil {
		return map[string]Order{}
	}
	all := map[string]Order{}
	if err := json.Unmarshal(data, &all); err != nil {
		return map[string]Order{}
	}
	return all
}

func (b *Book) saveAll(all map[string]Order) {
	_ = os.MkdirAll(datadir.Dir(), 0o755)
	if data, err := marshalJSON(all); err == nil {
		_ = os.WriteFile(b.file, data, 0o644)
	}
}

// RecordOrder records an order under our reference. The stored order
// starts as pending unless the given order already carries a status.
// It returns the stored order.
func (b *Book) RecordOrder(reference string, order Order) Order {
	all := b.loadAll()
	stored := order
	stored.Reference = reference
	if stored.Status == "" {
		stored.Status = "pending"
	}
	all[reference] = stored
	b.saveAll(all)
	return stored
}

// GetOrder returns the order with the given reference, or nil.
func (b *Book) GetOrder(reference string) *Order {
	if reference == "" {
		return nil
	}
	all := b.loadAll()
	o, ok := all[reference]
	if !ok {
		return nil
	}
	return &o
}

// FindByProviderRef finds an order by Seev's session reference, for
// webhooks that carry Seev's reference rather than ours.
func (b *Book) FindByProviderRef(providerRef string) *Order {
	if providerRef == "" {
		return nil
	}
	for _, o := range b.loadAll() {
		if o.ProviderRef != nil && *o.ProviderRef == providerRef {
			oo := o
			return &oo
		}
	}
	return nil
}

// SetStatus updates an order's status and returns the order, or nil
// when the reference is unknown.
func (b *Book) SetStatus(reference, status string) *Order {
	all := b.loadAll()
	o, ok := all[reference]
	if !ok {
		return nil
	}
	o.Status = status
	all[reference] = o
	b.saveAll(all)
	return &o
}

// Update applies a change to the stored order in place and saves
// it, returning the stored order, or nil when the reference is
// unknown. Payment state (the chosen path, a claim, a settlement)
// moves through here, one small step at a time.
func (b *Book) Update(reference string, mutate func(*Order)) *Order {
	all := b.loadAll()
	o, ok := all[reference]
	if !ok {
		return nil
	}
	mutate(&o)
	all[reference] = o
	b.saveAll(all)
	return &o
}

// refStamp is the ordering key inside a reference: the figure
// after the last dash. References are now a shop's code plus the
// shop's own serial (EFUA-0007), so the serial orders a shop's
// orders in time; the references minted before that shape were
// "HACK-" plus a millisecond stamp, whose tail orders them the
// same way. An unparseable reference sorts as the oldest of all.
func refStamp(reference string) int64 {
	n, err := strconv.ParseInt(reference[strings.LastIndex(reference, "-")+1:], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// CountFor counts a shop's orders in the book, every status. A
// new order's serial is this count plus one (bumped past any key
// already taken), so a shop's references run 0001, 0002, ... in
// the order her sales complete.
func (b *Book) CountFor(merchantID string) int {
	n := 0
	for _, o := range b.loadAll() {
		if o.MerchantID == merchantID {
			n++
		}
	}
	return n
}

// NewestFor returns the buyer's most recent order in one shop,
// whatever its status, or nil. Order checking reads here: "check
// my order" pulls up her latest one in the shop she is standing
// in, and only ever hers.
func (b *Book) NewestFor(merchantID, buyer string) *Order {
	var best *Order
	for _, o := range b.loadAll() {
		if o.MerchantID != merchantID || o.From != buyer {
			continue
		}
		oo := o
		if best == nil || refStamp(oo.Reference) > refStamp(best.Reference) {
			best = &oo
		}
	}
	return best
}

// NewestUnpaidFor returns the buyer's most recent unpaid order in
// one shop, or nil. Claims and direct-payment choices attach here:
// to her own newest open order, in the shop she is bound to, and
// never to another shop's order or another buyer's.
func (b *Book) NewestUnpaidFor(merchantID, buyer string) *Order {
	var best *Order
	for _, o := range b.loadAll() {
		if o.MerchantID != merchantID || o.From != buyer || o.Status != "pending" {
			continue
		}
		oo := o
		if best == nil || refStamp(oo.Reference) > refStamp(best.Reference) {
			best = &oo
		}
	}
	return best
}

// OldestOpenClaim returns the shop's oldest order whose direct
// payment claim is still open and unpaid, or nil. The merchant's
// yes or no settles claims in the order buyers made them, and only
// ever inside her own shop: the caller resolves her shop first and
// this lookup never crosses into another.
func (b *Book) OldestOpenClaim(merchantID string) *Order {
	var best *Order
	for _, o := range b.loadAll() {
		if o.MerchantID != merchantID || o.Status != "pending" || o.ClaimStatus != "open" {
			continue
		}
		oo := o
		if best == nil || refStamp(oo.Reference) < refStamp(best.Reference) {
			best = &oo
		}
	}
	return best
}
