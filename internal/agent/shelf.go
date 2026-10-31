package agent

// The shelf as a sheet: WhatsApp's list message slides a
// panel up from the bottom of the chat, and a stall's goods
// belong on it exactly the way they sit on a real shelf.
// A sheet reports one tap at a time — that is the platform's
// shape, there is no ticking several boxes at once — so
// picking many is built the way a buyer picks at a real
// stall: she taps an item and it goes into her hand, the
// sheet comes back so she can tap the next one, and a Done
// row sits at the top of the sheet whenever she is holding
// anything. Tapping a picked item again takes it back off.
// Done starts the settling: the picks are put in front of
// her one by one, each with its photos, its price and its
// own haggle, and they complete as one order with one
// payment, the same basket machinery a spoken "add the bag
// too" has always used. Typing still works exactly as
// before; the sheet is an addition, never a replacement.
//
// Platform limits shape the composition: a sheet holds ten
// rows in all, a row's title is 24 characters and its
// description 72. A shelf longer than a sheet is paged —
// nine items and a More row that opens the next sheet,
// Shelf 2, Shelf 3 — and the Done row takes one of the ten
// rows while she is holding picks, so the pages narrow by
// one to make room for it. No shelf is ever cut off
// silently. A product's identity never rides in the visible
// title, which may be shortened; it rides in the row's id,
// so a tap resolves to the exact product however its name
// was trimmed for display.

