// Package orders is the order book: the small ledger between a deal
// agreed in chat and the money actually arriving. A pending order is
// recorded under our reference (and Seev's session reference when
// there is one); the Seev webhook settles it. JSON file, same pattern
// as the day book.
package orders

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

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
// PaymentPath records how the order was settled: "seev" when
// the hosted payment link settles it, which is the only way an
// order is ever paid. It stays empty while the order is open.
// SettledVia is a legacy field from the removed direct-payment
// path; nothing writes it now, and old books keep whatever they
// recorded then.
// Unpaid reports whether the order is still open and
// owing: a live checkout is waiting on her (pending), or
// its link never came up (link_failed) and she still has
// no way to pay it. Both are the same fact to the buyer —
// the money has not moved and the order is hers to pay —
// and every path that serves her about paying keys on
// this, never on one status word alone.
func (o *Order) Unpaid() bool {
	return o.Status == "pending" || o.Status == "link_failed"
}

// OrderItem is one line of a multi-item order: what it is,
// the price agreed for it, its measure when it was sold by
// one, and its size when the buyer chose one.
// StockLine is one line of an order's shelf consumption:
// the product's name and the units the order took, in the
// product's own measure.
type StockLine struct {
	Name  string  `json:"name"`
	Units float64 `json:"units"`
	// Unit and BaseQty record the measure the units were
	// taken in (the sixth audit): a give-back restores in
	// the take-time denomination, so a product whose
	// measure was cleared or re-based after the take
	// still receives its units whole instead of a
	// fraction truncated against its current shape.
	Unit    string  `json:"unit,omitempty"`
	BaseQty float64 `json:"baseQty,omitempty"`
}

