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
	if !strings.Contains(saved, "Saved ✅ B Buckle Slides | List: GH₵75 | Last: GH₵70") {
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
func TestAProductWithoutPhotosAnswersInWords(t *testing.T) {
	stubPhotoMedia(t)
	owner := "233550003011"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550003012"
	HandleInbound(Inbound{From: buyer, Text: "SHOP-TEST"})
	c.replies(t, buyer)
	HandleInbound(Inbound{From: buyer, Text: "Send pic of the beaded bag"})
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "I cannot send a photo") {
		t.Fatalf("a photoless product should answer in words, got %q", got)
	}
}
