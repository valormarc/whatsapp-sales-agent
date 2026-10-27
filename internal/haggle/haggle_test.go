package haggle

import (
	"testing"
)

func f64(v float64) *float64 { return &v }

func TestCounterNeverBelowFloorAndLandsOnIt(t *testing.T) {
	ask := 850.0
	floor := 680.0
	for round := 0; round < 300 && ask > floor; round++ {
		ask = NextAsk(ask, floor, round, 1)
		if ask < floor {
			t.Fatalf("round %d: ask %v below floor %v", round, ask, floor)
		}
	}
	if ask != floor {
		t.Fatalf("ask %v, want convergence exactly onto floor %v", ask, floor)
	}
}

func TestConcessionsShrink(t *testing.T) {
	floor := 680.0
	ask := 850.0
	drops := []float64{}
	for round := 0; round < 4; round++ {
		next := NextAsk(ask, floor, round, 1)
		drops = append(drops, ask-next)
		ask = next
	}
	for i := 1; i < len(drops); i++ {
		if drops[i] >= drops[i-1] {
			t.Fatalf("drop %v not smaller than %v", drops[i], drops[i-1])
		}
	}
}

func TestOfferAtOrAboveAskAcceptedAtAsk(t *testing.T) {
	if got := Decide(DecideParams{Offer: 850, Ask: 850, Floor: 680, Round: 0, StepScale: 1}); got != (Result{Type: "accept", Price: 850}) {
		t.Fatalf("got %+v", got)
	}
	if got := Decide(DecideParams{Offer: 900, Ask: 850, Floor: 680, Round: 0, StepScale: 1}); got != (Result{Type: "accept", Price: 850}) {
		t.Fatalf("got %+v", got)
	}
}

func TestLowOfferCounteredNeverAcceptedBelowFloor(t *testing.T) {
	got := Decide(DecideParams{Offer: 500, Ask: 850, Floor: 680, Round: 0, StepScale: 1})
	if got.Type != "counter" {
		t.Fatalf("type %q, want counter", got.Type)
	}
	if !(got.Price > 500 && got.Price < 850) {
		t.Fatalf("price %v not between offer and ask", got.Price)
	}
}

func TestLateCloseOfferAboveFloorAccepted(t *testing.T) {
	got := Decide(DecideParams{Offer: 725, Ask: 733, Floor: 680, Round: 3, StepScale: 1})
	if got != (Result{Type: "accept", Price: 725}) {
		t.Fatalf("got %+v", got)
	}
}

func TestAtFloorBelowFloorOfferGetsLastPriceCounter(t *testing.T) {
	got := Decide(DecideParams{Offer: 600, Ask: 681, Floor: 680, Round: 5, StepScale: 1})
	if got.Type != "counter" || got.Price != 680 || !got.LastPrice {
		t.Fatalf("got %+v, want counter at 680, last price", got)
	}
}

func TestMarginGivenAway(t *testing.T) {
	if MarginGivenAway(850, 725) != 125 {
		t.Fatal("want 125")
	}
	if MarginGivenAway(850, 900) != 0 {
		t.Fatal("want 0, never negative")
	}
}

func TestHighFirstOfferBeatingCounterAcceptedAtOffer(t *testing.T) {
	// The old engine countered 774 at an 800 offer, talking itself down.
	got := Decide(DecideParams{Offer: 800, Ask: 850, Floor: 680, Round: 0, StepScale: 1})
	if got != (Result{Type: "accept", Price: 800}) {
		t.Fatalf("got %+v", got)
	}
}

func TestFirmerPostureConcedesLess(t *testing.T) {
	normal := NextAsk(850, 680, 0, 1)
	firm := NextAsk(850, 680, 0, 0.5)
	if !(firm > normal) {
		t.Fatalf("firm counter %v should hold above normal %v", firm, normal)
	}
}

