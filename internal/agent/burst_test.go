package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
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
