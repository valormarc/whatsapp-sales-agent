package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/model"
)

// finishHeld drives one held burst item through its intake
// gate: the correction settles the doubted price, the
// figure is its last price, and "whole" answers the
// measure question if it is asked.
func finishHeld(t *testing.T, c *chat, owner string) {
	t.Helper()
	c.say(t, owner, "price is 170")
	c.replies(t, owner)
	c.say(t, owner, "160")
	c.replies(t, owner)
	if catalogue.FindByName(MerchantID, "Serving Trays") == nil {
		c.say(t, owner, "whole")
		c.replies(t, owner)
	}
}

// A forwarded burst of different, priced products is kept
// whole: the first opens its listing, the rest are batched
// with their details, the batch is presented when the open
// listing saves, and one reply prices them. An item whose
// price the harvest doubted is not priced from the batch:
// it opens afterwards at the intake gate.
func TestBurstBatchIsPricedTogetherAndSaved(t *testing.T) {
	stubPhotoMedia(t)
	owner := "233550002601"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, owner, "Corner Rack\nSteel frame with 4 tiers\nGH₵270")
	c.photo(t, owner, "Dinner Set\n16 pieces for the family\nGH₵180")
	joined := c.joined(t, owner)
	if !strings.Contains(joined, "kept") || !strings.Contains(joined, "Dinner Set") {
		t.Fatalf("the second product should be kept for the batch, got %q", joined)
	}
	c.photo(t, owner, "Serving Trays\nGold trim, set of 3\nGH₵170")
	s := sessions[sessionKey(MerchantID, owner)]
	if s == nil || len(s.BurstBatch) != 2 {
		t.Fatalf("two products should be waiting in the batch, got %+v", s)
	}

	// Her last price and measure answers save the open
	// listing, and the batch is presented in the same turn,
	// holding only the sound item: the trays' price was
	// read from among other figures and is doubted.
	c.say(t, owner, "250")
	c.replies(t, owner)
	c.say(t, owner, "whole")
	joined = strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(joined, "last price for each") || !strings.Contains(joined, "Dinner Set") {
		t.Fatalf("saving the open listing should present the batch, got %q", joined)
	}
	if strings.Contains(joined, "Serving Trays —") {
		t.Fatalf("the doubted item should not be priced from the batch, got %q", joined)
	}

	// One reply prices the batch; the held item then opens
	// at the gate.
	c.say(t, owner, "170")
	joined = strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(joined, "Added ✅") || !strings.Contains(joined, "Is it right?") {
		t.Fatalf("the batch should save and the held item should open at the gate, got %q", joined)
	}
	finishHeld(t, c, owner)

	byPrice := map[float64]catalogue.Product{}
	for _, p := range catalogue.ListProducts(MerchantID) {
		byPrice[p.ListPrice] = p
	}
	rack := byPrice[270]
	if rack.Name == "" || rack.FloorPrice != 250 || len(rack.Photos) != 1 {
		t.Fatalf("the open listing should be saved at 270/250 with its own photo, got %+v", rack)
	}
	dinner := byPrice[180]
	if dinner.Name == "" || dinner.FloorPrice != 170 || len(dinner.Photos) != 1 {
		t.Fatalf("the batch item should be saved at 180/170 with its own photo, got %+v", dinner)
	}
	trays := byPrice[170]
	if trays.Name == "" || trays.FloorPrice != 160 || len(trays.Photos) != 1 {
		t.Fatalf("the held item should be saved at its corrected 170/160 with its own photo, got %+v", trays)
	}
	if s.BurstAwaiting || len(s.BurstBatch) != 0 || len(s.BurstHeld) != 0 {
		t.Fatalf("the burst should be finished, got awaiting=%v batch=%v held=%v", s.BurstAwaiting, s.BurstBatch, s.BurstHeld)
	}
}

