// Package haggle is the haggling engine. Pure functions, no I/O,
// fully unit-tested.
//
// Model of a real trader: the ask starts at the list price and moves
// toward the floor in shrinking concessions. The floor it is given is
// never breached, by counter or by acceptance. The floor passed in is
// the operative floor for this haggle: the day book (package daybook)
// may lift it above the product's hard floor when the day's giveaway
// pool runs low, and stepScale shrinks the concessions to match.
// Neither ever loosens past the hard floor.
package haggle

import (
	"math"

	"github.com/valormarc/hackathon-agent/internal/money"
)

// steps holds the fraction of the remaining gap conceded per round.
var steps = []float64{0.45, 0.3, 0.2, 0.12, 0.06}

// Result is the outcome of a haggling decision: an acceptance at Price,
// or a counter at Price. LastPrice marks a counter that is the final
// stand.
type Result struct {
	Type      string // "accept" or "counter"
	Price     float64
	LastPrice bool
}

// NextAsk computes the next counter from the current ask, moving toward
// the floor by the round's concession step, scaled by stepScale. A
// stepScale of 1 is normal softness; lower is firmer. The result never
// crosses the floor and always moves at least one cedi.
func NextAsk(ask, floor float64, round int, stepScale float64) float64 {
	if ask <= floor {
		return floor
	}
	r := round
	if r > len(steps)-1 {
		r = len(steps) - 1
	}
	if r < 0 {
		r = 0
	}
	step := math.Min(0.95, steps[r]*stepScale)
	counter := money.Round(ask - (ask-floor)*step)
	if counter >= ask {
		counter = ask - 1 // always move, even when rounding stalls
	}
	return math.Max(floor, counter)
}

// DecideParams carries the inputs to Decide.
type DecideParams struct {
	Offer     float64
	Ask       float64
	Floor     float64
	Round     int
	StepScale float64 // 1 when unset in effect; callers pass the session value
}

// Decide decides what to do with a buyer's offer: accept at a price, or
// counter. An offer at or above the ask settles at the ask. An offer
// that already beats the counter the engine was about to make settles
// at the offer, so the engine never talks itself down. Late in the
// haggle, a close offer above the floor is also taken.
func Decide(p DecideParams) Result {
	if p.Offer >= p.Ask {
		return Result{Type: "accept", Price: p.Ask}
	}
	counter := NextAsk(p.Ask, p.Floor, p.Round, p.StepScale)
	// The buyer's offer already beats the counter we were about to
	// make: take the money instead of talking ourselves down.
	if p.Offer >= p.Floor && p.Offer >= counter {
		return Result{Type: "accept", Price: p.Offer}
	}
	// Late in the haggle, an offer close to the ask that clears the
	// floor is worth taking: a real trader closes rather than lose
	// the sale over crumbs.
	if p.Offer >= p.Floor && p.Round >= 2 && p.Offer >= p.Ask-(p.Ask-p.Floor)*0.18 {
		return Result{Type: "accept", Price: p.Offer}
	}
	if counter <= p.Floor {
		return Result{Type: "counter", Price: p.Floor, LastPrice: true}
	}
	return Result{Type: "counter", Price: counter}
}

// MarginGivenAway reports how much margin the merchant gave away on an
// agreed deal, for reporting. Never negative.
func MarginGivenAway(listPrice, agreedPrice float64) float64 {
	return math.Max(0, listPrice-agreedPrice)
}

// Proposal is a negotiation step proposed by the model: an action
// ("accept", "counter" or "final"), the proposed price, and optionally
// the message the model wants sent. Price is nil when the proposal
// carried no numeric price.
type Proposal struct {
	Action  string
	Price   *float64
	Message string
}

// Bounds are the bounds a proposal is checked against: the customer's
// offer, the current ask and the operative floor.
type Bounds struct {
	Offer float64
	Ask   float64
	Floor float64
}

// CheckProposal is the guardrail on model proposals. The model
// proposes; the engine disposes. A proposal is usable only when its
// price is a real number inside the bounds this conversation already
// lives by:
//
//   - accept: settles at the buyer's offer, and only if that offer
//     clears the floor. The model cannot invent a settlement price.
//   - counter or final: a genuine move down from the current ask that
//     never crosses the floor. A counter landing on the floor is a
//     final stand.
//
// Anything else (below the floor, no movement, a price above the ask,
// a missing or non-numeric price, an unknown action) returns nil and
// the caller falls back to Decide. The floor is never breached by a
// model proposal, exactly as it is never breached by the engine.
func CheckProposal(proposal *Proposal, b Bounds) *Result {
	if proposal == nil || proposal.Price == nil {
		return nil
	}
	price := *proposal.Price
	if math.IsNaN(price) || math.IsInf(price, 0) {
		return nil
	}
	p := money.Round(price)
	switch proposal.Action {
	case "accept":
		// The settlement is the buyer's offer, bounded by
		// the current ask exactly as Decide bounds it: an
		// offer at or above the ask settles AT the ask,
		// never above it. The third audit found this
		// branch settling at a stale offer above the ask
		// the conversation had already come down to. The
		// proposal's own price must equal the settlement:
		// the model cannot invent a settlement price.
		settle := b.Offer
		if settle >= b.Ask {
			settle = b.Ask
		}
		if settle >= b.Floor && p == money.Round(settle) {
			r := Result{Type: "accept", Price: money.Round(settle)}
			return &r
		}
		return nil
	case "counter", "final":
		if p < b.Floor || p >= b.Ask {
			return nil
		}
		r := Result{Type: "counter", Price: p, LastPrice: proposal.Action == "final" || p <= b.Floor}
		return &r
	}
	return nil
}
