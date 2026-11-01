package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// The joggers caption, verbatim from the live number: her
// first line is a statement about the goods, and the shelf
// must carry the name inside it, never her sentence.
func TestAdvertNameStripsTheAnnouncement(t *testing.T) {
	draft, _, ok := parsePastedAdvert("Please joggers are available 120gh\nOnly few in stock please\nVery heavy and thick")
	if !ok {
		t.Fatalf("the caption shape must read as an advert")
	}
	if draft.Name != "joggers" {
		t.Fatalf("advert name = %q, want the goods' name inside the announcement", draft.Name)
	}
	if draft.ListPrice != 120 {
		t.Fatalf("advert price = %v, want 120", draft.ListPrice)
	}
}

func TestAdvertNameKeepsARealName(t *testing.T) {
	draft, _, ok := parsePastedAdvert("Ankara Print Dress\nGH₵220\nBold wax print, fully lined")
	if !ok || draft.Name != "Ankara Print Dress" {
		t.Fatalf("advert name = %q ok=%v, want the name untouched", draft.Name, ok)
	}
}

func TestCaptionNameStripsTheAnnouncement(t *testing.T) {
	d := throwDraftFromCaption("Joggers are available")
	if d.Name != "Joggers" {
		t.Fatalf("caption name = %q, want the name inside the announcement, in her casing", d.Name)
	}
	d = throwDraftFromCaption("Leather bag")
	if d.Name != "Leather bag" {
		t.Fatalf("caption name = %q, want the phrase untouched", d.Name)
	}
}

func TestStockCountReads(t *testing.T) {
	for text, want := range map[string]int{
		"I have 3 joggers in stock": 3,
		"3 pieces in stock":         3,
		"stock: 5":                  5,
		"Stock is 2":                2,
	} {
		if n, ok := parseStockCount(text); !ok || n != want {
			t.Fatalf("parseStockCount(%q) = %d,%v, want %d", text, n, ok, want)
		}
	}
	if _, ok := parseStockCount("Only few in stock please"); ok {
		t.Fatalf("an uncounted phrase must read no count")
	}
}

// A stock statement with no walk open lands on the named
// product, in her own words, with the platform's own
// confirmation — never a lecture about what cannot be
// tracked.
func TestMerchantStockStatementAppliesToTheShelf(t *testing.T) {
	merchant := "233550001801"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "I have 7 handmade beaded bag in stock")
	if got := c.last(t, merchant); !strings.Contains(got, "now has 7 in stock") {
		t.Fatalf("stock statement reply = %q, want the applied confirmation", got)
	}
	p := catalogue.FindByName(MerchantID, "Handmade Beaded Bag")
	if p == nil || p.Stock != 7 {
		t.Fatalf("bag stock = %+v, want 7", p)
	}
}

// A message carrying more than the statement keeps its
// own road: the stock helper never swallows an errand.
func TestMerchantStockStatementLeavesCompoundsAlone(t *testing.T) {
	merchant := "233550001802"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "I have 7 handmade beaded bag in stock, and cancel that")
	p := catalogue.FindByName(MerchantID, "Handmade Beaded Bag")
	if p == nil || p.Stock == 7 {
		t.Fatalf("a compound message must not be applied as a stock statement, stock = %+v", p)
	}
}

// A stated count in the listing's own words rides the
// walk to the save: the product goes live with her count,
// not the assumed one.
func TestAdvertStockRidesTheWalkToTheSave(t *testing.T) {
	merchant := "233550001803"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.last(t, merchant)
	c.say(t, merchant, "Canvas Tote\n120gh\nStrong canvas tote\n3 in stock")
	c.last(t, merchant)
	c.say(t, merchant, "100")
	finishWalkToShelf(t, c, merchant)
	p := catalogue.FindByName(MerchantID, "Canvas Tote")
	if p == nil {
		t.Fatalf("the advert listing never saved")
	}
	if p.Stock != 3 {
		t.Fatalf("saved stock = %d, want her stated 3", p.Stock)
	}
}

const twoGoodsOneGone = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":0},{"id":"leather-sandals","name":"Leather Sandals","description":"Brown handmade leather sandals.","listPrice":150,"floorPrice":120,"stock":2}]}}}`

// The sold out answer speaks its line and presents the
// shelf as the sheet, the way every shelf presentation
// now goes: no fenced text dump of the goods.
func TestSoldOutAnswerPresentsTheSheet(t *testing.T) {
	merchant := "233550001804"
	dir := setupChat(t, merchant, twoGoodsOneGone)
	c := &chat{dir: dir}

	buyer := "233550001814"
	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "I want the handmade beaded bag")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "sold out just now") {
		t.Fatalf("sold out reply = %q", got)
	}
	if strings.Contains(got, "```") {
		t.Fatalf("the shelf went out as a text dump: %q", got)
	}
	if sheet := lastSheetText(t, dir, buyer); sheet == "" {
		t.Fatalf("no shelf sheet followed the sold out line")
	}
}

// A direct question about stock tracking is answered by
// the platform with its own truth, in code: the drafted
// answer to this question invented a limit the platform
// does not have, twice over, against the facts.
func TestMerchantStockQuestionAnsweredTruthfully(t *testing.T) {
	merchant := "233550001805"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "can you track my stock?")
	got := c.last(t, merchant)
	if !strings.Contains(got, "Yes, I track your stock") || strings.Contains(got, "cannot track your stock") {
		t.Fatalf("stock question answer = %q, want the platform's truth", got)
	}
	if !strings.Contains(got, "outside this chat") {
		t.Fatalf("stock question answer = %q, want the one true limit named", got)
	}
}
