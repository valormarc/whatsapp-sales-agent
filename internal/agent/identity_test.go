package agent

import (
	"testing"

	"github.com/valormarc/hackathon-agent/internal/model"
)

// stubDraftListing stands the model in for the photo read:
// the fake answers from the caption and the open listing's
// name, as the live read would from the picture.
func stubDraftListing(t *testing.T, fn func(caption, openName string) *model.ListingDraft) {
	t.Helper()
	old := draftListingFn
	draftListingFn = func(dataURI, caption, openName, openDesc string) *model.ListingDraft {
		return fn(caption, openName)
	}
	t.Cleanup(func() { draftListingFn = old })
}

// The identity judgement: a photo the model reads as a
// different design must not collect onto the open listing
// merely because its name contains the listing's name.
// "Strappy Flat Sandals" contains "Flat Sandals" word for
// word, and word overlap alone merged exactly such designs
// in live bursts; the model's verdict keeps them apart.
func TestIdentityVerdictDifferentKeepsDesignsApart(t *testing.T) {
	stubPhotoMedia(t)
	stubDraftListing(t, func(caption, openName string) *model.ListingDraft {
		if openName == "" {
			return &model.ListingDraft{}
		}
		return &model.ListingDraft{
			Name:        "Strappy Flat Sandals",
			Description: "Gold strappy flat sandals with shell medallion",
			SameAsOpen:  "different",
		}
	})
	owner := "233550002901"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, owner, "Flat Sandals\nBrown leather flat sandals\nGH₵80")
	c.replies(t, owner)
	c.photo(t, owner, "37-41@80")
	c.replies(t, owner)
	s := sessions[sessionKey(MerchantID, owner)]
	if s == nil || s.Onboarding == nil {
		t.Fatal("the open listing should still be open")
	}
	if len(s.Onboarding.PhotoRefs) != 1 {
		t.Fatalf("the different design must not join the open listing, refs = %v", s.Onboarding.PhotoRefs)
	}
	if len(s.BurstBatch) != 1 || s.BurstBatch[0].Name != "Strappy Flat Sandals" || s.BurstBatch[0].ListPrice != 80 {
		t.Fatalf("the different design should be kept as its own batch item, batch = %+v", s.BurstBatch)
	}
}

// The other direction: a photo the model reads as the same
// product collects even when its read name shares too few
// words with the listing's name for word overlap to match
// them — the verdict is the identification, not the words.
func TestIdentityVerdictSameCollectsDespiteNameShape(t *testing.T) {
	stubPhotoMedia(t)
	stubDraftListing(t, func(caption, openName string) *model.ListingDraft {
		if openName == "" {
			return &model.ListingDraft{}
		}
		return &model.ListingDraft{
			Name:        "Leather Sandals with Ankle Strap",
			Description: "Brown leather flat sandals, second angle",
			SameAsOpen:  "same",
		}
	})
	owner := "233550002902"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, owner, "Flat Sandals\nBrown leather flat sandals\nGH₵80")
	c.replies(t, owner)
	c.photo(t, owner, "37-41@80")
	c.replies(t, owner)
	s := sessions[sessionKey(MerchantID, owner)]
	if s == nil || s.Onboarding == nil {
		t.Fatal("the open listing should still be open")
	}
	if len(s.Onboarding.PhotoRefs) != 2 {
		t.Fatalf("the same product's second photo should join its listing, refs = %v", s.Onboarding.PhotoRefs)
	}
	if len(s.BurstBatch) != 0 || len(s.BurstHeld) != 0 {
		t.Fatalf("nothing should be batched or held, batch = %+v held = %+v", s.BurstBatch, s.BurstHeld)
	}
}
