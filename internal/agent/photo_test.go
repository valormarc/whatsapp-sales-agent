package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// Throw-in onboarding, in the styles the selling groups actually
// post in: a photo with a bare figure, a photo with a size and an
// @ price, a photo with a full write-up, a photo with nothing, a
// photo with one word, and a photo whose caption carries a
// measure. In every style the agent reads what the seller gave,
// asks only for what she did not give, and saves nothing until
// her clear yes. The last price is asked in every single style,
// because no post ever carries it.

func (c *chat) photo(t *testing.T, from, caption string) {
	t.Helper()
	HandleInbound(Inbound{From: from, ImageID: "img-throw-1", Caption: caption})
}

func finishWhole(t *testing.T, c *chat, merchant string) {
	t.Helper()
	if got := c.last(t, merchant); !strings.Contains(got, "by measure") {
		t.Fatalf("measure question = %q", got)
	}
	c.say(t, merchant, "whole")
	if got := c.last(t, merchant); !strings.Contains(got, "Shall I add it?") {
		t.Fatalf("confirm = %q", got)
	}
	c.say(t, merchant, "yes")
	if got := c.joined(t, merchant); !strings.Contains(got, "Saved") {
		t.Fatalf("saved reply = %q", got)
	}
}

func TestThrowInBareFigure(t *testing.T) {
	merchant := "233550001101"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, merchant, "300")
	replies := c.replies(t, merchant)
	if len(replies) != 3 || replies[0] != "Got your photo." {
		t.Fatalf("photo replies = %q", replies)
	}
	if !strings.Contains(replies[1], "List price GH₵300") {
		t.Fatalf("readback = %q", replies[1])
	}
	if !strings.Contains(replies[2], "What is it called?") {
		t.Fatalf("first question = %q", replies[2])
	}

	c.say(t, merchant, "Leather Heels")
	if got := c.last(t, merchant); !strings.Contains(got, "Tell me about Leather Heels") {
		t.Fatalf("description question = %q", got)
	}
	c.say(t, merchant, "Black leather heels.")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") || strings.Contains(got, "What is the list price") {
		t.Fatalf("after description, want the last price question only, got %q", got)
	}
	c.say(t, merchant, "240")
	finishWhole(t, c, merchant)

	p := catalogue.FindByName("demo", "Leather Heels")
	if p == nil || p.ListPrice != 300 || p.FloorPrice != 240 {
		t.Fatalf("saved product = %+v, want list 300 floor 240", p)
	}
}

func TestThrowInSizeAndAtPrice(t *testing.T) {
	merchant := "233550001102"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, merchant, "Size 44 Available. @250ghc")
	replies := c.replies(t, merchant)
	joined := strings.Join(replies, "\n")
	if !strings.Contains(joined, "List price GH₵250") {
		t.Fatalf("the @ figure is the price, replies = %q", replies)
	}
	if strings.Contains(joined, "GH₵44") {
		t.Fatalf("the size was read as a price, replies = %q", replies)
	}
	if !strings.Contains(joined, "Size 44") {
		t.Fatalf("her words were kept as the description, replies = %q", replies)
	}

	c.say(t, merchant, "Court Heels")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") {
		t.Fatalf("name given, want the last price question next, got %q", got)
	}
	c.say(t, merchant, "200")
	finishWhole(t, c, merchant)

	p := catalogue.FindByName("demo", "Court Heels")
	if p == nil || p.ListPrice != 250 || p.FloorPrice != 200 || !strings.Contains(p.Description, "Size 44") {
		t.Fatalf("saved product = %+v, want list 250 floor 200 with her size words kept", p)
	}
}

func TestThrowInLongWriteUp(t *testing.T) {
	merchant := "233550001103"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, merchant, "HP EliteBook 840 laptop, Intel Core i7, 16GB RAM, 512GB SSD. Brand new sealed with manufacturer warranty. GHS 4500")
	replies := c.replies(t, merchant)
	joined := strings.Join(replies, "\n")
	if !strings.Contains(joined, "List price GH₵4,500") || !strings.Contains(joined, "Brand new sealed") {
		t.Fatalf("readback = %q", replies)
	}
	if strings.Contains(joined, "Tell me about") || strings.Contains(joined, "What is the list price") {
		t.Fatalf("she already gave a description and a price, so neither is asked, replies = %q", replies)
	}
	if !strings.Contains(replies[len(replies)-1], "What is it called?") {
		t.Fatalf("the one missing piece is the name, replies = %q", replies)
	}

	c.say(t, merchant, "HP EliteBook 840")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") {
		t.Fatalf("after name, want the last price question, got %q", got)
	}
	c.say(t, merchant, "3900")
	finishWhole(t, c, merchant)

	p := catalogue.FindByName("demo", "HP EliteBook 840")
	if p == nil || p.ListPrice != 4500 || p.FloorPrice != 3900 || !strings.Contains(p.Description, "512GB") {
		t.Fatalf("saved product = %+v, want list 4500 floor 3900 with her write-up kept", p)
	}
}

