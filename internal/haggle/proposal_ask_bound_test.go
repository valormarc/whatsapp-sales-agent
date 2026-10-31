package haggle

import "testing"

// Run-3 record: CheckProposal's accept branch settled at
// the buyer's offer without bounding it by the current
// ask, so a verified model proposal could settle ABOVE
// the ask the conversation had already come down to —
// where Decide settles at the ask. The accept settlement
// is now the offer bounded by the ask, and the proposal's
// price must equal it.
func TestCheckProposalAcceptIsBoundedByTheAsk(t *testing.T) {
	above := 600.0
	if r := CheckProposal(&Proposal{Action: "accept", Price: &above}, Bounds{Offer: 600, Ask: 500, Floor: 400}); r != nil {
		t.Fatalf("an accept priced above the ask must not verify, got %+v", r)
	}
	atAsk := 500.0
	r := CheckProposal(&Proposal{Action: "accept", Price: &atAsk}, Bounds{Offer: 600, Ask: 500, Floor: 400})
	if r == nil || r.Price != 500 {
		t.Fatalf("an accept at the ask for an above-ask offer settles at the ask, got %+v", r)
	}
	offer := 450.0
	r = CheckProposal(&Proposal{Action: "accept", Price: &offer}, Bounds{Offer: 450, Ask: 500, Floor: 400})
	if r == nil || r.Price != 450 {
		t.Fatalf("an ordinary accept still settles at the offer, got %+v", r)
	}
}
