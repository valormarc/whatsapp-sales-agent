package agent

import (
	"strings"
	"testing"
)

// The heels advert from the live LUXE thread (2026-10-28): a
// seller forwarded her advert and then her photo, and the
// advert was thanked and noted instead of listed.
const heelsAdvert = "So pretty 120 cedis\n\nBlack 36 to 41\nGold 36 to 41\nSilver 36 to 41\nChampagne 36 to 41"

// A pasted advert with no walk open opens the walk itself:
// the paste is the instruction to list it, for a stocked
// shop and for an empty one alike, and only what the advert
// did not carry is asked for.
func TestAPastedAdvertOpensTheWalk(t *testing.T) {
	for _, fixture := range []string{shopWithBag, emptyShop} {
		owner := "233550002400"
		dir := setupChat(t, owner, fixture)
		c := &chat{dir: dir}
		c.say(t, owner, heelsAdvert)
		got := strings.Join(c.replies(t, owner), "\n")
		if strings.Contains(got, "I have noted") || strings.Contains(got, "shelf") && strings.Contains(got, "empty") {
			t.Fatalf("the advert was deflected instead of listed (fixture %.20s): %q", fixture, got)
		}
		if !strings.Contains(got, "I took the price from your message: GH₵120") {
			t.Fatalf("the price was not read back to her: %q", got)
		}
		if !strings.Contains(got, "last price") || strings.Contains(got, "What is the list price") {
			t.Fatalf("the walk should stand at the last price with the list price carried: %q", got)
		}
	}
}

// A photo that follows her advert joins the same listing:
// the price her advert carried is not asked for again.
func TestAPhotoAfterHerAdvertKeepsTheAdvertPrice(t *testing.T) {
	owner := "233550002401"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}
	c.say(t, owner, heelsAdvert)
	c.replies(t, owner)
	HandleInbound(Inbound{From: owner, ImageID: "heels-1"})
	got := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "Got your photo.") {
		t.Fatalf("the photo was not taken into the listing: %q", got)
	}
	if strings.Contains(got, "What is the list price") {
		t.Fatalf("the advert's price was asked for again after the photo: %q", got)
	}
}

// The same photo sent again while its listing is open gets a
// short reminder of what is awaited, never the full question
// repeated word for word a second later.
func TestARepeatedPhotoRemindsShortly(t *testing.T) {
	owner := "233550002402"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}
	HandleInbound(Inbound{From: owner, ImageID: "heels-2", Caption: "Sparkly heels"})
	c.replies(t, owner)
	HandleInbound(Inbound{From: owner, ImageID: "heels-2", Caption: "Sparkly heels"})
	got := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "I am still on that same photo.") {
		t.Fatalf("the repeat was not acknowledged: %q", got)
	}
	if strings.Contains(got, "That is the lowest I can go") || strings.Count(got, "What is the list price") > 0 && strings.Contains(got, "Give it in cedis") {
		t.Fatalf("the full question was repeated verbatim: %q", got)
	}
}
