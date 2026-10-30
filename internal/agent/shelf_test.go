package agent

// The shelf sheet, pinned: composition under the platform's
// limits (ten rows, paged with a More row), titles trimmed
// between words, and the tap-to-add picking — a tap adds an
// item to her picks and the sheet returns with it ticked, a
// second tap takes it off, the Done row rides at the top
// while she holds picks and narrows the pages by its row,
// and Done starts the settling that ends in one order.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
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

// lastSheetBody is the words over the most recent sheet —
// a tap's answer rides there, so it is where "Added the…"
// is read from.
func lastSheetBody(t *testing.T, dir, to string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "data", "outbox.jsonl"))
	if err != nil {
		return ""
	}
	body := ""
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec struct {
			To   string `json:"to"`
			Kind string `json:"kind"`
			Body string `json:"body"`
		}
		if json.Unmarshal([]byte(line), &rec) == nil && rec.To == to && rec.Kind == "list" {
			body = rec.Body
		}
	}
	return body
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

	first := shelfSections(shop, 1, nil)
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
	second := shelfSections(shop, 2, nil)
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

func TestShelfDoneRowNarrowsThePages(t *testing.T) {
	owner := "233550009003"
	setupChat(t, owner, shopLongShelf)
	shop := catalogue.GetMerchant(MerchantID)
	s := &Session{ShelfPicks: []string{"item-1"}}

	first := shelfSections(shop, 1, s)
	if len(first) != 2 || first[0].Title != "Your picks" {
		t.Fatalf("the Done section should ride first, got %+v", first)
	}
	done := first[0].Rows[0]
	if done.ID != shelfDoneID || !strings.Contains(done.Description, "1 item") || !strings.Contains(done.Description, "GH₵110") {
		t.Fatalf("the Done row should carry the hand so far, got %+v", done)
	}
	if len(first[1].Rows) != 9 {
		t.Fatalf("page 1 should narrow to 8 items and the More row beside Done, got %d rows", len(first[1].Rows))
	}
	total := 0
	for _, sec := range first {
		total += len(sec.Rows)
	}
	if total != 10 {
		t.Fatalf("the whole sheet is ten rows, got %d", total)
	}
	second := shelfSections(shop, 2, s)
	if len(second) != 2 || len(second[1].Rows) != 4 {
		t.Fatalf("page 2 should carry the remaining 4 items, got %+v", second)
	}
	for _, r := range second[1].Rows {
		if strings.HasPrefix(r.ID, shelfMorePrefix) {
			t.Fatalf("the last page carries no More row, got %+v", r)
		}
	}
}

func TestShelfTapAddsTicksAndTakesOff(t *testing.T) {
	owner := "233550009002"
	buyer := "233550009102"
	dir := setupChat(t, owner, shopLongShelf)
	c := &chat{dir: dir}

	c.say(t, buyer, "SHOP-LONG")
	entry := strings.Join(c.replies(t, buyer), "\n") + "\n" + sheetText(t, dir, buyer)
	if !strings.Contains(entry, "Item One") || !strings.Contains(entry, "More items") {
		t.Fatalf("entry should present the first shelf sheet, got %q", entry)
	}

	HandleInbound(Inbound{From: buyer, ListID: "shelf_item-3", MessageID: "wamid.SHELF-A1"})
	if body := lastSheetBody(t, dir, buyer); !strings.Contains(body, "Added the Item Three") || !strings.Contains(body, "1 item picked so far, GH₵130 in all") {
		t.Fatalf("a tap should add the item and say so over the sheet, got %q", body)
	}
	sheet := lastSheetText(t, dir, buyer)
	if !strings.Contains(sheet, "✓ Item Three") || !strings.Contains(sheet, "Your picks") || !strings.Contains(sheet, "Done, settle them") {
		t.Fatalf("the sheet should come back with the pick ticked and Done riding, got %q", sheet)
	}
	s := sessions[sessionKey(MerchantID, buyer)]
	if s == nil || len(s.ShelfPicks) != 1 || s.ShelfPicks[0] != "item-3" {
		t.Fatalf("the pick should be held on the session, got %+v", s)
	}

	HandleInbound(Inbound{From: buyer, ListID: "shelf_item-3", MessageID: "wamid.SHELF-A2"})
	if body := lastSheetBody(t, dir, buyer); !strings.Contains(body, "Took the Item Three off your picks") {
		t.Fatalf("a second tap should take the item back off, got %q", body)
	}
	sheet = lastSheetText(t, dir, buyer)
	if strings.Contains(sheet, "Done, settle them") || strings.Contains(sheet, "✓") {
		t.Fatalf("with nothing picked there is no Done row and no tick, got %q", sheet)
	}

	HandleInbound(Inbound{From: buyer, ListID: "shelf_item-2", MessageID: "wamid.SHELF-A3"})
	HandleInbound(Inbound{From: buyer, ListID: "shelfmore_2", MessageID: "wamid.SHELF-A4"})
	if rows := lastSheetText(t, dir, buyer); !strings.Contains(rows, "Shelf 2 of 2") || !strings.Contains(rows, "Item Twelve") {
		t.Fatalf("the More tap should still open the second sheet, got %q", rows)
	}
	if body := lastSheetBody(t, dir, buyer); !strings.Contains(body, "ticked") {
		t.Fatalf("a sheet reopened over picks should say they are ticked, got %q", body)
	}
}

