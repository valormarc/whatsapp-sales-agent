package agent

// Bringing buyers in: the moment a product goes live, its
// seller holds a Status caption and a forwardable broadcast
// carrying her shop link, written from the listing's facts.
// Her last price never appears in either; with a kept photo
// the Status rides the photo itself.

import (
	"strings"
	"testing"
)

const announceOwner = "233550001001"

func TestListingAnnouncementsRideTheSave(t *testing.T) {
	t.Setenv("AGENT_WHATSAPP_NUMBER", "233209450786")
	dir := setupChat(t, announceOwner, emptyShop)
	c := &chat{dir: dir}

	c.say(t, announceOwner, "add a product")
	c.last(t, announceOwner)
	c.say(t, announceOwner, "Kente Cloth")
	c.last(t, announceOwner)
	c.say(t, announceOwner, "Handwoven kente cloth, bright traditional patterns.")
	c.last(t, announceOwner)
	c.say(t, announceOwner, "GH₵600")
	c.last(t, announceOwner)
	c.say(t, announceOwner, "480 cedis")
	c.last(t, announceOwner)
	c.say(t, announceOwner, "only as a whole")
	c.last(t, announceOwner)
	c.say(t, announceOwner, "yes")
	got := c.joined(t, announceOwner)

	if !strings.Contains(got, "For your Status, copy this:") || !strings.Contains(got, "To forward to your contacts and groups:") {
		t.Fatalf("a save should hand her both announcements, got %q", got)
	}
	for _, want := range []string{"Kente Cloth", "GH₵600", "https://wa.me/233209450786?text=SHOP-TEST"} {
		if strings.Count(got, want) < 2 {
			t.Fatalf("both announcements should carry %q, got %q", want, got)
		}
	}
	announcements := got[strings.Index(got, "For your Status"):]
	if strings.Contains(announcements, "GH₵480") {
		t.Fatalf("her last price must never reach an announcement, got %q", announcements)
	}
}

func TestListingAnnouncementRidesThePhoto(t *testing.T) {
	stubPhotoMedia(t)
	dir := setupChat(t, announceOwner, emptyShop)
	c := &chat{dir: dir}

	c.photo(t, announceOwner, "300")
	c.replies(t, announceOwner)
	c.say(t, announceOwner, "Leather Heels")
	c.last(t, announceOwner)
	c.say(t, announceOwner, "Black leather heels.")
	c.last(t, announceOwner)
	c.say(t, announceOwner, "250")
	c.last(t, announceOwner)
	c.say(t, announceOwner, "whole")
	c.last(t, announceOwner)
	c.say(t, announceOwner, "yes")

	got := c.replies(t, announceOwner)
	status := ""
	for _, body := range got {
		if strings.Contains(body, "To forward to your contacts and groups:") {
			if !strings.Contains(body, "Leather Heels") || !strings.Contains(body, "GH₵300") || !strings.Contains(body, "SHOP-TEST") {
				t.Fatalf("the broadcast should carry the goods, the price and the door, got %q", body)
			}
		}
		if strings.Contains(body, "Leather Heels — GH₵300") {
			status = body
		}
	}
	if status == "" {
		t.Fatalf("the Status caption should have ridden the photo as its caption, got %q", got)
	}
}