func TestThrowInNoCaption(t *testing.T) {
	merchant := "233550001104"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, merchant, "")
	replies := c.replies(t, merchant)
	if strings.Contains(strings.Join(replies, "\n"), "Here is what I have from it") {
		t.Fatalf("nothing was sent, so nothing is read back, replies = %q", replies)
	}
	if got := replies[len(replies)-1]; !strings.Contains(got, "What is it called?") {
		t.Fatalf("first question = %q", got)
	}

	// From here the walk is the guided one, question by question.
	c.say(t, merchant, "Rice Cooker")
	c.say(t, merchant, "Electric rice cooker, 2 litres.")
	if got := c.last(t, merchant); !strings.Contains(got, "What is the list price") {
		t.Fatalf("no price was given, so the price is asked, got %q", got)
	}
	c.say(t, merchant, "300")
	c.say(t, merchant, "250")
	finishWhole(t, c, merchant)

	p := catalogue.FindByName("demo", "Rice Cooker")
	if p == nil || p.ListPrice != 300 || p.FloorPrice != 250 {
		t.Fatalf("saved product = %+v, want list 300 floor 250", p)
	}
}

func TestThrowInOneWord(t *testing.T) {
	merchant := "233550001105"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, merchant, "Heels")
	replies := c.replies(t, merchant)
	if !strings.Contains(strings.Join(replies, "\n"), "Name: Heels") {
		t.Fatalf("the one word is the name, replies = %q", replies)
	}
	if got := replies[len(replies)-1]; !strings.Contains(got, "Tell me about Heels") {
		t.Fatalf("first question = %q", got)
	}

	c.say(t, merchant, "Ladies court heels, black.")
	if got := c.last(t, merchant); !strings.Contains(got, "What is the list price") {
		t.Fatalf("no price was given, so the price is asked, got %q", got)
	}
	c.say(t, merchant, "180")
	c.say(t, merchant, "140")
	finishWhole(t, c, merchant)

	p := catalogue.FindByName("demo", "Heels")
	if p == nil || p.ListPrice != 180 || p.FloorPrice != 140 {
		t.Fatalf("saved product = %+v, want list 180 floor 140", p)
	}
}

func TestThrowInMeasureCaption(t *testing.T) {
	merchant := "233550001106"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, merchant, "Ankara Fabric, 6 yards. 450gh")
	replies := c.replies(t, merchant)
	joined := strings.Join(replies, "\n")
	if !strings.Contains(joined, "List price GH₵450") || !strings.Contains(joined, "Sold by the yard") {
		t.Fatalf("readback = %q", replies)
	}

	c.say(t, merchant, "Ankara Fabric")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") {
		t.Fatalf("after name, want the last price question, got %q", got)
	}
	c.say(t, merchant, "360")
	// Her caption already carried the measure, so there is no
	// measure question: the floor answer lands on the summary,
	// which shows the per-yard price her figures make.
	if got := c.last(t, merchant); !strings.Contains(got, "Shall I add it?") || !strings.Contains(got, "GH₵75 per yard") {
		t.Fatalf("confirm = %q", got)
	}
	c.say(t, merchant, "yes")
	if got := c.joined(t, merchant); !strings.Contains(got, "Saved") {
		t.Fatalf("saved reply = %q", got)
	}

	p := catalogue.FindByName("demo", "Ankara Fabric")
	if p == nil || !p.ByMeasure() || p.Unit != "yard" || p.BaseQty != 6 || p.ListPrice != 450 || p.FloorPrice != 360 {
		t.Fatalf("saved product = %+v, want 6 yards at list 450 floor 360", p)
	}
}

func TestThrowInUnmarkedNumbersAreNotPrices(t *testing.T) {
	if d := throwDraftFromCaption("Used laptop, 16GB RAM, 512GB SSD, 15 inch screen, very neat and clean"); d.ListPrice != 0 {
		t.Fatalf("unmarked specification numbers read as a price: %+v", d)
	}
	if d := throwDraftFromCaption("Size 44"); d.ListPrice != 0 {
		t.Fatalf("a size read as a price: %+v", d)
	}
	if d := throwDraftFromCaption("@700"); d.ListPrice != 700 {
		t.Fatalf("@ figure not read as the price: %+v", d)
	}
	if d := throwDraftFromCaption("still available"); d.Name != "" || d.Description != "" || d.ListPrice != 0 {
		t.Fatalf("pure filler read as product detail: %+v", d)
	}
	if d := throwDraftFromCaption("Ghs400 / 1 and half year advance"); d.ListPrice != 400 {
		t.Fatalf("a cedi figure at the head of a text post not read: %+v", d)
	}
}