// The fixed-price phrase is read with its negation: a
// seller saying she does NOT want fixed prices has not
// set anything fixed, however exactly her sentence
// contains the trigger words. A clean clause still sets
// them, and "no bargaining" beside it is not a negation
// of the fixed price itself.
func TestBurstFixedPhraseReadsNegation(t *testing.T) {
	cases := map[string]bool{
		"i don't want a fixed price; customers can bargain": false,
		"i don't want a fixed price":                        false,
		"not fixed price, they can bargain":                 false,
		"it is not a fixed price shop":                      false,
		"fixed price":                                       true,
		"all fixed price":                                   true,
		"price is final":                                    true,
		"non-negotiable":                                    true,
		"no bargaining, fixed price":                        true,
	}
	for msg, want := range cases {
		if got := burstFixedPhrase(msg); got != want {
			t.Errorf("burstFixedPhrase(%q) = %v, want %v", msg, got, want)
		}
	}
}

// End to end: the negated sentence is not claimed as the
// batch's answer at all — nothing saves, the batch keeps
// waiting — and a plain "fixed price" afterwards still
// sets every item fixed, exactly as before.
func TestBurstNegatedFixedSentenceSavesNothing(t *testing.T) {
	owner := "233550002610"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, owner, "Corner Rack\nSteel frame with 4 tiers\nGH₵270")
	c.photo(t, owner, "Dinner Set\n16 pieces for the family\nGH₵180")
	c.photo(t, owner, "Serving Bowls\nShiny steel, polished finish\nGH₵170")
	c.say(t, owner, "250")
	c.replies(t, owner)
	c.say(t, owner, "whole")
	c.replies(t, owner)

	c.say(t, owner, "I don't want a fixed price; customers can bargain")
	c.replies(t, owner)
	s := sessions[sessionKey(MerchantID, owner)]
	if s == nil || !s.BurstAwaiting || len(s.BurstBatch) != 2 {
		t.Fatalf("the negated sentence should leave the batch waiting, got %+v", s)
	}
	for _, p := range catalogue.ListProducts(MerchantID) {
		if p.ListPrice == 180 || p.ListPrice == 170 {
			t.Fatalf("nothing from the batch should have saved, got %+v", p)
		}
	}

	c.say(t, owner, "fixed price")
	joined := strings.Join(c.replies(t, owner), "\n")
	if strings.Count(joined, "Added ✅") != 2 {
		t.Fatalf("a plain fixed price should save both items, got %q", joined)
	}
	for _, p := range catalogue.ListProducts(MerchantID) {
		if (p.ListPrice == 180 || p.ListPrice == 170) && !p.Fixed {
			t.Fatalf("both batch items should be fixed, got %+v", p)
		}
	}
}

