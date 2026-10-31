package agent

import (
	"strings"
	"sync"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// The completion journal: a small, synchronous record of
// every sale as it completes, written in the completion
// turn itself — before the first unit of stock moves, and
// before the session snapshot is scheduled.
//
// It exists for the two crash windows the session
// snapshot's asynchronous cadence cannot close:
//
//   - Identity: a sale's identity reaches the snapshot
//     only after the turn, so a crash in that gap restores
//     the buyer's session from just before her purchase
//     completed, SaleID missing, and her next confirmation
//     could mint a twin of a standing order. On restore, a
//     session whose identity is missing is rebound to the
//     identity its completion journalled, and the ordinary
//     replay guard (liveAgreementOrder) takes it from
//     there.
//
//   - Stock: the mint takes stock before it records the
//     order, so a crash between the two left units taken
//     for a sale no book anywhere recorded — the shelf
//     silently short, unrecoverable by any path that keys
//     off orders. The journal names the sale and its stock
//     lines before the take, records each line as its take
//     lands, and the next boot reconciles: an entry whose
//     order never reached the book gives back the units
//     that actually moved, once, under a reconciled mark
//     persisted before the first unit returns.
//
// A completion whose take is refused settles its own
// intent at once (its rollback already returned the
// units), so reconciliation never gives back what a
// refusal already returned.

type saleCompletion struct {
	Key        string             `json:"key"`
	SaleID     string             `json:"saleId"`
	Reference  string             `json:"reference"`
	ProductID  string             `json:"productId"`
	ShopID     string             `json:"shopId"`
	Lines      []orders.StockLine `json:"lines,omitempty"`
	// TakenLines records the lines the take actually
	// moved, appended as each take lands. The journal is
	// written before the take begins, so Lines alone is
	// only intent: the third audit found reconciliation
	// returning lines the take never reached after a crash
	// inside the take loop, inflating the shelf. TakeMarked
	// distinguishes entries written with take marks from
	// older entries that carry intent only; reconciliation
	// falls back to intent for those, their semantics when
	// written.
	TakenLines []orders.StockLine `json:"takenLines,omitempty"`
	TakeMarked bool               `json:"takeMarked,omitempty"`
	Reconciled bool               `json:"reconciled,omitempty"`
	At         string             `json:"at"`
}

var saleJournalMu sync.Mutex

const saleJournalKeep = 500

func loadSaleJournal() []saleCompletion {
	var all []saleCompletion
	loadBookJSON(datadir.Path("salecompletions.json"), &all)
	return all
}

// forgetSaleJournalFor drops one buyer's journal entries
// at erasure (the fifth audit): the journal keys each
// completion by her session key — shop and number — so
// her number stayed on file, tied to her anonymised
// order's reference and sale identity, after
// delete-my-data had confirmed everything was gone.
func forgetSaleJournalFor(buyer string) {
	saleJournalMu.Lock()
	defer saleJournalMu.Unlock()
	all := loadSaleJournal()
	kept := all[:0]
	for _, e := range all {
		if !strings.HasSuffix(e.Key, "|"+buyer) {
			kept = append(kept, e)
		}
	}
	if len(kept) != len(all) {
		saveBookJSON(datadir.Path("salecompletions.json"), kept)
	}
}

func recordSaleCompletion(key, saleID, reference, productID, shopID string, lines []orders.StockLine) {
	if saleID == "" {
		return
	}
	saleJournalMu.Lock()
	defer saleJournalMu.Unlock()
	all := loadSaleJournal()
	all = append(all, saleCompletion{
		Key: key, SaleID: saleID, Reference: reference,
		ProductID: productID, ShopID: shopID, Lines: lines,
		TakeMarked: true,
		At:         time.Now().UTC().Format(time.RFC3339),
	})
	if len(all) > saleJournalKeep {
		all = all[len(all)-saleJournalKeep:]
	}
	saveBookJSON(datadir.Path("salecompletions.json"), all)
}

// recordJournalTake appends one actually-taken line to
// its journal entry and persists it at once: after a
// crash inside the take loop, the journal says exactly
// which units moved, no more.
func recordJournalTake(reference string, ln orders.StockLine) {
	saleJournalMu.Lock()
	defer saleJournalMu.Unlock()
	all := loadSaleJournal()
	for i := range all {
		if all[i].Reference == reference && !all[i].Reconciled {
			all[i].TakenLines = append(all[i].TakenLines, ln)
			saveBookJSON(datadir.Path("salecompletions.json"), all)
			return
		}
	}
}

// settleSaleIntent marks one journal entry reconciled
// without giving anything back: the completion it named
// was refused and its own rollback settled the stock.
func settleSaleIntent(reference string) {
	saleJournalMu.Lock()
	defer saleJournalMu.Unlock()
	all := loadSaleJournal()
	for i := range all {
		if all[i].Reference == reference && !all[i].Reconciled {
			all[i].Reconciled = true
			saveBookJSON(datadir.Path("salecompletions.json"), all)
			return
		}
	}
}

// reconcileSaleJournal runs at boot, before restored
// sessions are revalidated: every entry whose order never
// reached the book gives its stock lines back and is
// marked reconciled; entries whose orders stand are
// marked reconciled as housekeeping. An entry is
// reconciled at most once, so a second boot gives
// nothing back twice.
func reconcileSaleJournal() {
	saleJournalMu.Lock()
	defer saleJournalMu.Unlock()
	all := loadSaleJournal()
	for i := range all {
		if all[i].Reconciled {
			continue
		}
		// The claim is persisted BEFORE the give-backs it
		// guards, entry by entry: the third audit found
		// the marks held in memory until every give had
		// run, so a crash during reconciliation returned
		// the same stock again at the next boot. A crash
		// between the persisted claim and a give now loses
		// that give instead — the shelf reads short, never
		// doubled — the same claim-first discipline as the
		// order book's restock claims.
		all[i].Reconciled = true
		saveBookJSON(datadir.Path("salecompletions.json"), all)
		if orders.Default.GetOrder(all[i].Reference) != nil {
			continue
		}
		lines := all[i].TakenLines
		if !all[i].TakeMarked {
			lines = all[i].Lines
		}
		for _, ln := range lines {
			catalogue.GiveStockMeasured(all[i].ShopID, ln.Name, ln.Units, ln.Unit, ln.BaseQty)
		}
	}
}

// rebindSaleIDFromJournal restores a session's sale
// identity from the completion journal when the snapshot
// lost it: the session key and the product it was buying
// must both match a journalled completion. A session that
// already carries an identity is left exactly as it is.
func rebindSaleIDFromJournal(key string, s *Session) {
	if s == nil || s.SaleID != "" || s.Product == nil {
		return
	}
	// A session carrying a payment URL is the
	// POST-completion agreed session completeSale itself
	// installed, not the pre-completion snapshot this
	// rebind exists for (the sixth audit): its SaleID was
	// never lost, it simply belongs to the finished sale,
	// and rebinding a journalled identity onto it lets a
	// later purchase inherit the old sale whole.
	if s.PaymentURL != "" {
		return
	}
	// The rebind exists for one shape: the pre-completion
	// snapshot of a sale that DID complete, restored
	// after its process died. A session that has not
	// reached agreement — haggling, browsing, settling
	// on the shelf — is a new purchase of the same
	// product, not that snapshot, and must never inherit
	// the earlier sale's identity (the fifth audit).
	if s.Stage != "agreed" && s.Stage != "fulfilment" {
		return
	}
	saleJournalMu.Lock()
	defer saleJournalMu.Unlock()
	all := loadSaleJournal()
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Key == key && all[i].ProductID == s.Product.ID {
			s.SaleID = all[i].SaleID
			return
		}
	}
}
