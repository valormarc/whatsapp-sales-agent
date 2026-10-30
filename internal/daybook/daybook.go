// Package daybook keeps the day book: how a real trader thinks.
//
// A market seller does not judge each sale alone; she judges the whole
// day. Sell cheap in the morning and the loss sits in her head, so the
// next customers meet a firmer seller who quietly wins it back, a few
// cedis at a time. This package keeps that book per merchant, per day:
//
//   - The merchant sets a daily giveaway allowance: the total discount
//     the agent may hand out across all sales today.
//   - Every agreed sale draws its giveaway (list price minus agreed
//     price) from the allowance.
//   - As the allowance drains, haggling gets firmer: concessions shrink
//     and the agent stops higher. The product's hard floor never moves
//     and is never crossed; firm only ever means stopping further
//     above it.
package daybook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/money"
)

// State is one merchant's book for one day.
type State struct {
	Date      string  `json:"date"`
	Sales     int     `json:"sales"`
	Revenue   float64 `json:"revenue"`
	GivenAway float64 `json:"givenAway"`
	WalkAways int     `json:"walkAways"`
	Allowance float64 `json:"allowance"`
}

// Options carries the optional inputs to a day book read.
type Options struct {
	Allowance float64
	Date      string // defaults to today (UTC)
}

// Sale carries the inputs to RecordSale.
type Sale struct {
	ListPrice   float64
	AgreedPrice float64
	Allowance   float64
	Date        string // defaults to today (UTC)
}

// TodayString returns today's date in UTC, formatted YYYY-MM-DD. Accra
// is UTC+0, so the UTC date is the trading date.
func TodayString() string {
	return time.Now().UTC().Format("2006-01-02")
}

func freshDay(date string, allowance float64) State {
	return State{Date: date, Sales: 0, Revenue: 0, GivenAway: 0, WalkAways: 0, Allowance: allowance}
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

// Daybook is a day book stored in one JSON file. It is
// written from agent turns and from the payment webhook's
// settlement path, which do not hold the same locks, so
// every public method holds this mutex across its whole
// load, modify, save cycle.
type Daybook struct {
	file string
	mu   sync.Mutex
}

// New returns a day book backed by the given file.
func New(file string) *Daybook {
	return &Daybook{file: file}
}

// Default is the shared day book, backed by data/daybook.json.
var Default = New(datadir.Path("daybook.json"))

// loadAll reads the book; the caller holds the lock. A
// file that exists but does not parse is preserved under a
// .corrupt- name and shouted about, never silently read as
// empty: the next save would wipe the day's trading.
func (b *Daybook) loadAll() map[string]State {
	data, err := os.ReadFile(b.file)
	if err != nil {
		return map[string]State{}
	}
	all := map[string]State{}
	if err := json.Unmarshal(data, &all); err != nil {
		bad := b.file + ".corrupt-" + time.Now().Format("20060102-150405")
		_ = os.WriteFile(bad, data, 0o600)
		fmt.Fprintf(os.Stderr, "daybook: %s does not parse (%v); preserved as %s and read as empty\n", b.file, err, bad)
		return map[string]State{}
	}
	return all
}

// saveAll writes the book atomically (temporary file,
// renamed over), owner-only: the day's money story is the
// shop's own business. The caller holds the lock.
func (b *Daybook) saveAll(all map[string]State) {
	_ = os.MkdirAll(filepath.Dir(b.file), 0o700)
	_ = os.MkdirAll(datadir.Dir(), 0o700)
	if data, err := marshalJSON(all); err == nil {
		tmp := b.file + ".tmp"
		if err := os.WriteFile(tmp, data, 0o600); err == nil {
			_ = os.Rename(tmp, b.file)
		}
	}
}

// GetDayState returns the current day state for a merchant; a new date
// starts a fresh book. A non-zero allowance in opts updates the stored
// allowance for the day.
func (b *Daybook) GetDayState(merchantID string, opts Options) State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.getDayState(merchantID, opts)
}

