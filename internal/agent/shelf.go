package agent

// The shelf as a sheet: WhatsApp's list message slides a
// panel up from the bottom of the chat, and a stall's goods
// belong on it exactly the way they sit on a real shelf —
// the buyer looks, taps the one she wants, and it is put in
// front of her. The sheet is an addition, never a
// replacement: a buyer who types the item's name is
// understood exactly as before, and a tap is read as the
// very pick she would have made in words (the same
// philosophy as the fulfilment taps).
//
// Platform limits shape the composition: a sheet holds ten
// rows in all, a row's title is 24 characters and its
// description 72. A shelf longer than a sheet is paged —
// nine items and a More row that opens the next sheet,
// Shelf 2, Shelf 3 — so no shelf is ever cut off silently.
// A product's identity never rides in the visible title,
// which may be shortened; it rides in the row's id, so a
// tap resolves to the exact product however its name was
// trimmed for display.

import (
	"strconv"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// Row id spellings on a shelf sheet: a product pick, and
// the More row that opens the next sheet.
const (
	shelfRowPrefix  = "shelf_"
	shelfMorePrefix = "shelfmore_"
)

// shelfItems is what the sheet can show: the shop's
// products with stock in hand, in shelf order. A sold-out
// item has no row — the sheet is for choosing, and there is
// nothing to choose in an empty space (the text list,
// which still names everything, remains the fallback when
// nothing at all is in stock).
func shelfItems(shop *catalogue.Merchant) []catalogue.Product {
	var items []catalogue.Product
	for _, p := range catalogue.ListProducts(shop.ID) {
		if p.Stock > 0 {
			items = append(items, p)
		}
	}
	return items
}

// shelfPageCount is how many sheets a shelf of n items
// takes: one when they fit the ten rows, else pages of
// nine items plus the More row.
func shelfPageCount(n int) int {
	if n <= 10 {
		return 1
	}
	return (n + 8) / 9
}

// shelfSections composes one page of the shelf as a list
// section: each item a row (price in its description), and
// a More row when a further page exists.
func shelfSections(shop *catalogue.Merchant, page int) []whatsapp.ListSection {
	items := shelfItems(shop)
	pages := shelfPageCount(len(items))
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	var pageItems []catalogue.Product
	if pages == 1 {
		pageItems = items
	} else {
		start := (page - 1) * 9
		end := start + 9
		if end > len(items) {
			end = len(items)
		}
		pageItems = items[start:end]
	}
	rows := make([]whatsapp.ListRow, 0, len(pageItems)+1)
	for _, p := range pageItems {
		rows = append(rows, whatsapp.ListRow{
			ID:          shelfRowPrefix + p.ID,
			Title:       trimToRunes(p.Name, 24),
			Description: shelfRowDescription(p),
		})
	}
	if page < pages {
		rows = append(rows, whatsapp.ListRow{
			ID:          shelfMorePrefix + strconv.Itoa(page+1),
			Title:       "More items",
			Description: "See Shelf " + strconv.Itoa(page+1) + " of " + strconv.Itoa(pages),
		})
	}
	title := "Shelf"
	if pages > 1 {
		title = "Shelf " + strconv.Itoa(page) + " of " + strconv.Itoa(pages)
	}
	return []whatsapp.ListSection{{Title: title, Rows: rows}}
}

// shelfRowDescription is a row's second line: the price
// first (that is what a browser of a shelf is scanning
// for), a fixed price marked, then as much of the
// product's own word as the 72 characters leave room for.
func shelfRowDescription(p catalogue.Product) string {
	d := money.GHS(p.ListPrice)
	if p.Fixed {
		d += " · fixed price"
	}
	if strings.TrimSpace(p.Description) != "" {
		d += " · " + p.Description
	}
	return trimToRunes(d, 72)
}

// trimToRunes shortens s to at most n runes, preferring to
// break at the last word boundary inside the limit so a
// name is cut between words, never inside one, when a
// boundary is there to take.
func trimToRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := string(r[:n])
	if i := strings.LastIndex(cut, " "); i >= n/3 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;·-")
}

// sendShelfSheet puts one page of the shop's shelf in
// front of the buyer as a slide-up sheet.
func sendShelfSheet(from string, shop *catalogue.Merchant, page int) {
	body := "This is the shelf at " + shop.Name + ". Tap any item to see it properly: its photos, the full word on it, and its price."
	whatsapp.SendList(from, body, "See the shelf", shelfSections(shop, page))
}

// presentShelf shows a buyer the shop's shelf: the sheet
// when anything is in stock, the plain text list when the
// shelf is stocked out entirely (the sheet has nothing to
// choose from then, and the text still names the goods).
func presentShelf(from string, shop *catalogue.Merchant) {
	if len(shelfItems(shop)) == 0 {
		whatsapp.SendText(from, productListText(shop.ID))
		return
	}
	sendShelfSheet(from, shop, 1)
}

// routeShelfTap reads a tap on a shelf sheet in the shop
// the buyer is standing in. A More tap opens the next
// sheet. A product tap puts the product in front of her
// exactly as naming it would — remembered as what she is
// looking at, its photos when it has them, its description
// and price, and the invitation to make an offer — so
// everything she says next (a figure, "I'll buy") lands on
// it the way it lands on a named item. A tap on a product
// the shelf no longer holds reopens the shelf at its
// first page rather than answering silence.
func routeShelfTap(from, listID string) {
	shopID := bindings[from]
	if shopID == "" {
		return
	}
	shop := catalogue.GetMerchant(shopID)
	if shop == nil {
		return
	}
	if rest, ok := strings.CutPrefix(listID, shelfMorePrefix); ok {
		n, err := strconv.Atoi(rest)
		if err != nil || n < 1 {
			n = 1
		}
		sendShelfSheet(from, shop, n)
		return
	}
	pid, ok := strings.CutPrefix(listID, shelfRowPrefix)
	if !ok {
		return
	}
	var picked *catalogue.Product
	for _, p := range catalogue.ListProducts(shopID) {
		if p.ID == pid {
			cp := p
			picked = &cp
			break
		}
	}
	if picked == nil {
		sendShelfSheet(from, shop, 1)
		return
	}
	s, ok := sessions[sessionKey(shopID, from)]
	if !ok {
		s = &Session{Stage: "browsing", MerchantID: shopID}
		sessions[sessionKey(shopID, from)] = s
	}
	s.LastViewed = picked
	if s.Stage == "agreed" {
		s.Stage = "browsing"
	}
	sendProductPhotos(from, picked)
	sendProductView(from, picked, " Want to make an offer?")
}