func TestThrowInCountFigureIsNotAPrice(t *testing.T) {
	// The live fault this pins: a wardrobe caption whose only
	// figure counts its doors. The figure is a count, so no price
	// is read and the walk asks for the price as a question
	// instead of trapping her floor against GH₵3.
	if d := throwDraftFromCaption("Marble wardrobe with mirror, 3 doors"); d.ListPrice != 0 {
		t.Fatalf("a door count read as a price: %+v", d)
	}
	if d := throwDraftFromCaption("3 seater sofa, very neat"); d.ListPrice != 0 {
		t.Fatalf("a seater count read as a price: %+v", d)
	}
	if d := throwDraftFromCaption("3-door wardrobe"); d.ListPrice != 0 {
		t.Fatalf("a hyphenated count read as a price: %+v", d)
	}
	if d := throwDraftFromCaption("Heels 250"); d.ListPrice != 250 {
		t.Fatalf("a figure standing at the end is still a price: %+v", d)
	}
	if d := throwDraftFromCaption("Wardrobe, 250 negotiable"); d.ListPrice != 250 {
		t.Fatalf("price talk after a figure still reads as a price: %+v", d)
	}
}

func TestThrowInPriceCorrectionInsideTheFlow(t *testing.T) {
	merchant := "233550001108"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	// A price she later sees is wrong can be corrected in one
	// line wherever the flow is standing, instead of trapping
	// her against it.
	c.photo(t, merchant, "300")
	c.say(t, merchant, "Leather Heels")
	c.say(t, merchant, "Black leather heels.")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") {
		t.Fatalf("standing at the last price question, got %q", got)
	}
	c.say(t, merchant, "price is 2500")
	replies := c.replies(t, merchant)
	if !strings.Contains(replies[0], "Corrected") || !strings.Contains(replies[0], "GH₵2,500") {
		t.Fatalf("correction reply = %q", replies)
	}
	if !strings.Contains(replies[len(replies)-1], "last price") {
		t.Fatalf("after the correction the last price is asked against the new price, got %q", replies)
	}
	c.say(t, merchant, "2000")
	finishWhole(t, c, merchant)

	p := catalogue.FindByName("demo", "Leather Heels")
	if p == nil || p.ListPrice != 2500 || p.FloorPrice != 2000 {
		t.Fatalf("saved product = %+v, want list 2500 floor 2000", p)
	}
}

func TestThrowInSamePhotoTwiceIsNotReadBackTwice(t *testing.T) {
	merchant := "233550001109"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, merchant, "300")
	_ = c.replies(t, merchant)
	// Inside the same burst the resend is silent: the first
	// reply already covered it. After the window, one short
	// line answers it, and the readback never repeats.
	c.photo(t, merchant, "300")
	if replies := c.replies(t, merchant); len(replies) != 0 {
		t.Fatalf("a resend inside the burst should be silent, got %q", replies)
	}
	for _, candidate := range sessions {
		if candidate.Onboarding != nil {
			candidate.Onboarding.RepeatAckAt = candidate.Onboarding.RepeatAckAt.Add(-2 * photoAckWindow)
		}
	}
	c.photo(t, merchant, "300")
	replies := c.replies(t, merchant)
	joined := strings.Join(replies, "\n")
	if !strings.Contains(joined, "still on that same photo") {
		t.Fatalf("the late resend replies = %q", replies)
	}
	if strings.Contains(joined, "Here is what I have from it") {
		t.Fatalf("the readback was repeated on a resend: %q", replies)
	}
}

func TestBuyerPhotoDoesNotOpenOnboarding(t *testing.T) {
	merchant := "233550001107"
	buyer := "233550002107"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, buyer, "Handmade Beaded Bag 500")
	if got := c.last(t, buyer); strings.Contains(got, "What is it called?") || strings.Contains(got, "Here is what I have from it") {
		t.Fatalf("a buyer's photo opened merchant onboarding: %q", got)
	}
	if p := catalogue.FindByName("demo", "Handmade Beaded Bag"); p == nil || p.ListPrice != 850 {
		t.Fatalf("the shelf changed behind a buyer's photo: %+v", p)
	}
}
