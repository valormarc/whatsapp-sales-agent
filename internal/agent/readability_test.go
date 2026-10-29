package agent

// Readability, the button half: a two-option question is
// asked on buttons, and a tap is read as the very word the
// person would have typed. The save confirmation is the
// proof case — a misread typed yes there once cost a
// listing — and the remove confirmation rides the same
// buttons the other way.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// buttonTitlesShown returns the button titles of every buttons
// message sent to one number in this test's outbox.
func buttonTitlesShown(t *testing.T, dir, to string) [][]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "data", "outbox.jsonl"))
	if err != nil {
		return nil
	}
	var shown [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec struct {
			To      string   `json:"to"`
			Kind    string   `json:"kind"`
			Buttons []string `json:"buttons"`
		}
		if json.Unmarshal([]byte(line), &rec) == nil && rec.To == to && rec.Kind == "buttons" {
			shown = append(shown, rec.Buttons)
		}
	}
	return shown
}

func hasButtons(shown [][]string, yes, no string) bool {
	for _, titles := range shown {
		if len(titles) == 2 && titles[0] == yes && titles[1] == no {
			return true
		}
	}
	return false
}

func TestSaveConfirmationArrivesOnButtonsAndTapSaves(t *testing.T) {
	owner := "233550007101"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	// The text walk, to its confirm step: name, description,
	// list price, last price, and the measure answer.
	c.say(t, owner, "add a product")
	c.replies(t, owner)
	c.say(t, owner, "Corner Rack")
	c.replies(t, owner)
	c.say(t, owner, "Steel frame with 4 tiers")
	c.replies(t, owner)
	c.say(t, owner, "270")
	c.replies(t, owner)
	c.say(t, owner, "250")
	if got := c.last(t, owner); strings.Contains(got, "measure") {
		c.say(t, owner, "whole")
	}
	got := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "Shall I add it?") {
		t.Fatalf("confirm question = %q", got)
	}
	if !hasButtons(buttonTitlesShown(t, dir, owner), "Yes, save it", "No, drop it") {
		t.Fatalf("the save question should ride on buttons, shown = %v", buttonTitlesShown(t, dir, owner))
	}

	HandleInbound(Inbound{From: owner, ButtonID: ynYesID})
	joined := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(joined, "Saved") {
		t.Fatalf("a yes tap should save, got %q", joined)
	}
	if p := catalogue.FindByName(MerchantID, "Corner Rack"); p == nil || p.ListPrice != 270 || p.FloorPrice != 250 {
		t.Fatalf("the tapped save should land at 270/250, got %+v", p)
	}
}

func TestRemoveConfirmationTapsKeepThenDrop(t *testing.T) {
	owner := "233550007201"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, owner, "remove Handmade Beaded Bag")
	c.replies(t, owner)
	if !hasButtons(buttonTitlesShown(t, dir, owner), "Yes, remove it", "No, keep it") {
		t.Fatalf("the remove question should ride on buttons, shown = %v", buttonTitlesShown(t, dir, owner))
	}
	HandleInbound(Inbound{From: owner, ButtonID: ynNoID})
	c.replies(t, owner)
	if catalogue.FindByName(MerchantID, "Handmade Beaded Bag") == nil {
		t.Fatal("a no tap should keep the product")
	}

	c.say(t, owner, "remove Handmade Beaded Bag")
	c.replies(t, owner)
	HandleInbound(Inbound{From: owner, ButtonID: ynYesID})
	c.replies(t, owner)
	if catalogue.FindByName(MerchantID, "Handmade Beaded Bag") != nil {
		t.Fatal("a yes tap should remove the product")
	}
}
