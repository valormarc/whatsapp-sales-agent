package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/model"
)

// The photo name doctrine, in the live cases that made it:
// a seller sends a photo so the agent will do the looking.
// The reading commits to the product's name — its own
// judgement, or one drawn from its description — and the
// walk asks only for the prices, which are the one thing a
// reading must never invent. A read name is shown as read
// and corrected in one line. The name question survives
// only behind the plain admission that the photo could not
// be read at all.

// stubReedDiffuserRead stands the photo read in with the
// reed diffuser's own shape: a committed name and an
// accurate description, as the live read returned.
func stubReedDiffuserRead(t *testing.T) {
	t.Helper()
	stubPhotoMedia(t)
	stubDraftListing(t, func(caption, openName string) *model.ListingDraft {
		return &model.ListingDraft{
			Name:        "Reed Diffuser",
			Description: "Nature Essence reed diffuser for home and office in assorted fruity scents",
			Unit:        "bottle",
			UnitCertain: true,
		}
	})
}

// (i) A read that commits to a name: no name question is
// asked, the recap shows the name as read with its
// correction invited, and the walk asks only for the one
// outstanding price.
func TestPhotoReadNamesTheProductAndAsksOnlyPrices(t *testing.T) {
	stubReedDiffuserRead(t)
	merchant := "233550006101"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, merchant, "65")
	replies := c.replies(t, merchant)
	if len(replies) != 1 {
		t.Fatalf("a fully read photo earns the one compact message, got %q", replies)
	}
	got := replies[0]
	if !strings.Contains(got, "I have it as Reed Diffuser, from the photo") {
		t.Fatalf("the recap must present the name as read, got %q", got)
	}
	if !strings.Contains(got, "GH₵65") || !strings.Contains(got, "last price") {
		t.Fatalf("the one outstanding question is the last price, got %q", got)
	}
	if !strings.Contains(got, "If I have the name wrong, tell me the right one.") {
		t.Fatalf("the correction must be invited in the recap itself, got %q", got)
	}
	if strings.Contains(got, "What is it called?") || strings.Contains(got, "cannot read this photo") {
		t.Fatalf("a successful read never asks the name, got %q", got)
	}

	c.say(t, merchant, "55")
	if got := c.joined(t, merchant); !strings.Contains(got, "Saved ✅ Reed Diffuser - GH₵65, last price GH₵55") {
		t.Fatalf("the last price answer saves it under the read name, got %q", got)
	}
	p := catalogue.FindByName("demo", "Reed Diffuser")
	if p == nil || p.ListPrice != 65 || p.FloorPrice != 55 {
		t.Fatalf("saved product = %+v, want the diffuser at 65 with last price 55", p)
	}
}

// The refinement's own case: the read returns an accurate
// description and no name at all. The name is drawn from
// the description's leading product phrase — a read that
// can describe the goods can name them — and presented as
// read, never asked.
func TestPhotoDescriptionYieldsTheNameWhenTheReadGivesNone(t *testing.T) {
	stubPhotoMedia(t)
	stubDraftListing(t, func(caption, openName string) *model.ListingDraft {
		return &model.ListingDraft{
			Description: "Nature Essence reed diffuser for home and office in assorted fruity scents",
			Unit:        "bottle",
			UnitCertain: true,
		}
	})
	merchant := "233550006102"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, merchant, "65")
	got := c.last(t, merchant)
	if !strings.Contains(got, "I have it as Nature Essence Reed Diffuser, from the photo") {
		t.Fatalf("the name should be drawn from the read's own description, got %q", got)
	}
	if strings.Contains(got, "What is it called?") || strings.Contains(got, "cannot read this photo") {
		t.Fatalf("a good description with no separate name must never lead to the name question, got %q", got)
	}

	c.say(t, merchant, "55")
	p := catalogue.FindByName("demo", "Nature Essence Reed Diffuser")
	if p == nil || p.ListPrice != 65 || p.FloorPrice != 55 {
		t.Fatalf("saved product = %+v, want the diffuser under its drawn name", p)
	}
}

