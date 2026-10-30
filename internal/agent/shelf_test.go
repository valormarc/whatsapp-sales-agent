package agent

// The shelf sheet, pinned: composition under the platform's
// limits (ten rows, paged by nine with a More row), titles
// trimmed between words, a tap putting the exact product in
// front of the buyer the way naming it does, and a More tap
// opening the next sheet.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// sheetText flattens what a phone shows of every list
// sheet sent to a number: section titles, row titles and
// row descriptions, so a test can assert on the sheet's
// contents the way a buyer reads them.
func sheetText(t *testing.T, dir, to string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "data", "outbox.jsonl"))
	if err != nil {
		return ""
	}
	var parts []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec struct {
			To       string                 `json:"to"`
			Kind     string                 `json:"kind"`
			Sections []whatsapp.ListSection `json:"sections"`
		}
		if json.Unmarshal([]byte(line), &rec) == nil && rec.To == to && rec.Kind == "list" {
			for _, s := range rec.Sections {
				parts = append(parts, s.Title)
				for _, r := range s.Rows {
					parts = append(parts, r.Title, r.Description)
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

// lastSheetText flattens only the most recent list sheet
// sent to a number — the sheet a switch or a paging tap
// just put in front of her, with none of the sheets she
// was shown before it.
func lastSheetText(t *testing.T, dir, to string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "data", "outbox.jsonl"))
	if err != nil {
		return ""
	}
	var last []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec struct {
			To       string                 `json:"to"`
			Kind     string                 `json:"kind"`
			Sections []whatsapp.ListSection `json:"sections"`
		}
		if json.Unmarshal([]byte(line), &rec) == nil && rec.To == to && rec.Kind == "list" {
			last = nil
			for _, s := range rec.Sections {
				last = append(last, s.Title)
				for _, r := range s.Rows {
					last = append(last, r.Title, r.Description)
				}
			}
		}
	}
	return strings.Join(last, "\n")
}

const shopLongShelf = `{"merchants":{"demo":{"id":"demo","name":"Long Shelf Shop","shopCode":"LONG","dailyGiveawayAllowance":300,"products":[
{"id":"item-1","name":"Item One","description":"The first item.","listPrice":110,"floorPrice":90,"stock":2},
{"id":"item-2","name":"Item Two","description":"The second item.","listPrice":120,"floorPrice":95,"stock":2},
{"id":"item-3","name":"Item Three","description":"The third item.","listPrice":130,"floorPrice":100,"stock":2},
{"id":"item-4","name":"Item Four","description":"The fourth item.","listPrice":140,"floorPrice":110,"stock":2},
{"id":"item-5","name":"Item Five","description":"The fifth item.","listPrice":150,"floorPrice":120,"stock":2},
{"id":"item-6","name":"Item Six","description":"The sixth item.","listPrice":160,"floorPrice":130,"stock":2},
{"id":"item-7","name":"Item Seven","description":"The seventh item.","listPrice":170,"floorPrice":140,"stock":2},
{"id":"item-8","name":"Item Eight","description":"The eighth item.","listPrice":180,"floorPrice":150,"stock":2},
{"id":"item-9","name":"Item Nine","description":"The ninth item.","listPrice":190,"floorPrice":160,"stock":2},
{"id":"item-10","name":"Item Ten","description":"The tenth item.","listPrice":200,"floorPrice":170,"stock":2},
{"id":"item-11","name":"Ribbed Off-Shoulder Bodycon Maxi Dress","description":"A very long named dress.","listPrice":210,"floorPrice":180,"stock":2},
{"id":"item-12","name":"Item Twelve","description":"The twelfth item.","listPrice":220,"floorPrice":190,"stock":2},
{"id":"item-13","name":"Gone Item","description":"Sold out.","listPrice":230,"floorPrice":200,"stock":0}
]}}}`

func TestShelfPagesByNineWithAMoreRow(t *testing.T) {
	owner := "233550009001"
	dir := setupChat(t, owner, shopLongShelf)
	shop := catalogue.GetMerchant(MerchantID)
	if shop == nil {
		t.Fatal("the fixture shop should exist")
	}
	_ = dir

	first := shelfSections(shop, 1)
	if len(first) != 1 || len(first[0].Rows) != 10 {
		t.Fatalf("page 1 should carry 9 items and the More row, got %+v", first)
	}
	if first[0].Title != "Shelf 1 of 2" {
		t.Fatalf("page 1 title = %q", first[0].Title)
	}
	last := first[0].Rows[9]
	if last.ID != "shelfmore_2" || last.Title != "More items" {
		t.Fatalf("the tenth row should be the More row, got %+v", last)
	}
	second := shelfSections(shop, 2)
	if len(second[0].Rows) != 3 {
		t.Fatalf("page 2 should carry the remaining 3 in-stock items, got %d rows", len(second[0].Rows))
	}
	for _, r := range second[0].Rows {
		if strings.HasPrefix(r.ID, shelfMorePrefix) {
			t.Fatalf("the last page carries no More row, got %+v", r)
		}
		if r.ID == "shelf_item-13" {
			t.Fatal("a sold-out item must not take a row")
		}
	}
	// The long name is trimmed for the sheet, between words,
	// inside the platform's 24 characters.
	found := false
	for _, r := range second[0].Rows {
		if r.ID == "shelf_item-11" {
			found = true
			if n := len([]rune(r.Title)); n > 24 {
				t.Fatalf("row title is %d runes, over the 24 limit: %q", n, r.Title)
			}
			if r.Title == "Ribbed Off-Shoulder Bodycon Maxi Dress" {
				t.Fatal("the long name should have been trimmed for display")
			}
		}
	}
	if !found {
		t.Fatal("the long-named dress should be on page 2")
	}
}

func TestShelfTapPutsTheProductInFrontOfHer(t *testing.T) {
	owner := "233550009002"
	buyer := "233550009102"
	dir := setupChat(t, owner, shopLongShelf)
	c := &chat{dir: dir}

	c.say(t, buyer, "SHOP-LONG")
	entry := strings.Join(c.replies(t, buyer), "\n") + "\n" + sheetText(t, dir, buyer)
	if !strings.Contains(entry, "Item One") || !strings.Contains(entry, "More items") {
		t.Fatalf("entry should present the first shelf sheet, got %q", entry)
	}

	HandleInbound(Inbound{From: buyer, ListID: "shelf_item-3", MessageID: "wamid.SHELF-T1"})
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Item Three") || !strings.Contains(got, "GH₵130") {
		t.Fatalf("a product tap should present the item and its price, got %q", got)
	}
	s := sessions[sessionKey(MerchantID, buyer)]
	if s == nil || s.LastViewed == nil || s.LastViewed.ID != "item-3" {
		t.Fatalf("the tapped item should be what she is looking at, got %+v", s)
	}

	HandleInbound(Inbound{From: buyer, ListID: "shelfmore_2", MessageID: "wamid.SHELF-T2"})
	if rows := sheetText(t, dir, buyer); !strings.Contains(rows, "Shelf 2 of 2") || !strings.Contains(rows, "Item Twelve") {
		t.Fatalf("the More tap should open the second sheet, got %q", rows)
	}
}
