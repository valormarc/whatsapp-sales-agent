package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// The seller's photos are kept on the listing she makes from
// them, and a buyer who asks for the pic is sent them.

func stubPhotoMedia(t *testing.T) {
	t.Helper()
	oldDown, oldUp := downloadMediaBytesFn, uploadMediaFn
	downloadMediaBytesFn = func(mediaID string) ([]byte, string, bool) {
		return []byte("fake-jpeg-bytes-" + mediaID), "image/jpeg", true
	}
	uploadMediaFn = func(data []byte, mime string) (string, bool) {
		return "uploaded-ref", true
	}
	t.Cleanup(func() { downloadMediaBytesFn, uploadMediaFn = oldDown, oldUp })
}

func TestListingKeepsItsPhotosAndTheBuyerIsSentThem(t *testing.T) {
	stubPhotoMedia(t)
	owner := "233550003001"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	// The listing: her advert opens it, two photos of the
	// same slides join it (an album: the second is silent and
	// still collected), and the compact walk saves it.
	c.say(t, owner, "B Buckle Slides\n\nSizes 37 to 42")
	c.replies(t, owner)
	HandleInbound(Inbound{From: owner, ImageID: "slides-a"})
	c.replies(t, owner)
	HandleInbound(Inbound{From: owner, ImageID: "slides-b"})
	if got := c.replies(t, owner); len(got) != 0 {
		t.Fatalf("the second album photo should be silent, got %q", got)
	}
	c.say(t, owner, "75")
	c.replies(t, owner)
	c.say(t, owner, "70")
	saved := strings.Join(c.replies(t, owner), "\n")
	if strings.Contains(saved, "by measure") {
		c.say(t, owner, "whole")
		saved = strings.Join(c.replies(t, owner), "\n")
	}
	if !strings.Contains(saved, "Saved ✅ B Buckle Slides - GH₵75, last price GH₵70") {
		t.Fatalf("the compact save did not land: %q", saved)
	}

	shop := catalogue.GetMerchant("demo")
	if shop == nil {
		t.Fatal("the demo shop is missing")
	}
	var slides *catalogue.Product
	for _, p := range catalogue.ListProducts(shop.ID) {
		if strings.Contains(p.Name, "Buckle Slides") {
			pp := p
			slides = &pp
		}
	}
	if slides == nil {
		t.Fatal("the slides were never listed")
	}
	if len(slides.Photos) != 2 {
		t.Fatalf("the listing should keep both album photos, got %v", slides.Photos)
	}
	for _, rel := range slides.Photos {
		if _, err := os.Stat(filepath.Join(dir, "data", rel)); err != nil {
			t.Fatalf("the kept photo is not on disk: %v", err)
		}
	}

	// A buyer inside the shop asks for the pic and is sent
	// the photos, not the old cannot-send line.
	buyer := "233550003002"
	HandleInbound(Inbound{From: buyer, Text: "SHOP-" + shop.ShopCode})
	c.replies(t, buyer)
	HandleInbound(Inbound{From: buyer, Text: "Show me the B Buckle Slides"})
	c.replies(t, buyer)
	before := len(doorRecords(t, dir, buyer))
	HandleInbound(Inbound{From: buyer, Text: "Send pic"})
	got := strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "I cannot send a photo") {
		t.Fatalf("the buyer got the cannot-send line despite kept photos: %q", got)
	}
	images := 0
	for _, rec := range doorRecords(t, dir, buyer)[before:] {
		if rec.Kind == "image" {
			images++
		}
	}
	if images != 2 {
		t.Fatalf("the buyer should be sent both photos, got %d image sends", images)
	}
}

// A product listed in words alone has no photos, and the
// buyer hears the plain words answer, exactly as before.
func TestAProductWithoutPhotosAsksTheOwner(t *testing.T) {
	stubPhotoMedia(t)
	_, c := setupTwitterShops(t)
	buyer := "233550003012"
	HandleInbound(Inbound{From: buyer, Text: "SHOP-EFUA"})
	c.replies(t, buyer)
	HandleInbound(Inbound{From: buyer, Text: "Handmade Beaded Bag"})
	c.replies(t, buyer)
	HandleInbound(Inbound{From: buyer, Text: "Send pic"})
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "I don't have the pic of the Handmade Beaded Bag in this chat. Let me get it from the shop owner for you.") {
		t.Fatalf("a photoless product should tell her plainly and promise the owner's pic, got %q", got)
	}
	ownerGot := strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(ownerGot, "A buyer is asking for a picture of Handmade Beaded Bag") {
		t.Fatalf("the owner should have been asked for the picture, got %q", ownerGot)
	}
}

// The colour rule: a listing whose photos showed one item
// in several colours saves under a colour-free name with
// the colours listed in the description; a single colour
// leaves the name and description exactly as they were.
func TestColourRule(t *testing.T) {
	flow := &OnboardingFlow{ColoursSeen: []string{"white", "black", "brown"}}
	name, desc := applyColourRule(flow, "White Maxi Dress", "White ribbed off-shoulder bodycon maxi dress")
	if name != "Maxi Dress" {
		t.Fatalf("the colour should come out of the name, got %q", name)
	}
	if !strings.Contains(desc, "Comes in white, black and brown.") {
		t.Fatalf("the colours should be listed in the description, got %q", desc)
	}

	one := &OnboardingFlow{ColoursSeen: []string{"black"}}
	name, desc = applyColourRule(one, "Black Stiletto Heels", "Plain black leather heels")
	if name != "Black Stiletto Heels" || desc != "Plain black leather heels" {
		t.Fatalf("one colour must leave the listing untouched, got %q / %q", name, desc)
	}

	two := &OnboardingFlow{ColoursSeen: []string{"rose gold", "silver"}}
	name, desc = applyColourRule(two, "Rose Gold Clutch", "Metallic clutch bag")
	if name != "Clutch" || !strings.Contains(desc, "Comes in rose gold and silver.") {
		t.Fatalf("a two-word colour should strip as a phrase, got %q / %q", name, desc)
	}

	none := &OnboardingFlow{ColoursSeen: []string{"red", "blue"}}
	name, _ = applyColourRule(none, "Ankara Fabric", "Bold print fabric")
	if name != "Ankara Fabric" {
		t.Fatalf("colours absent from the name change nothing, got %q", name)
	}
}

// The kept-photo cap: six photos are kept, a duplicate is
// already on the listing, and a seventh new photo is NOT
// kept — collectFlowPhoto says so, so no acknowledgement
// can claim an addition that did not happen.
func TestCollectFlowPhotoCap(t *testing.T) {
	flow := &OnboardingFlow{}
	for i := 1; i <= maxProductPhotos; i++ {
		if !collectFlowPhoto(flow, "img-"+string(rune('a'+i))) {
			t.Fatalf("photo %d should be kept", i)
		}
	}
	if len(flow.PhotoRefs) != maxProductPhotos {
		t.Fatalf("the listing should hold the cap, got %d", len(flow.PhotoRefs))
	}
	if collectFlowPhoto(flow, "img-overflow") {
		t.Fatalf("a photo past the cap must report not kept")
	}
	if len(flow.PhotoRefs) != maxProductPhotos {
		t.Fatalf("the overflow photo must not be recorded, got %d", len(flow.PhotoRefs))
	}
	if !collectFlowPhoto(flow, flow.PhotoRefs[0]) {
		t.Fatalf("a photo already on the listing reports kept")
	}
}