// Two different products that read under one name at one
// price do not fold into each other: the model's identity
// verdict against the kept item is what decides, the way
// it decides against an open listing. A further photo the
// model judges the SAME product still folds, as an angle
// should.
func TestBurstFoldTakesTheIdentityVerdict(t *testing.T) {
	stubPhotoMedia(t)
	stubDraftListing(t, func(caption, openName string) *model.ListingDraft {
		switch {
		case openName == "":
			return &model.ListingDraft{}
		case openName == "Metal Rack" && strings.Contains(caption, "White single"):
			return &model.ListingDraft{SameAsOpen: "different"}
		case openName == "Metal Rack" && strings.Contains(caption, "from the side"):
			return &model.ListingDraft{SameAsOpen: "same"}
		default:
			return &model.ListingDraft{SameAsOpen: "different"}
		}
	})
	owner := "233550002611"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, owner, "Corner Rack\nSteel frame with 4 tiers\nGH₵270")
	c.photo(t, owner, "Metal Rack\nBlack double rack with bottom shelf\nGH₵270")
	c.photo(t, owner, "Metal Rack\nWhite single rack with wheels\nGH₵270")
	s := sessions[sessionKey(MerchantID, owner)]
	if s == nil || len(s.BurstBatch) != 2 {
		t.Fatalf("two different racks under one name should stay two batch items, got %+v", s)
	}
	if !strings.Contains(s.BurstBatch[0].Description, "Black double") ||
		!strings.Contains(s.BurstBatch[1].Description, "White single") {
		t.Fatalf("each design should keep its own details, got %+v", s.BurstBatch)
	}

	c.photo(t, owner, "Metal Rack\nBlack double rack from the side\nGH₵270")
	if len(s.BurstBatch) != 2 || len(s.BurstBatch[0].PhotoRefs) != 2 {
		t.Fatalf("an angle of the first design should fold into it, got %+v", s.BurstBatch)
	}

	// And both designs complete: the open listing saves,
	// the batch presents both racks, and one reply prices
	// them. The shelf keeps one product per name, so the
	// second design saves under its name qualified by its
	// own detail — neither is refused into a loop that
	// could never save it.
	c.say(t, owner, "250")
	c.replies(t, owner)
	c.say(t, owner, "whole")
	joined := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(joined, "kept 2 more items") {
		t.Fatalf("the batch should present both designs, got %q", joined)
	}
	c.say(t, owner, "240, 240")
	joined = strings.Join(c.replies(t, owner), "\n")
	if strings.Count(joined, "Added ✅") != 2 {
		t.Fatalf("both designs should save on the one reply, got %q", joined)
	}
	var racks []catalogue.Product
	for _, p := range catalogue.ListProducts(MerchantID) {
		if strings.HasPrefix(p.Name, "Metal Rack") {
			racks = append(racks, p)
		}
	}
	if len(racks) != 2 || racks[0].Name == racks[1].Name {
		t.Fatalf("the two designs should stand on the shelf under distinct names, got %+v", racks)
	}
	photos := map[string]int{}
	for _, r := range racks {
		photos[r.Name] = len(r.Photos)
		if r.ListPrice != 270 || r.FloorPrice != 240 {
			t.Fatalf("each design should carry its own prices, got %+v", r)
		}
	}
	if photos[racks[0].Name]+photos[racks[1].Name] != 3 {
		t.Fatalf("all three photos should survive, one design with two angles and one with one, got %v", photos)
	}
}

// One figure for the whole batch, said as such, prices
// every item at it.
func TestBurstBatchOneFigureForAll(t *testing.T) {
	owner := "233550002602"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, owner, "Corner Rack\nSteel frame with 4 tiers\nGH₵270")
	c.photo(t, owner, "Dinner Set\n16 pieces for the family\nGH₵180")
	c.photo(t, owner, "Serving Bowls\nShiny steel, polished finish\nGH₵170")
	c.say(t, owner, "250")
	c.replies(t, owner)
	c.say(t, owner, "whole")
	c.replies(t, owner)
	c.say(t, owner, "all 150")
	joined := strings.Join(c.replies(t, owner), "\n")
	if strings.Count(joined, "Added ✅") != 2 {
		t.Fatalf("both items should be added at the shared figure, got %q", joined)
	}
	for _, p := range catalogue.ListProducts(MerchantID) {
		if p.ListPrice == 180 && p.FloorPrice != 150 {
			t.Fatalf("the dinner set should carry the shared last price, got %+v", p)
		}
		if p.ListPrice == 170 && p.FloorPrice != 150 {
			t.Fatalf("the bowls should carry the shared last price, got %+v", p)
		}
	}
}

// A figure above an item's list price is refused for that
// item alone, and the batch keeps waiting until the figures
// are right.
func TestBurstBatchPartialAndOverListAnswers(t *testing.T) {
	owner := "233550002603"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, owner, "Corner Rack\nSteel frame with 4 tiers\nGH₵270")
	c.photo(t, owner, "Dinner Set\n16 pieces for the family\nGH₵180")
	c.photo(t, owner, "Serving Trays\nGold trim, set of 3\nGH₵170")
	c.say(t, owner, "250")
	c.replies(t, owner)
	c.say(t, owner, "whole")
	c.replies(t, owner)

	c.say(t, owner, "500")
	joined := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(joined, "above the list price") {
		t.Fatalf("an over-list figure should be refused plainly, got %q", joined)
	}
	if catalogue.FindByName(MerchantID, "Dinner Set") != nil {
		t.Fatal("the refused item must not be saved")
	}
	s := sessions[sessionKey(MerchantID, owner)]
	if s == nil || len(s.BurstBatch) != 1 || len(s.BurstHeld) != 1 || !s.BurstAwaiting {
		t.Fatalf("the sound item should still be waiting and the doubted one held, got batch=%v held=%v awaiting=%v", s.BurstBatch, s.BurstHeld, s.BurstAwaiting)
	}

	c.say(t, owner, "170")
	joined = strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(joined, "Added ✅") {
		t.Fatalf("the corrected figure should save the batch item, got %q", joined)
	}
	finishHeld(t, c, owner)
	if len(s.BurstBatch) != 0 || len(s.BurstHeld) != 0 || s.BurstAwaiting {
		t.Fatalf("the burst should be finished, got batch=%v held=%v awaiting=%v", s.BurstBatch, s.BurstHeld, s.BurstAwaiting)
	}
}

