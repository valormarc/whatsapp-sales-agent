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

// A forwarded advert riding as a photo caption is raw
// material, not a description: with no model to summarise
// it, the dump is not carried onto the listing at all, and
// no reply ever shows the seller or a buyer the emoji spec
// block she forwarded.
func TestAForwardedCaptionDumpIsNotTheDescription(t *testing.T) {
	owner := "233550002404"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}
	dump := "HP EliteBook 1030 G8 x360\nIntel Core i5 Processor\n11TH GENERATION\n2.6~4.4GHz Speed. (8CPUs)\n✅16GB RAM\n✅512GB SSD\n13.3\" FLIP TOUCHSCREEN\n✅x360 Convertible FHD IPS\n✅Facial ID Recognition\nKeyboard light, USB-C\nFew months Used in UK like new\nwith all accessories and box 📦\n\nGhc 6,800"
	HandleInbound(Inbound{From: owner, ImageID: "elite-1", Caption: dump})
	got := strings.Join(c.replies(t, owner), "\n")
	if strings.Contains(got, "✅") || strings.Contains(got, "FLIP TOUCHSCREEN") || strings.Contains(got, "8CPUs") {
		t.Fatalf("the forwarded dump reached the listing conversation: %q", got)
	}
	if !strings.Contains(got, "GH₵6,800") {
		t.Fatalf("her price should still be read from the dump's marked figure: %q", got)
	}
}

// An album is one act of sending: four photos of the same
// slides land seconds apart, the listing opens on the first,
// the second earns the one acknowledgement, and the rest are
// taken in silently. Four identical lines is spam, not
// acknowledgement.
func TestAnAlbumBurstIsAcknowledgedOnce(t *testing.T) {
	owner := "233550002405"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}
	HandleInbound(Inbound{From: owner, ImageID: "slides-1", Caption: "B buckle slides, 75gh each. Sizes from 37 to 42"})
	c.replies(t, owner)
	var rest []string
	for _, id := range []string{"slides-2", "slides-3", "slides-4", "slides-5"} {
		HandleInbound(Inbound{From: owner, ImageID: id, Caption: "B buckle slides, 75gh each. Sizes from 37 to 42"})
		rest = append(rest, c.replies(t, owner)...)
	}
	if len(rest) != 1 || rest[0] != "I am still on that same photo." {
		t.Fatalf("the burst should earn exactly one acknowledgement line, got %q", rest)
	}
}

// A different product held during an open listing is also
// acknowledged once per burst, not once per photo.
func TestADifferentProductBurstIsAcknowledgedOnce(t *testing.T) {
	owner := "233550002406"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}
	HandleInbound(Inbound{From: owner, ImageID: "heels-9", Caption: "Red heels 100 cedis"})
	c.replies(t, owner)
	var rest []string
	for _, p := range []struct{ id, caption string }{
		{"bag-1", "Blue bag 200 cedis"},
		{"hat-1", "Green hat 50 cedis"},
	} {
		HandleInbound(Inbound{From: owner, ImageID: p.id, Caption: p.caption})
		rest = append(rest, c.replies(t, owner)...)
	}
	count := 0
	for _, r := range rest {
		if strings.Contains(r, "One at a time") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the held products should earn one line between them, got %q", rest)
	}
}
