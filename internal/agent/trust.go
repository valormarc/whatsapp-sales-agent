package agent

// The marketplace memory: what a WhatsApp number has done
// across every shop on Stall, kept so a stranger's record
// speaks before her promises do.
//
// Two kinds of fact live here, kept apart on purpose. What
// the order book already knows — sales paid, deliveries the
// buyer confirmed — is counted from the book itself, per
// owner number across all her shops, so it can never drift
// from the orders. What the book cannot know — the strikes
// Stall has judged against a number, and the flag three
// strikes raise — is recorded in the trust book, and only
// the dispute roads write it (see disputes.go).

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// trustRecord is one number's judged record: strikes, and
// the flag they raise.
type trustRecord struct {
	Number  string `json:"number"`
	Strikes int    `json:"strikes"`
	Flagged bool   `json:"flagged"`
}

var trustMu sync.Mutex

func loadTrust() map[string]*trustRecord {
	out := map[string]*trustRecord{}
	data, err := os.ReadFile(datadir.Path("trust.json"))
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}

func saveTrust(all map[string]*trustRecord) {
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(datadir.Path("trust.json"), data, 0o644)
}

// strikesOf is the number's standing strike count.
func strikesOf(number string) int {
	trustMu.Lock()
	defer trustMu.Unlock()
	if r := loadTrust()[number]; r != nil {
		return r.Strikes
	}
	return 0
}

// flaggedNumber reports whether the number carries three
// strikes: its sales are gated and its buyers are warned.
func flaggedNumber(number string) bool {
	trustMu.Lock()
	defer trustMu.Unlock()
	if r := loadTrust()[number]; r != nil {
		return r.Flagged
	}
	return false
}

// addStrike records one judged strike against a number.
// Three strikes flag it.
func addStrike(number string) int {
	trustMu.Lock()
	defer trustMu.Unlock()
	all := loadTrust()
	r := all[number]
	if r == nil {
		r = &trustRecord{Number: number}
		all[number] = r
	}
	r.Strikes++
	if r.Strikes >= 3 {
		r.Flagged = true
	}
	saveTrust(all)
	return r.Strikes
}

// shopsOfOwner lists the shop IDs one owner number runs.
func shopsOfOwner(owner string) map[string]bool {
	out := map[string]bool{}
	for _, m := range catalogue.ListMerchants() {
		if m.OwnerWhatsApp == owner {
			out[m.ID] = true
		}
	}
	return out
}

// sellerDelivered counts, from the order book, the paid
// orders across all of the owner's shops that the buyer
// confirmed received: the seller's proven deliveries.
func sellerDelivered(owner string) int {
	shops := shopsOfOwner(owner)
	n := 0
	for _, o := range orders.Default.All() {
		if shops[o.MerchantID] && o.Status == "paid" && o.DeliveryState == "received" {
			n++
		}
	}
	return n
}

// sellerIsNew is the new-seller line: fewer than three
// proven deliveries anywhere on the marketplace. A new
// seller's settlement cannot be held — Seev releases link
// money to her MoMo the next day by itself, and Stall has
// no escrow lever on that rail — so newness is met with the
// controls Stall does own: her buyers are told she is new
// before they pay, and every sale waits for her word that
// the item is in hand (see inhand.go).
func sellerIsNew(owner string) bool {
	return sellerDelivered(owner) < 3
}