func TestLiftedOperativeFloorRespectedAtMaximumFirmness(t *testing.T) {
	ask := 850.0
	floor := 765.0 // hard floor 680 lifted by an empty day-book pool
	for round := 0; round < 300 && ask > floor; round++ {
		ask = NextAsk(ask, floor, round, 0.45)
		if ask < floor {
			t.Fatalf("round %d: ask %v below operative floor %v", round, ask, floor)
		}
	}
	if ask != floor {
		t.Fatalf("ask %v, want %v", ask, floor)
	}
}

func TestGuardrailCounterBelowStopRefused(t *testing.T) {
	b := Bounds{Offer: 500, Ask: 850, Floor: 680}
	if CheckProposal(&Proposal{Action: "counter", Price: f64(679)}, b) != nil {
		t.Fatal("counter below stop must be refused")
	}
	if CheckProposal(&Proposal{Action: "final", Price: f64(600)}, b) != nil {
		t.Fatal("final below stop must be refused")
	}
}

func TestGuardrailCounterInsideBoundsHonoured(t *testing.T) {
	got := CheckProposal(&Proposal{Action: "counter", Price: f64(790)}, Bounds{Offer: 500, Ask: 850, Floor: 680})
	if got == nil || *got != (Result{Type: "counter", Price: 790, LastPrice: false}) {
		t.Fatalf("got %+v", got)
	}
}

func TestGuardrailCounterOnFloorIsFinalStand(t *testing.T) {
	b := Bounds{Offer: 500, Ask: 850, Floor: 680}
	got := CheckProposal(&Proposal{Action: "counter", Price: f64(680)}, b)
	if got == nil || *got != (Result{Type: "counter", Price: 680, LastPrice: true}) {
		t.Fatalf("got %+v", got)
	}
	got = CheckProposal(&Proposal{Action: "final", Price: f64(720)}, b)
	if got == nil || *got != (Result{Type: "counter", Price: 720, LastPrice: true}) {
		t.Fatalf("got %+v", got)
	}
}

func TestGuardrailNoMovementOrRiseRefused(t *testing.T) {
	b := Bounds{Offer: 500, Ask: 850, Floor: 680}
	if CheckProposal(&Proposal{Action: "counter", Price: f64(850)}, b) != nil {
		t.Fatal("counter at the ask must be refused")
	}
	if CheckProposal(&Proposal{Action: "counter", Price: f64(900)}, b) != nil {
		t.Fatal("counter above the ask must be refused")
	}
}

func TestGuardrailAcceptanceSettlesAtOfferOnlyAboveFloor(t *testing.T) {
	got := CheckProposal(&Proposal{Action: "accept", Price: f64(720)}, Bounds{Offer: 720, Ask: 800, Floor: 680})
	if got == nil || *got != (Result{Type: "accept", Price: 720}) {
		t.Fatalf("got %+v", got)
	}
	// An offer below the floor can never be accepted, whatever the model says.
	if CheckProposal(&Proposal{Action: "accept", Price: f64(650)}, Bounds{Offer: 650, Ask: 800, Floor: 680}) != nil {
		t.Fatal("below-floor acceptance must be refused")
	}
	// The model cannot invent a settlement price different from the offer.
	if CheckProposal(&Proposal{Action: "accept", Price: f64(700)}, Bounds{Offer: 720, Ask: 800, Floor: 680}) != nil {
		t.Fatal("invented settlement price must be refused")
	}
}

func TestGuardrailMalformedProposalsRefused(t *testing.T) {
	b := Bounds{Offer: 500, Ask: 850, Floor: 680}
	if CheckProposal(nil, b) != nil {
		t.Fatal("nil proposal must be refused")
	}
	if CheckProposal(&Proposal{Action: "counter"}, b) != nil {
		t.Fatal("missing price must be refused")
	}
	// A string price never becomes a Proposal price in the Go client:
	// ParseProposal leaves it nil, which is refused here (covered in
	// the model package tests).
	if CheckProposal(&Proposal{Action: "dance", Price: f64(790)}, b) != nil {
		t.Fatal("unknown action must be refused")
	}
}