type OrderItem struct {
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Quantity float64 `json:"quantity,omitempty"`
	Unit     string  `json:"unit,omitempty"`
	Size     string  `json:"size,omitempty"`
	// ListPrice is the list figure this line was booked
	// into the day book at (the sixth audit): a
	// cancellation reverses the booking line by line at
	// the figures it was booked with, never recomputed
	// from a shelf that has moved since.
	ListPrice float64 `json:"listPrice,omitempty"`
}

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

	// DeliveryPhone and Landmark are the delivery details a
	// details form can carry (see the agent's flows): the
	// number the rider calls, and the landmark or station
	// that finds her. They ride to the merchant in the same
	// notice, beside the package name.
	DeliveryPhone string `json:"deliveryPhone,omitempty"`
	Landmark      string `json:"landmark,omitempty"`

	// Size is the buyer's chosen size when the product comes
	// in sizes, verified against the listing's own list
	// before the order was made. It rides to the seller in
	// her notice so the parcel that ships is the size asked
	// for.
	Size string `json:"size,omitempty"`

	// Items is the basket: every line the order carries when
	// the buyer bought more than one thing in it. Price is
	// then the items' total and ProductName their joined
	// label; the single-item order leaves Items empty and
	// reads exactly as it always did.
	Items []OrderItem `json:"items,omitempty"`

	// StockLines records what this order took from the
	// shelf when it was minted, line by line, in each
	// product's own units (pieces, or base units for a
	// by-measure product). An order that dies unpaid gives
	// exactly these units back. Restocked marks that the
	// giving-back has happened, so it can never happen twice.
	StockLines []StockLine `json:"stockLines,omitempty"`
	Restocked  bool        `json:"restocked,omitempty"`

	// SaleID is the identity of the session sale that
	// minted this order: a completion replayed from a stale
	// session restore finds its order by this id and resumes
	// it instead of minting a twin.
	SaleID string `json:"saleId,omitempty"`

	// SettleAlerted marks that the merchant has been told a
	// verified payment arrived for this order after it had
	// already ended (cancelled or failed): the money is real
	// but the order does not resurrect, and she is told once.
	SettleAlerted bool `json:"settleAlerted,omitempty"`

	// HeldAlerted marks that the merchant has been told a
	// paid event arrived for this order with no payment
	// session on record to verify it against, so the order
	// was held rather than settled on the event alone.
	// She is told once, however often the event repeats.
	HeldAlerted bool `json:"heldAlerted,omitempty"`

	// DryRun marks an order whose payment link was minted
	// by the disabled-checkout factory: a synthetic link,
	// no provider session, no real money. The webhook's
	// sessionless hold exempts exactly these orders, and
	// no others: an order minted against a configured
	// provider always carries its session, so a
	// sessionless one that is not marked dry-run is an
	// anomaly to hold, never a sale to settle on an
	// event's word.
	DryRun bool `json:"dryRun,omitempty"`

	// Quantity and Unit record a sale by measure: how much of the
	// product's measure the buyer took (3 of "yard"), against a
	// Price that is the settled price for that quantity, scaled
	// from the product's base quantity. Zero Quantity means the
	// whole item, as every order before this field existed.
	Quantity float64 `json:"quantity,omitempty"`
	Unit     string  `json:"unit,omitempty"`

	DeliveryPaidTo string `json:"deliveryPaidTo,omitempty"`

	// DisputeState is the order's scam-protection state: ""
	// (no dispute), "open" (a dispute is live and the order
	// is frozen on Stall's side), "refund_claimed" (the
	// seller says she refunded; the buyer is being asked to
	// confirm), "resolved_received" (the goods arrived),
	// "resolved_refund" (the buyer confirmed her refund), or
	// "struck" (unresolved past the limit; a strike stands
	// against the seller). DisputeBy is the number that
	// opened it and DisputeAt when, RFC3339.
	DisputeState string `json:"disputeState,omitempty"`
	DisputeBy    string `json:"disputeBy,omitempty"`
	DisputeAt    string `json:"disputeAt,omitempty"`

	PaymentPath   string `json:"paymentPath,omitempty"`
	SettledVia    string `json:"settledVia,omitempty"`
	ScreenshotRef string `json:"screenshotRef,omitempty"`

	// BookedList is the list figure the order's main line
	// was booked into the day book at (the sixth audit),
	// the single-order counterpart of OrderItem.ListPrice.
	BookedList float64 `json:"bookedList,omitempty"`
	// CreatedAt is when the order was placed, RFC3339. Orders
	// placed before this field existed carry none; the unpaid
	// watch stamps those on first sight rather than treating
	// them as ancient.
	CreatedAt string `json:"createdAt,omitempty"`
	// UnpaidAlerted marks that the unpaid watch has already
	// told both sides about this order sitting unpaid, so it
	// alerts once and never nags.
	UnpaidAlerted bool `json:"unpaidAlerted,omitempty"`
	// LinkAttempts counts the payment links minted for this
	// order: the first at the sale, then a fresh one each time
	// the buyer reports the link dead and the agent re-issues.
	// Zero on orders placed before the field existed means one.
	LinkAttempts int `json:"linkAttempts,omitempty"`

	// DeliveryState follows the goods after payment, the loop
	// the sale opens: "" (paid, not yet sent), "sent",
	// "received" (the buyer confirmed it reached her), and
	// "missing" (the buyer says it has not arrived, passed to
	// the seller). SentAt and ReceivedAt stamp the two moves
	// the seller and the buyer each report.
	DeliveryState string `json:"deliveryState,omitempty"`
	SentAt        string `json:"sentAt,omitempty"`
	ReceivedAt    string `json:"receivedAt,omitempty"`
}