// getDayState is GetDayState with the lock already held:
// the recording methods below compose it inside their own
// locked cycle, and a plain mutex cannot be taken twice.
func (b *Daybook) getDayState(merchantID string, opts Options) State {
	date := opts.Date
	if date == "" {
		date = TodayString()
	}
	all := b.loadAll()
	stored, ok := all[merchantID]
	if !ok || stored.Date != date {
		state := freshDay(date, opts.Allowance)
		all[merchantID] = state
		b.saveAll(all)
		return state
	}
	if opts.Allowance != 0 {
		stored.Allowance = opts.Allowance
	}
	return stored
}

// RecordSale books an agreed sale: one more sale, the revenue in, and
// the giveaway (list minus agreed, never negative) drawn from the
// allowance. It returns the updated day state.
func (b *Daybook) RecordSale(merchantID string, s Sale) State {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.getDayState(merchantID, Options{Allowance: s.Allowance, Date: s.Date})
	state.Sales++
	state.Revenue += s.AgreedPrice
	given := s.ListPrice - s.AgreedPrice
	if given < 0 {
		given = 0
	}
	state.GivenAway += given
	all := b.loadAll()
	all[merchantID] = state
	b.saveAll(all)
	return state
}

// RecordCancellation takes a booked sale back out of the
// book: one fewer sale, the revenue out, the giveaway it drew
// returned to the day's allowance. It applies only when the
// stored book is for the sale's own date: the book keeps one
// day at a time, and a past day is history that is not
// rewritten. Figures never drop below zero. It reports
// whether the reversal landed.
func (b *Daybook) RecordCancellation(merchantID string, s Sale) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	all := b.loadAll()
	stored, ok := all[merchantID]
	if !ok || stored.Date != s.Date {
		return false
	}
	stored.Sales--
	if stored.Sales < 0 {
		stored.Sales = 0
	}
	stored.Revenue -= s.AgreedPrice
	if stored.Revenue < 0 {
		stored.Revenue = 0
	}
	given := s.ListPrice - s.AgreedPrice
	if given < 0 {
		given = 0
	}
	stored.GivenAway -= given
	if stored.GivenAway < 0 {
		stored.GivenAway = 0
	}
	all[merchantID] = stored
	b.saveAll(all)
	return true
}

// RecordWalkAway books a customer who would not meet the final price
// and was let go. The merchant hears about these in her day report: a
// pattern of walk-aways on one product is pricing information, not
// failure.
func (b *Daybook) RecordWalkAway(merchantID string, opts Options) State {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.getDayState(merchantID, opts)
	state.WalkAways++
	all := b.loadAll()
	all[merchantID] = state
	b.saveAll(all)
	return state
}

// PoolRatio is the fraction of today's allowance still unspent. No
// allowance set means the day book is off and the agent haggles at
// normal softness (ratio 1).
func PoolRatio(state State) float64 {
	if state.Allowance == 0 {
		return 1
	}
	r := 1 - state.GivenAway/state.Allowance
	if r < 0 {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}

// Context is the haggling posture for the next customer, derived from
// the day book: the operative floor and the concession step scale.
type Context struct {
	Floor     float64
	StepScale float64
	Firmness  float64
}

// HaggleContext derives the posture for a product from the day state.
// Floor is the operative floor for this haggle (hard floor plus a
// firmness lift of up to half the list-to-floor gap). StepScale
// multiplies concession sizes (1 is normal, lower is firmer).
func HaggleContext(product catalogue.Product, state State) Context {
	ratio := PoolRatio(state)
	firmness := 1 - ratio // 0 = relaxed, 1 = pool empty, maximum firm
	stepScale := 1 - 0.55*firmness
	floor := money.Round(product.FloorPrice + (product.ListPrice-product.FloorPrice)*0.5*firmness)
	if floor < product.FloorPrice {
		floor = product.FloorPrice
	}
	return Context{Floor: floor, StepScale: stepScale, Firmness: firmness}
}