import (
	"strconv"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// Row id spellings on a shelf sheet: a product pick, the
// More row that opens the next sheet, and the Done row
// that starts the settling.
const (
	shelfRowPrefix  = "shelf_"
	shelfMorePrefix = "shelfmore_"
	shelfDoneID     = "shelfdone"
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

// shelfLayout works out how a shelf of n items pages,
// given the fixed rows the sheet must also carry: the
// Done row while she holds picks, and a More row on every
// page but the last. Ten rows is the whole sheet.
func shelfLayout(n int, showDone bool) (pages, perPage int) {
	extra := 0
	if showDone {
		extra = 1
	}
	if n+extra <= 10 {
		return 1, n
	}
	perPage = 9 - extra
	return (n + perPage - 1) / perPage, perPage
}

// pickedIDs is the set of products this buyer has already
// committed to in some way: the picks still in her hand,
// the items parked in her order, and — while the settling
// runs — the one in front of her now. Those rows wear a
// tick on the sheet.
func pickedIDs(s *Session) map[string]bool {
	set := map[string]bool{}
	if s == nil {
		return set
	}
	for _, id := range s.ShelfPicks {
		set[id] = true
	}
	for _, it := range s.Basket {
		if it.Product != nil {
			set[it.Product.ID] = true
		}
	}
	if s.ShelfSettling && s.Product != nil {
		set[s.Product.ID] = true
	}
	return set
}

// picksFigure is what her picks come to at list prices:
// how many of them still resolve to goods on the shelf,
// and their total. A pick the shop can no longer supply
// counts for nothing: the figure she is shown only ever
// counts goods that can still be settled for her.
func picksFigure(s *Session, shopID string) (count int, total float64) {
	if s == nil {
		return 0, 0
	}
	for _, id := range s.ShelfPicks {
		for _, p := range catalogue.ListProducts(shopID) {
			if p.ID == id && p.Stock > 0 {
				count++
				total += p.ListPrice
				break
			}
		}
	}
	return count, total
}

// shelfSections composes one page of the shelf for this
// buyer: her Done section first while she holds picks,
// then the items — each a row with its price in its
// description and a tick on the ones she has picked — and
// a More row when a further page exists.
func shelfSections(shop *catalogue.Merchant, page int, s *Session) []whatsapp.ListSection {
	items := shelfItems(shop)
	showDone := s != nil && len(s.ShelfPicks) > 0 && !s.ShelfSettling
	pages, perPage := shelfLayout(len(items), showDone)
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
		start := (page - 1) * perPage
		end := start + perPage
		if end > len(items) {
			end = len(items)
		}
		pageItems = items[start:end]
	}
	picked := pickedIDs(s)
	rows := make([]whatsapp.ListRow, 0, len(pageItems)+1)
	for _, p := range pageItems {
		title := trimToRunes(p.Name, 24)
		if picked[p.ID] {
			title = "✓ " + trimToRunes(p.Name, 22)
		}
		rows = append(rows, whatsapp.ListRow{
			ID:          shelfRowPrefix + p.ID,
			Title:       title,
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
	sections := []whatsapp.ListSection{}
	if showDone {
		count, total := picksFigure(s, shop.ID)
		sections = append(sections, whatsapp.ListSection{
			Title: "Your picks",
			Rows: []whatsapp.ListRow{{
				ID:          shelfDoneID,
				Title:       "Done, settle them",
				Description: strconv.Itoa(count) + pluralItems(count) + " · " + money.GHS(total) + " in all",
			}},
		})
	}
	return append(sections, whatsapp.ListSection{Title: title, Rows: rows})
}

func pluralItems(n int) string {
	if n == 1 {
		return " item"
	}
	return " items"
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

// shelfSession is the buyer's session in this shop, when
// she has one: the sheet is composed from it (her ticks,
// her Done row) and her place on it is remembered there.
func shelfSession(from string, shop *catalogue.Merchant) *Session {
	if bindings.get(from) != shop.ID {
		return nil
	}
	return sessions.get(sessionKey(shop.ID, from))
}

// sendShelfSheet puts one page of the shop's shelf in
// front of the buyer as a slide-up sheet, with a body that
// matches where she is: fresh to the shelf, or already
// holding picks.
func sendShelfSheet(from string, shop *catalogue.Merchant, page int) {
	s := shelfSession(from, shop)
	body := "This is the shelf at " + shop.Name + ". Tap an item to pick it, and pick as many as you like. When you are ready, tap Done on the sheet and we will settle them one by one."
	if s != nil && len(s.ShelfPicks) > 0 && !s.ShelfSettling {
		body = "Your picks are ticked on the shelf. Tap an item to add it, tap a ticked one to take it off, or tap Done to settle them."
	}
	sendShelfSheetBody(from, shop, page, body)
}

// sendShelfSheetBody sends the sheet with the given words
// over it — a tap's answer rides on the sheet it reopens.
func sendShelfSheetBody(from string, shop *catalogue.Merchant, page int, body string) {
	s := shelfSession(from, shop)
	if s != nil {
		_, pages := shelfLayout(len(shelfItems(shop)), s != nil && len(s.ShelfPicks) > 0 && !s.ShelfSettling)
		if page < 1 {
			page = 1
		}
		if page > pages {
			page = pages
		}
		s.ShelfPage = page
		sessions.put(sessionKey(shop.ID, from), s)
	}
	whatsapp.SendList(from, body, "See the shelf", shelfSections(shop, page, s))
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

// presentShelfPick puts a settled pick in front of her the
// way naming it would: its photos when it has them, its
// description and price, and the invitation to make an
// offer.
func presentShelfPick(from, shopID string, p *catalogue.Product) {
	sendProductPhotos(from, shopID, p)
	sendProductView(from, p, " Want to make an offer?")
}

// takeNextShelfPick moves the first of her remaining
// picks that the shelf still holds into the session as
// the item in front of her, reset the way an agreed add
// leaves a session when it puts the next item up (see
// basket.go), and returns it. Picks the shelf no longer
// holds are let go on the way. It returns nil when
// nothing she picked can still be settled, and leaves the
// settling state cleared then, so the caller can fall
// back to whatever it was doing.
func takeNextShelfPick(s *Session, shopID string) *catalogue.Product {
	for len(s.ShelfPicks) > 0 {
		id := s.ShelfPicks[0]
		s.ShelfPicks = s.ShelfPicks[1:]
		var found *catalogue.Product
		for _, p := range catalogue.ListProducts(shopID) {
			if p.ID == id && p.Stock > 0 {
				cp := p
				found = &cp
				break
			}
		}
		if found == nil {
			continue
		}
		s.Product = found
		s.Ask = found.ListPrice
		s.AgreedPrice = 0
		s.Qty, s.Unit = 0, ""
		s.Round, s.Floor, s.StepScale = 0, 0, 0
		s.PendingClose = false
		s.PendingHandOffer = 0
		s.FulfilStep = ""
		s.PendingFulfil = fulfilment{}
		s.Stage = "browsing"
		s.LastViewed = found
		s.ShelfSettling = true
		return found
	}
	s.ShelfSettling = false
	return nil
}

// serveShelfDone reads the Done tap: the settling starts.
// The first pick goes in front of her with its photos and
// its price; the rest wait in her picks, and every price
// agreement from here parks its item and brings the next
// one up (see beginFulfilment) until the last one, whose
// agreement opens the one fulfilment for the whole order.
func serveShelfDone(from string, shop *catalogue.Merchant) {
	s := shelfSession(from, shop)
	if s == nil || len(s.ShelfPicks) == 0 {
		sendShelfSheet(from, shop, 1)
		return
	}
	count, _ := picksFigure(s, shop.ID)
	next := takeNextShelfPick(s, shop.ID)
	sessions.put(sessionKey(shop.ID, from), s)
	if next == nil {
		sendShelfSheet(from, shop, 1)
		return
	}
	if count > 1 {
		whatsapp.SendText(from, "Good. Let us settle them one by one, starting with this one.")
	}
	presentShelfPick(from, shop.ID, next)
}

// routeShelfTap reads a tap on a shelf sheet in the shop
// the buyer is standing in. A More tap opens the next
// sheet. A Done tap starts the settling. A product tap
// while the settling runs is a look, read exactly as
// before. A product tap while she is still picking adds
// the item to her picks — or takes it back off, when it
// was already picked — and the sheet returns at the page
// she tapped from, her picks ticked, with the state of
// her hand in the words over it.
func routeShelfTap(from, listID string) {
	shopID := bindings.get(from)
	if shopID == "" {
		return
	}
	shop := catalogue.GetMerchant(shopID)
	if shop == nil {
		return
	}
	// Her session stands from the first tap of any kind, so
	// the sheet can remember the page she is looking at and
	// her picks have somewhere to live.
	if _, ok := sessions.lookup(sessionKey(shopID, from)); !ok {
		sessions.put(sessionKey(shopID, from), &Session{Stage: "browsing", MerchantID: shopID})
	}
	if listID == shelfDoneID {
		serveShelfDone(from, shop)
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
	s := sessions.get(sessionKey(shopID, from))
	page := s.ShelfPage
	if page < 1 {
		page = 1
	}
	if picked.AvailableUnits() <= 0 {
		// The availability check stands before the branch
		// split (the seventh audit): the settling branch
		// presented a sold-out tap as available because
		// its stock test sat below the branch's return.
		// Every branch now answers a sold-out tap the
		// same way, with the current sheet.
		sendShelfSheet(from, shop, page)
		return
	}
	s.LastViewed = picked
	if s.ShelfSettling {
		// Mid-settling a tap is a look at the goods, the
		// way it always was: the settling itself is not
		// disturbed by her glancing at another item.
		if s.Stage == "agreed" {
			s.Stage = "browsing"
		}
		sessions.put(sessionKey(shopID, from), s)
		presentShelfPick(from, shopID, picked)
		return
	}
	for i, id := range s.ShelfPicks {
		if id == pid {
			s.ShelfPicks = append(s.ShelfPicks[:i], s.ShelfPicks[i+1:]...)
			sessions.put(sessionKey(shopID, from), s)
			count, total := picksFigure(s, shopID)
			body := "Took the " + picked.Name + " off your picks."
			if count > 0 {
				body += " You have " + strconv.Itoa(count) + pluralItems(count) + " picked so far, " + money.GHS(total) + " in all. Tap Done on the sheet when you are ready."
			} else {
				body += " Tap any item to pick it."
			}
			sendShelfSheetBody(from, shop, page, body)
			return
		}
	}
	if inBasket(s, picked) {
		sendShelfSheetBody(from, shop, page, "The "+picked.Name+" is already in this order. Tap Done on the sheet when you are ready, or tap more items to pick them.")
		return
	}
	s.ShelfPicks = append(s.ShelfPicks, pid)
	sessions.put(sessionKey(shopID, from), s)
	count, total := picksFigure(s, shopID)
	body := "Added the " + picked.Name + ". You have " + strconv.Itoa(count) + pluralItems(count) + " picked so far, " + money.GHS(total) + " in all. Tap more items to add them, or tap Done on the sheet and we will settle them one by one."
	sendShelfSheetBody(from, shop, page, body)
}