// Two products can share a name at different prices. A
// later photo folds into a kept batch item only when its
// price is compatible with that item's; a conflicting price
// keeps it as a separate item with its own price and photo,
// while a same-price angle of the first still folds in.
func TestBurstAlikeNamesWithDifferentPricesStaySeparate(t *testing.T) {
	stubPhotoMedia(t)
	owner := "233550002801"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, owner, "Corner Rack\nSteel frame with 4 tiers\nGH₵270")
	c.photo(t, owner, "Metal Rack\nWall mounted steel rack\nGH₵150")
	c.photo(t, owner, "Metal Rack\nHeavy duty floor rack\nGH₵200")
	s := sessions[sessionKey(MerchantID, owner)]
	if s == nil || len(s.BurstBatch) != 2 {
		t.Fatalf("the two differently priced racks should be separate batch items, got %+v", s)
	}
	if s.BurstBatch[0].ListPrice != 150 || s.BurstBatch[1].ListPrice != 200 {
		t.Fatalf("each item should keep its own price, got %+v", s.BurstBatch)
	}
	if len(s.BurstBatch[0].PhotoRefs) != 1 || len(s.BurstBatch[1].PhotoRefs) != 1 {
		t.Fatalf("each item should hold only its own photo, got %+v", s.BurstBatch)
	}

	c.photo(t, owner, "Metal Rack\nWall mounted steel rack\nGH₵150")
	s = sessions[sessionKey(MerchantID, owner)]
	if len(s.BurstBatch) != 2 || len(s.BurstBatch[0].PhotoRefs) != 2 {
		t.Fatalf("a same-price angle should fold into its own item only, got %+v", s.BurstBatch)
	}
}

// While a batch waits for its last prices, a sentence that
// merely contains a number is not its answer: "I have 2
// dinner sets in stock" must not price or save anything.
// The batch keeps waiting, and a clean figure still prices
// it afterwards.
func TestBurstNumberedSentenceIsNotAPriceAnswer(t *testing.T) {
	stubPhotoMedia(t)
	owner := "233550002802"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, owner, "Corner Rack\nSteel frame with 4 tiers\nGH₵270")
	c.photo(t, owner, "Dinner Set\n16 pieces for the family\nGH₵180")
	c.replies(t, owner)
	c.say(t, owner, "250")
	c.replies(t, owner)
	c.say(t, owner, "whole")
	joined := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(joined, "last price for each") {
		t.Fatalf("the batch should be presented, got %q", joined)
	}

	c.say(t, owner, "I have 2 dinner sets in stock")
	c.replies(t, owner)
	if catalogue.FindByName(MerchantID, "Dinner Set") != nil {
		t.Fatal("the numbered sentence must not save the batch item")
	}
	s := sessions[sessionKey(MerchantID, owner)]
	if s == nil || !s.BurstAwaiting || len(s.BurstBatch) != 1 {
		t.Fatalf("the batch should still be waiting, got %+v", s)
	}

	c.say(t, owner, "170")
	joined = strings.Join(c.replies(t, owner), "\n")
	p := catalogue.FindByName(MerchantID, "Dinner Set")
	if p == nil || p.ListPrice != 180 || p.FloorPrice != 170 {
		t.Fatalf("the clean answer should save the item at 180/170, got %+v", p)
	}
	if !strings.Contains(joined, "Added") {
		t.Fatalf("the save should be announced, got %q", joined)
	}
}
