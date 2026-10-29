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
// the price her advert carried is not asked for again, the
// outstanding last price question is not repeated either,
// and her answer to it saves the product at once, because
// the listing is a photo listing now.
func TestAPhotoAfterHerAdvertKeepsTheAdvertPrice(t *testing.T) {
	owner := "233550002401"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}
	c.say(t, owner, heelsAdvert)
	c.replies(t, owner)
	HandleInbound(Inbound{From: owner, ImageID: "heels-1"})
	got := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "Photo added to So pretty.") {
		t.Fatalf("the photo was not taken into the listing: %q", got)
	}
	if strings.Contains(got, "What is the list price") || strings.Count(got, "last price") > 0 {
		t.Fatalf("a settled price or the outstanding question was repeated after the photo: %q", got)
	}
	// With no model to judge the unit, it is genuinely
	// undecided and the measure question is the one question
	// that survives; her "whole" then saves at once. (Live,
	// the judgement settles it silently and even that question
	// never appears.)
	c.say(t, owner, "100")
	got = strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "by measure") {
		t.Fatalf("an undecided unit should still earn the measure question without a model: %q", got)
	}
	c.say(t, owner, "whole")
	got = strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "Saved ✅ So pretty | List: GH₵120 | Last: GH₵100 | Unit: piece") {
		t.Fatalf("the photo listing did not save with the one saved line: %q", got)
	}
}

// The same photo sent again while its listing is open gets
// one short line, and nothing is asked again.
func TestARepeatedPhotoRemindsShortly(t *testing.T) {
	owner := "233550002402"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}
	HandleInbound(Inbound{From: owner, ImageID: "heels-2", Caption: "Sparkly heels"})
	c.replies(t, owner)
	HandleInbound(Inbound{From: owner, ImageID: "heels-2", Caption: "Sparkly heels"})
	got := strings.Join(c.replies(t, owner), "\n")
	if got != "I am still on that same photo." {
		t.Fatalf("the repeat should earn exactly one short line: %q", got)
	}
}

// A photo listing in the compact shape: her advert carried
// the name and description but no price, the photo joins,
// and the compact message asks the one missing thing. Each
// answer moves exactly one step, and the save lands with the
// one saved line, no summary anywhere.
func TestPhotoCompactFlow(t *testing.T) {
	owner := "233550002403"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}
	c.say(t, owner, "So pretty\n\nBlack 36 to 41\nGold 36 to 41")
	c.replies(t, owner)
	HandleInbound(Inbound{From: owner, ImageID: "romper-1"})
	got := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "Got it ✅ So pretty") || !strings.Contains(got, "What is the list price, in cedis?") {
		t.Fatalf("the compact message did not ask the one missing thing: %q", got)
	}
	c.say(t, owner, "80")
	got = strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "What is your last price? If non-negotiable, say 'non-negotiable'.") {
		t.Fatalf("the compact last price question did not follow: %q", got)
	}
	c.say(t, owner, "75")
	got = strings.Join(c.replies(t, owner), "\n")
	if strings.Contains(got, "by measure") {
		c.say(t, owner, "whole")
		got = strings.Join(c.replies(t, owner), "\n")
	}
	if !strings.Contains(got, "Saved ✅ So pretty | List: GH₵80 | Last: GH₵75 | Unit: piece") {
		t.Fatalf("the compact saved line did not land: %q", got)
	}
	if strings.Contains(got, "Shall I add") || strings.Contains(got, "Nothing is saved until you say yes") {
		t.Fatalf("a summary confirmation survived into the photo flow: %q", got)
	}
}
