package agent

import (
	"strings"
	"testing"
)

// State ownership, scenario one: a figure stated while a
// photo listing has no name is pending, and a different
// product's photo arriving next neither inherits it nor
// fills the listing's missing name. The figure attaches
// only when the seller names that same listing herself.
func TestPendingFigureIsNotInheritedByTheNextPhoto(t *testing.T) {
	owner := "233550002701"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, owner, "Black metal double clothes rack with lower shelf GH₵270")
	c.replies(t, owner)
	c.say(t, owner, "250")
	c.replies(t, owner)
	s := sessions.get(sessionKey(MerchantID, owner))
	if s == nil || s.Onboarding == nil || s.Onboarding.PendingFigure != 250 || s.Onboarding.FloorPrice != 0 {
		t.Fatalf("the figure should be pending and unattached, flow = %+v", s)
	}

	// A different, complete product arrives while the first
	// listing is still unnamed.
	c.photo(t, owner, "Dinner Set\n16 pieces for the family\nGH₵180")
	c.replies(t, owner)
	s = sessions.get(sessionKey(MerchantID, owner))
	if s == nil || s.Onboarding == nil {
		t.Fatal("the first listing should still be open")
	}
	if s.Onboarding.Name != "" || s.Onboarding.ListPrice != 270 || s.Onboarding.FloorPrice != 0 ||
		s.Onboarding.PendingFigure != 250 || len(s.Onboarding.PhotoRefs) != 1 {
		t.Fatalf("the next photo must not touch the unnamed listing, flow = %+v", s.Onboarding)
	}
	if len(s.BurstBatch) != 1 || s.BurstBatch[0].Name != "Dinner Set" {
		t.Fatalf("the different product should be kept as its own batch item, batch = %+v", s.BurstBatch)
	}

	// Her own name for the first listing settles the figure
	// onto it, and onto nothing else.
	c.say(t, owner, "Double Clothes Rack")
	c.replies(t, owner)
	if s.Onboarding == nil || s.Onboarding.Name != "Double Clothes Rack" || s.Onboarding.FloorPrice != 250 {
		t.Fatalf("the pending figure should attach to the named listing, flow = %+v", s.Onboarding)
	}
	if len(s.BurstBatch) != 1 || s.BurstBatch[0].ListPrice != 180 {
		t.Fatalf("the batch item should be untouched by the figure, batch = %+v", s.BurstBatch)
	}
}

// State ownership, scenario two: a photo whose read carries
// the open listing's price but a different description and
// no name is not a match. It is parked as its own held
// item, and the open listing keeps only its own photo.
func TestSamePriceDifferentDescriptionPhotoIsParked(t *testing.T) {
	owner := "233550002702"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, owner, "Corner Rack\nSteel frame with 4 tiers\nGH₵270")
	c.replies(t, owner)
	s := sessions.get(sessionKey(MerchantID, owner))
	if s == nil || s.Onboarding == nil || s.Onboarding.Name != "Corner Rack" {
		t.Fatalf("the first photo should open its listing, session = %+v", s)
	}

	c.photo(t, owner, "Polished steel frame with four wide shelves 270gh")
	got := strings.Join(c.replies(t, owner), "\n")
	s = sessions.get(sessionKey(MerchantID, owner))
	if s == nil || s.Onboarding == nil || len(s.Onboarding.PhotoRefs) != 1 {
		t.Fatalf("the ambiguous photo must not join the open listing, session = %+v", s)
	}
	if len(s.BurstHeld) != 1 || s.BurstHeld[0].ListPrice != 270 {
		t.Fatalf("the ambiguous photo should be parked as a held item, held = %+v", s.BurstHeld)
	}
	if strings.Contains(got, "Photo added") {
		t.Fatalf("the reply must not claim the photo was added, got %q", got)
	}
}