const shelfSettleCatalogue = `{"merchants":{"demo":{"id":"demo","name":"Pick Shop","shopCode":"PICK","ownerWhatsApp":"233550009201","dailyGiveawayAllowance":300,"deliveryFee":20,"deliveryNote":"Sent by bus to your station","products":[
{"id":"dress","name":"Denim Mini Dress","description":"A short denim dress.","listPrice":190,"floorPrice":180,"stock":2},
{"id":"bag","name":"Black Tote Bag","description":"A roomy black tote.","listPrice":80,"floorPrice":75,"stock":2},
{"id":"top","name":"Striped Crop Top","description":"A striped crop top.","listPrice":60,"floorPrice":50,"stock":2}]}}}`

func TestShelfDoneStartsTheSettling(t *testing.T) {
	owner := "233550009201"
	buyer := "233550009202"
	dir := setupChat(t, owner, shelfSettleCatalogue)
	c := &chat{dir: dir}

	c.say(t, buyer, "SHOP-PICK")
	c.replies(t, buyer)
	HandleInbound(Inbound{From: buyer, ListID: "shelf_dress", MessageID: "wamid.SHELF-B1"})
	HandleInbound(Inbound{From: buyer, ListID: "shelf_bag", MessageID: "wamid.SHELF-B2"})
	HandleInbound(Inbound{From: buyer, ListID: "shelfdone", MessageID: "wamid.SHELF-B3"})
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "one by one") || !strings.Contains(got, "Denim Mini Dress") || !strings.Contains(got, "GH₵190") {
		t.Fatalf("Done should open the settling on the first pick, got %q", got)
	}
	s := sessions[sessionKey(MerchantID, buyer)]
	if s == nil || !s.ShelfSettling || s.Product == nil || s.Product.ID != "dress" || len(s.ShelfPicks) != 1 || s.ShelfPicks[0] != "bag" {
		t.Fatalf("the session should be settling the dress with the bag queued, got %+v", s)
	}
}

func TestShelfPicksSettleIntoOneOrder(t *testing.T) {
	owner := "233550009201"
	buyer := "233550009203"
	dir := setupChat(t, owner, shelfSettleCatalogue)
	c := &chat{dir: dir}

	c.say(t, buyer, "SHOP-PICK")
	c.replies(t, buyer)
	HandleInbound(Inbound{From: buyer, ListID: "shelf_dress", MessageID: "wamid.SHELF-C1"})
	HandleInbound(Inbound{From: buyer, ListID: "shelf_bag", MessageID: "wamid.SHELF-C2"})
	HandleInbound(Inbound{From: buyer, ListID: "shelfdone", MessageID: "wamid.SHELF-C3"})
	c.replies(t, buyer)

	agreed, ask := haggleToAgreement(t, c, buyer, 185)
	if !strings.Contains(ask, "Now the next one you picked:") || !strings.Contains(ask, "Black Tote Bag") || !strings.Contains(ask, "GH₵80") {
		t.Fatalf("agreeing the first pick should park it and present the next, got %q", ask)
	}
	s := sessions[sessionKey(MerchantID, buyer)]
	if s == nil || len(s.Basket) != 1 || s.Basket[0].Product.ID != "dress" || s.Product == nil || s.Product.ID != "bag" {
		t.Fatalf("the dress should be parked with the bag in front of her, got %+v", s)
	}

	c.say(t, buyer, "80")
	got := strings.Join(c.replies(t, buyer), "\n")
	sum := agreed + 80
	if !strings.Contains(got, "yours for GH₵80") || !strings.Contains(got, "That makes your order GH₵"+moneyFigure(sum)+" in all.") || !strings.Contains(got, "How do you want to get it?") {
		t.Fatalf("agreeing the last pick should open the one fulfilment, got %q", got)
	}

	c.say(t, buyer, "Send it to Koforidua")
	if got := c.last(t, buyer); !strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("the package name should be asked, got %q", got)
	}
	c.say(t, buyer, "Efua Mensima")
	c.last(t, buyer)
	c.say(t, buyer, "no")
	c.last(t, buyer)
	c.say(t, buyer, "yes")
	c.replies(t, buyer)

	all := orders.Default.All()
	if len(all) != 1 {
		t.Fatalf("the picks should complete as one order, got %d", len(all))
	}
	order := &all[0]
	if len(order.Items) != 2 || order.Price != sum {
		t.Fatalf("order = %+v, want two items totalling %v", order, sum)
	}
	if order.Items[0].Name != "Denim Mini Dress" || order.Items[1].Name != "Black Tote Bag" {
		t.Fatalf("order items = %+v", order.Items)
	}
	if order.ProductName != "Denim Mini Dress and Black Tote Bag" {
		t.Fatalf("order name = %q", order.ProductName)
	}
}