// (ii) A burst at scale: every kept photo arrives with its
// read name, the batch presents in one message with names
// and prices settled, and only the last prices are asked —
// no item's name question stalls the line.
func TestBurstPhotosArriveNamedAndArePricedTogether(t *testing.T) {
	stubPhotoMedia(t)
	stubDraftListing(t, func(caption, openName string) *model.ListingDraft {
		switch caption {
		case "65":
			return &model.ListingDraft{
				Name: "Reed Diffuser", Description: "Nature Essence reed diffuser for home and office in assorted fruity scents",
				Unit: "bottle", UnitCertain: true,
			}
		case "90":
			return &model.ListingDraft{
				Name: "Table Lamps", Description: "Decorative table lamps with gold bases",
				Unit: "piece", UnitCertain: true, SameAsOpen: "different",
			}
		case "55":
			return &model.ListingDraft{
				Name: "Preservation Bowl Set", Description: "White floral three piece preservation bowl set with lids",
				Unit: "set", UnitCertain: true, SameAsOpen: "different",
			}
		}
		return &model.ListingDraft{}
	})
	merchant := "233550006103"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, merchant, "65")
	c.photo(t, merchant, "90")
	c.photo(t, merchant, "55")
	_ = c.replies(t, merchant)

	c.say(t, merchant, "60")
	got := c.joined(t, merchant)
	if !strings.Contains(got, "Saved ✅ Reed Diffuser") {
		t.Fatalf("the open listing saves first, got %q", got)
	}
	if !strings.Contains(got, "*Table Lamps* - GH₵90") || !strings.Contains(got, "*Preservation Bowl Set* - GH₵55") {
		t.Fatalf("the batch must present in one message with its read names and prices, got %q", got)
	}
	if !strings.Contains(got, "What is the last price for each?") {
		t.Fatalf("only the last prices are outstanding, got %q", got)
	}
	if strings.Contains(got, "What is it called?") {
		t.Fatalf("no name question may stall the batch, got %q", got)
	}

	c.say(t, merchant, "80, 50")
	if p := catalogue.FindByName("demo", "Table Lamps"); p == nil || p.FloorPrice != 80 {
		t.Fatalf("the lamps should save from the batch answer, got %+v", p)
	}
	if p := catalogue.FindByName("demo", "Preservation Bowl Set"); p == nil || p.FloorPrice != 50 {
		t.Fatalf("the bowls should save from the batch answer, got %+v", p)
	}
}

// (iii) The one exception: a photo no read could make out
// at all — no name, no description, from the picture or
// from her words. The name is asked in one message, and
// the admission stands in it first.
func TestUnreadablePhotoAdmitsItAndAsksTheName(t *testing.T) {
	merchant := "233550006104"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	// No media stub: in this harness the photo cannot be
	// fetched, and the caption carries nothing either, so
	// the read yields neither a name nor a description.
	c.photo(t, merchant, "")
	replies := c.replies(t, merchant)
	if len(replies) != 2 || replies[0] != "Got your photo." {
		t.Fatalf("photo replies = %q", replies)
	}
	if replies[1] != "I cannot read this photo properly. What is the name of the product?" {
		t.Fatalf("the admission and the question stand together in one message, got %q", replies[1])
	}
	if strings.Contains(replies[1], "What is it called?") {
		t.Fatalf("a bare name question after a photo is the defect itself, got %q", replies[1])
	}

	c.say(t, merchant, "Rice Cooker")
	if got := c.last(t, merchant); !strings.Contains(got, "Tell me about Rice Cooker") {
		t.Fatalf("her answer names it and the walk goes on, got %q", got)
	}
}

// (iv) Her caption is judged before it can name anything:
// the availability word and the decoration come out, the
// price is hers, and what reaches the shelf is "Lamps".
func TestCaptionAvailabilityWordsNeverReachTheShelf(t *testing.T) {
	if d := throwDraftFromCaption("Lamps available 90gh🔥"); d.Name != "Lamps" || d.ListPrice != 90 {
		t.Fatalf("the caption should judge to the name Lamps at her price 90, got %+v", d)
	}

	stubPhotoMedia(t)
	stubDraftListing(t, func(caption, openName string) *model.ListingDraft {
		return &model.ListingDraft{
			Description: "Decorative table lamps with gold bases",
			Unit:        "piece",
			UnitCertain: true,
		}
	})
	merchant := "233550006105"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, merchant, "Lamps available 90gh🔥")
	got := c.last(t, merchant)
	if !strings.Contains(got, "Got it ✅ Lamps - GH₵90") {
		t.Fatalf("her judged caption name opens the listing, got %q", got)
	}
	if strings.Contains(got, "from the photo") || strings.Contains(got, "available 🔥") {
		t.Fatalf("the caption's name is hers, unmarked and undecorated, got %q", got)
	}
	c.say(t, merchant, "60")
	p := catalogue.FindByName("demo", "Lamps")
	if p == nil || p.ListPrice != 90 || p.FloorPrice != 60 {
		t.Fatalf("saved product = %+v, want Lamps at 90 with last price 60", p)
	}
}

// (v) A wrong read name costs one line at the recap: the
// explicit correction renames the listing before it saves,
// and the step she was answering is asked again under her
// name.
func TestReadNameCorrectedAtTheRecapSavesUnderHerName(t *testing.T) {
	stubReedDiffuserRead(t)
	merchant := "233550006106"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, merchant, "65")
	_ = c.replies(t, merchant)
	c.say(t, merchant, "call it Nature Essence Diffuser")
	replies := c.replies(t, merchant)
	if !strings.Contains(replies[0], "Corrected. Nature Essence Diffuser it is.") {
		t.Fatalf("the correction is answered plainly, got %q", replies)
	}
	if got := replies[len(replies)-1]; !strings.Contains(got, "Nature Essence Diffuser - GH₵65") || !strings.Contains(got, "last price") {
		t.Fatalf("the outstanding question is asked again under her name, got %q", got)
	}
	c.say(t, merchant, "55")
	p := catalogue.FindByName("demo", "Nature Essence Diffuser")
	if p == nil || p.ListPrice != 65 || p.FloorPrice != 55 {
		t.Fatalf("saved product = %+v, want the listing under her corrected name", p)
	}
	if p := catalogue.FindByName("demo", "Reed Diffuser"); p != nil {
		t.Fatalf("nothing may save under the read name she corrected, got %+v", p)
	}
}

