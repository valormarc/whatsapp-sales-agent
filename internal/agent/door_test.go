package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// The front door: a stranger whose first word is a bare greeting
// meets one message with two reply buttons, Buy and Sell. The door
// is for strangers only, it is offered once, and typing keeps
// working underneath it: the buttons are a shortcut, never a gate.

type doorRecord struct {
	To      string   `json:"to"`
	Body    string   `json:"body"`
	Kind    string   `json:"kind"`
	Buttons []string `json:"buttons"`
}

func doorRecords(t *testing.T, dir, to string) []doorRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "data", "outbox.jsonl"))
	if err != nil {
		return nil
	}
	var out []doorRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec doorRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("outbox line not JSON: %v", err)
		}
		if rec.To == to {
			out = append(out, rec)
		}
	}
	return out
}

func buttonsShown(recs []doorRecord) (found bool, titles []string) {
	for _, r := range recs {
		if r.Kind == "buttons" {
			return true, r.Buttons
		}
	}
	return false, nil
}

func TestStrangerGreetingGetsTheDoorOnce(t *testing.T) {
	dir, c := setupTwoShops(t)
	stranger := "233550008001"

	c.say(t, stranger, "Hello")
	recs := doorRecords(t, dir, stranger)
	shown, titles := buttonsShown(recs)
	if !shown {
		t.Fatalf("a stranger's hello should meet the door buttons, got %+v", recs)
	}
	if len(titles) != 2 || titles[0] != "I came to buy" || titles[1] != "I want to sell" {
		t.Fatalf("door buttons = %v, want the Buy and Sell pair", titles)
	}

	// A real question after the door gets the plain ask, with no
	// buttons attached, and nothing leaks from either shelf.
	c.say(t, stranger, "How much is the beaded bag?")
	recs = doorRecords(t, dir, stranger)
	if got := recs[len(recs)-1]; got.Kind == "buttons" || !strings.Contains(got.Body, "shop code") {
		t.Fatalf("post-door ask = %+v, want the plain shop code ask", got)
	}
	// And a second greeting does not bring the buttons back.
	c.say(t, stranger, "Hi")
	recs = doorRecords(t, dir, stranger)
	count := 0
	for _, r := range recs {
		if r.Kind == "buttons" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("door shown %d times, want exactly once", count)
	}
}

func TestDoorBuyTapAsksForTheShopCodeAndEntryStillWorks(t *testing.T) {
	_, c := setupTwoShops(t)
	stranger := "233550008002"

	c.say(t, stranger, "Hello")
	c.replies(t, stranger)
	HandleInbound(Inbound{From: stranger, ButtonID: "door_buy"})
	if got := c.last(t, stranger); !strings.Contains(got, "shop code") {
		t.Fatalf("Buy tap reply = %q, want the shop code ask", got)
	}
	c.say(t, stranger, "SHOP-AMA")
	got := strings.Join(c.replies(t, stranger), "\n")
	if !strings.Contains(got, "Ama's Stall") || !strings.Contains(got, "Smoked Tilapia") {
		t.Fatalf("entry after the door = %q", got)
	}
}

func TestDoorSellTapOpensSignup(t *testing.T) {
	_, c := setupTwoShops(t)
	stranger := "233550008003"

	c.say(t, stranger, "Hello")
	c.replies(t, stranger)
	HandleInbound(Inbound{From: stranger, ButtonID: "door_sell"})
	if got := c.last(t, stranger); !strings.Contains(got, "what is your shop called") {
		t.Fatalf("Sell tap reply = %q, want the signup opening", got)
	}
	c.say(t, stranger, "Door Test Provisions")
	c.last(t, stranger)
	shop := catalogue.MerchantByOwner(stranger)
	if shop == nil || shop.Name != "Door Test Provisions" || shop.ShopCode != "DOOR" {
		t.Fatalf("shop opened from a Sell tap = %+v", shop)
	}
}

func TestOwnerAndLinkBuyerNeverSeeTheDoor(t *testing.T) {
	dir, c := setupTwoShops(t)

	c.say(t, ownerEfua, "Hello")
	if shown, _ := buttonsShown(doorRecords(t, dir, ownerEfua)); shown {
		t.Fatal("a known merchant was shown the front door buttons")
	}
	if got := c.replies(t, ownerEfua); len(got) == 0 {
		t.Fatal("the merchant's hello got no reply at all")
	}

	buyer := "233550008004"
	c.say(t, buyer, "SHOP-EFUA")
	if shown, _ := buttonsShown(doorRecords(t, dir, buyer)); shown {
		t.Fatal("a buyer who arrived by a shop link was shown the front door buttons")
	}
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Efua's Store") {
		t.Fatalf("link buyer greeting = %q", got)
	}
}