// All returns every order in the book, in reference order, for
// the sweeps that walk the whole book (the unpaid watch).
func (b *Book) All() []Order {
	b.mu.Lock()
	defer b.mu.Unlock()
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

// Book is an order book stored in one JSON file. The mutex
// is not decoration: this book is written from agent turns
// (under the session lock), from the payment webhook and
// from the unpaid watch (both outside it), and a load,
// modify, save cycle from two of those at once would
// silently drop one side's orders. Every public method
// holds the lock across its whole cycle.
type Book struct {
	file string
	mu   sync.Mutex
	// poisoned is set when a load met bytes that did not
	// parse in the book or its last-known-good copy: while
	// it stands, saves refuse, because a save from that
	// load would cement the loss of every order the shop
	// ever took (the fourth audit's one medium).
	poisoned bool
}

// New returns an order book backed by the given file.
func New(file string) *Book {
	return &Book{file: file}
}

// Default is the shared order book, backed by data/orders.json.
var Default = New(datadir.Path("orders.json"))

// loadAll reads the book. The caller holds the lock. A file
// that exists but does not parse is preserved byte for byte
// under a single .corrupt name and shouted about, never
// silently read as an empty book: read as empty, the next
// save would wipe every order the shop ever took. The
// fourth audit found that preservation alone was not
// recovery — the aside holds the very bytes that failed,
// nothing ever read it back, and every load re-read the
// same corrupt file as empty until the first new order's
// save cemented the loss. So the book keeps the agent
// books' discipline: every good save leaves a last-known-
// good copy beside the book, a failed load restores from
// that copy, and only when both fail does the book read
// empty — poisoned, with saves refused until a load parses
// again or an operator repairs the file. The fifth audit
// added the finishing step: a successful restore REPAIRS
// the main file with the good bytes, so later loads are
// clean instead of re-entering the restore path forever,
// and the aside is one fixed file, overwritten by each new
// corruption rather than a fresh timestamped copy per load
// accumulating without bound.
func (b *Book) loadAll() map[string]Order {
	// restore tries the last known good copy: on success
	// the main file is repaired from it and the restored
	// book returned. Both failure roads below — bytes
	// that do not parse, and a file that exists but
	// cannot be read at all — go through it (the sixth
	// audit: the discipline keyed only on parse errors,
	// so a read error on an existing book read as a
	// fresh empty one and the next routine save cemented
	// the loss over the main file and its backup).
	restore := func() (map[string]Order, bool) {
		bak, berr := os.ReadFile(b.file + ".bak")
		if berr != nil {
			return nil, false
		}
		restored := map[string]Order{}
		if uerr := json.Unmarshal(bak, &restored); uerr != nil {
			return nil, false
		}
		tmp := b.file + ".tmp"
		if werr := os.WriteFile(tmp, bak, 0o600); werr == nil {
			_ = os.Rename(tmp, b.file)
		}
		b.poisoned = false
		return restored, true
	}
	data, err := os.ReadFile(b.file)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Order{}
		}
		if restored, ok := restore(); ok {
			fmt.Fprintf(os.Stderr, "orders: %s could not be read (%v); restored from the last known good copy\n", b.file, err)
			return restored
		}
		fmt.Fprintf(os.Stderr, "orders: %s could not be read (%v) and no good copy could be restored; read as empty; saves are refused until the book loads again\n", b.file, err)
		b.poisoned = true
		return map[string]Order{}
	}
	all := map[string]Order{}
	if err := json.Unmarshal(data, &all); err != nil {
		bad := b.file + ".corrupt"
		_ = os.WriteFile(bad, data, 0o600)
		if restored, ok := restore(); ok {
			fmt.Fprintf(os.Stderr, "orders: %s does not parse (%v); preserved as %s and restored from the last known good copy\n", b.file, err, bad)
			return restored
		}
		fmt.Fprintf(os.Stderr, "orders: %s does not parse (%v); preserved as %s and read as empty; saves are refused until the book parses again\n", b.file, err, bad)
		b.poisoned = true
		return map[string]Order{}
	}
	b.poisoned = false
	return all
}

// saveAll writes the book atomically: a temporary file in
// the same directory, renamed over the book, so a crash
// mid-write leaves either the old book or the new one,
// never a torn half. The same bytes then refresh the
// last-known-good copy the loader falls back to. The
// caller holds the lock. The mode is owner-only: the book
// holds phone numbers and addresses.
func (b *Book) saveAll(all map[string]Order) {
	if b.poisoned {
		fmt.Fprintf(os.Stderr, "orders: refusing to save over %s: the last load did not parse and no good copy could be restored\n", b.file)
		return
	}
	_ = os.MkdirAll(datadir.Dir(), 0o700)
	if data, err := marshalJSON(all); err == nil {
		tmp := b.file + ".tmp"
		if err := os.WriteFile(tmp, data, 0o600); err == nil {
			if err := os.Rename(tmp, b.file); err == nil {
				bakTmp := b.file + ".bak.tmp"
				if err := os.WriteFile(bakTmp, data, 0o600); err == nil {
					_ = os.Rename(bakTmp, b.file+".bak")
				}
			}
		}
	}
}