// (v, bare shape) The correction needs no formula: at the
// recap, a reply that is simply a different name renames
// the listing the same way.
func TestReadNameCorrectedByABareNameAtTheRecap(t *testing.T) {
	stubReedDiffuserRead(t)
	merchant := "233550006107"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, merchant, "65")
	_ = c.replies(t, merchant)
	c.say(t, merchant, "Nature Essence Diffuser")
	if got := c.joined(t, merchant); !strings.Contains(got, "Corrected. Nature Essence Diffuser it is.") {
		t.Fatalf("a bare different name at the recap is her correction, got %q", got)
	}
	c.say(t, merchant, "55")
	if p := catalogue.FindByName("demo", "Nature Essence Diffuser"); p == nil {
		t.Fatal("the listing should save under the bare corrected name")
	}
	if p := catalogue.FindByName("demo", "Reed Diffuser"); p != nil {
		t.Fatalf("nothing may save under the read name she corrected, got %+v", p)
	}
}

// (vi) A name she gives herself stands over the reading's
// proposal: her caption names the goods, the read fills
// only the description, and nothing presents the name as
// read.
func TestHerCaptionNameStandsOverTheReadName(t *testing.T) {
	stubPhotoMedia(t)
	stubDraftListing(t, func(caption, openName string) *model.ListingDraft {
		return &model.ListingDraft{
			Name:        "Scented Oil Diffuser",
			Description: "Nature Essence reed diffuser for home and office in assorted fruity scents",
			Unit:        "bottle",
			UnitCertain: true,
		}
	})
	merchant := "233550006108"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, merchant, "Reed Diffuser 65")
	got := c.last(t, merchant)
	if !strings.Contains(got, "Got it ✅ Reed Diffuser - GH₵65") {
		t.Fatalf("her caption name opens the listing, got %q", got)
	}
	if strings.Contains(got, "I have it as") || strings.Contains(got, "Scented Oil Diffuser") {
		t.Fatalf("the read's proposal must not displace or mark her name, got %q", got)
	}
	c.say(t, merchant, "55")
	if p := catalogue.FindByName("demo", "Reed Diffuser"); p == nil || p.FloorPrice != 55 {
		t.Fatalf("saved product = %+v, want her name at her last price", p)
	}
	if p := catalogue.FindByName("demo", "Scented Oil Diffuser"); p != nil {
		t.Fatalf("the read's name must not reach the shelf, got %+v", p)
	}
}

// The batch form of the same correction: at the kept
// batch's presentation, one line renames a read-named item
// before it is priced or saved, and the batch is presented
// again under her name.
func TestBurstReadNameCorrectedAtThePresentation(t *testing.T) {
	stubPhotoMedia(t)
	stubDraftListing(t, func(caption, openName string) *model.ListingDraft {
		switch caption {
		case "65":
			return &model.ListingDraft{
				Name: "Reed Diffuser", Description: "Nature Essence reed diffuser for home and office in assorted fruity scents",
				Unit: "bottle", UnitCertain: true,
			}
		case "90":
			return &model.ListingDraft{
				Name: "Table Lamps", Description: "Decorative table lamps with gold bases",
				Unit: "piece", UnitCertain: true, SameAsOpen: "different",
			}
		}
		return &model.ListingDraft{}
	})
	merchant := "233550006109"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, merchant, "65")
	c.photo(t, merchant, "90")
	_ = c.replies(t, merchant)
	c.say(t, merchant, "60")
	_ = c.replies(t, merchant)
	s := sessions.get(sessionKey(MerchantID, merchant))
	if s == nil || len(s.BurstBatch) != 1 || !s.BurstAwaiting {
		t.Fatalf("the batch of one should stand presented, session = %+v", s)
	}

	c.say(t, merchant, "call it Gold Table Lamps")
	got := c.joined(t, merchant)
	if !strings.Contains(got, "Corrected. Item 1 is Gold Table Lamps now.") || !strings.Contains(got, "*Gold Table Lamps* - GH₵90") {
		t.Fatalf("the correction renames the item and presents the batch again, got %q", got)
	}
	c.say(t, merchant, "80")
	if p := catalogue.FindByName("demo", "Gold Table Lamps"); p == nil || p.FloorPrice != 80 {
		t.Fatalf("the item should save under her name, got %+v", p)
	}
	// FindByName matches loosely, so the read name's
	// absence is checked on the shelf's exact names.
	for _, p := range catalogue.ListProducts("demo") {
		if p.Name == "Table Lamps" {
			t.Fatalf("nothing may save under the read name she corrected, got %+v", p)
		}
	}
}