// RecordOrder records an order under our reference. The stored order
// starts as pending unless the given order already carries a status.
// It returns the stored order.
func (b *Book) RecordOrder(reference string, order Order) Order {
	b.mu.Lock()
	defer b.mu.Unlock()
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
	b.mu.Lock()
	defer b.mu.Unlock()
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
	b.mu.Lock()
	defer b.mu.Unlock()
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
	b.mu.Lock()
	defer b.mu.Unlock()
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
// unknown. Payment state (the settled path, the settlement
// itself) moves through here, one small step at a time.
func (b *Book) Update(reference string, mutate func(*Order)) *Order {
	b.mu.Lock()
	defer b.mu.Unlock()
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
	// The serial is the figure segment after the shop code:
	// the second segment, whether the reference ends there
	// (EFUA-0007) or carries its unguessable tail after it
	// (EFUA-0007-K7Q). The legacy HACK- references carry
	// their millisecond stamp in the same place.
	parts := strings.Split(reference, "-")
	if len(parts) < 2 {
		return 0
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// CountFor counts a shop's orders in the book, every status. A
// new order's serial is this count plus one (bumped past any key
// already taken), so a shop's references run 0001, 0002, ... in
// the order her sales complete.
// AnonymiseBuyer tombstones every personal detail of one
// buyer across the book, for a "delete my data" request:
// her number, her delivery details and her words are struck
// from each of her orders, while the order itself survives
// as the shop's anonymised sales record (goods, amounts and
// status only — the shop's own books are not hers to erase,
// and nothing on the tombstone can identify or reach her).
// Returns how many orders were tombstoned.
func (b *Book) AnonymiseBuyer(from string) int {
	n := 0
	for _, o := range b.All() {
		if o.From != from {
			continue
		}
		b.Update(o.Reference, func(oo *Order) {
			oo.From = "deleted"
			oo.Location = ""
			oo.Collector = ""
			oo.PackageName = ""
			oo.Note = ""
			oo.DeliveryPhone = ""
			oo.Landmark = ""
			// The payment session key and the sale
			// identity are struck with the rest — except
			// on an order whose money may still be in
			// flight. They name a transaction, not her,
			// and on a pending order they are the only
			// road that money has home: the unpaid
			// sweep's read-back, the resume road and
			// the journal all key on them, and the
			// fourth audit found an already-paid order
			// stranded pending forever by their
			// striking, its money sitting at the shop's
			// subaccount with no road back to the book.
			if oo.Status != "pending" && oo.Status != "link_failed" {
				oo.ProviderRef = nil
				oo.SaleID = ""
			}
			oo.DisputeBy = ""
		})
		n++
	}
	return n
}

// ClaimRestock atomically claims the right to give one
// order's shelf units back, reporting the lines to give.
// The claim — the Restocked mark — is decided and set
// inside the book's own lock, from the stored record:
// the second audit found every ending road deciding from
// a snapshot copy taken before its update, so two roads
// ending the same order at once (a merchant's cancel
// against the sweep's failed leg, say) each saw the mark
// unset and each gave the units back. Whoever claims
// first gives; every later road gets nothing.
func (b *Book) ClaimRestock(reference string) ([]StockLine, bool) {
	var lines []StockLine
	claimed := false
	b.Update(reference, func(o *Order) {
		if o.Restocked || len(o.StockLines) == 0 {
			return
		}
		o.Restocked = true
		lines = o.StockLines
		claimed = true
	})
	return lines, claimed
}

func (b *Book) CountFor(merchantID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
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
	b.mu.Lock()
	defer b.mu.Unlock()
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
// one shop, or nil. Payment reports and payment questions attach
// here: to her own newest open order, in the shop she is bound
// to, and never to another shop's order or another buyer's.
func (b *Book) NewestUnpaidFor(merchantID, buyer string) *Order {
	b.mu.Lock()
	defer b.mu.Unlock()
	var best *Order
	for _, o := range b.loadAll() {
		if o.MerchantID != merchantID || o.From != buyer || !o.Unpaid() {
			continue
		}
		oo := o
		if best == nil || refStamp(oo.Reference) > refStamp(best.Reference) {
			best = &oo
		}
	}
	return best
}
