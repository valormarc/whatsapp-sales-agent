package agent

// Sizes on an order. A product that comes in sizes used to
// be sold with the sizes sitting in its description and the
// buyer never asked: the wrong pair shipped. Now the listing
// carries its sizes as facts, the details ask for the choice
// before the name, the answer is verified against the list,
// and the size lands on the order, the recap and the
// seller's notice. A size she labelled herself earlier in
// the chat is remembered instead of asked again.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

const sizesCatalogue = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","ownerWhatsApp":"233550002001","dailyGiveawayAllowance":300,"deliveryFee":25,"deliveryNote":"Sent by bus to your station","products":[{"id":"heels","name":"Ankle Strap Heels","description":"Pointed ankle-strap heels in mesh.","listPrice":120,"floorPrice":100,"stock":3,"sizes":["37","38","39","40","41","42"]}]}}}`

func buyHeelsToAgreement(t *testing.T, c *chat, buyer string) {
	t.Helper()
	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Ankle Strap Heels")
	c.last(t, buyer)
	_, ask := haggleToAgreement(t, c, buyer, 110)
	if !strings.Contains(ask, "How do you want to get it?") {
		t.Fatalf("agreement should open fulfilment, got %q", ask)
	}
	c.say(t, buyer, "Send it to Koforidua")
}

func TestSizeIsAskedVerifiedAndLandsOnTheOrder(t *testing.T) {
	dir := setupChat(t, "233550002001", sizesCatalogue)
	c := &chat{dir: dir}
	buyer := "233550003001"
	buyHeelsToAgreement(t, c, buyer)

	// The details open with the size, before the name.
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "What size do you take? They come in 37, 38, 39, 40, 41 and 42.") {
		t.Fatalf("the size question should open the details, got %q", got)
	}
	// An answer that is not one of the sizes is asked again.
	c.say(t, buyer, "the biggest one")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "They come in 37, 38, 39, 40, 41 and 42.") || !strings.Contains(strings.ToLower(got), "size") {
		t.Fatalf("an unlisted answer should be re-asked with the list, got %q", got)
	}
	// A listed size moves on to the name, and the whole
	// order carries it from there.
	c.say(t, buyer, "40")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("a listed size should open the name question, got %q", got)
	}
	c.say(t, buyer, "Akosua Mensah")
	c.say(t, buyer, "no")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Size: 40") {
		t.Fatalf("the recap should read the size back, got %q", got)
	}
	c.say(t, buyer, "yes")
	c.replies(t, buyer)
	book := ordersOnDisk(t, dir)
	if len(book) != 1 {
		t.Fatalf("orders = %v, want 1", book)
	}
	if book[0]["size"] != "40" {
		t.Fatalf("the order should carry the chosen size, got %v", book[0])
	}
	notice := strings.Join(c.replies(t, "233550002001"), "\n")
	if !strings.Contains(notice, "Size: 40.") {
		t.Fatalf("the seller's notice should carry the size, got %q", notice)
	}
	_ = orders.Default
}

func TestSizeStatedInHerOwnWordsIsNotAskedAgain(t *testing.T) {
	setupChat(t, "233550002001", sizesCatalogue)
	c := &chat{dir: ""}
	buyer := "233550003002"
	// Rebuild the chat against the same world setupChat made.
	dir := setupChat(t, "233550002001", sizesCatalogue)
	c = &chat{dir: dir}
	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Ankle Strap Heels")
	c.last(t, buyer)
	// She labels her size while she haggles; the details
	// must hold it, not ask again.
	c.say(t, buyer, "110, and I take size 38")
	got := strings.Join(c.replies(t, buyer), "\n")
	figs := figuresIn(got)
	if len(figs) == 0 {
		t.Fatalf("her offer should draw a counter or an agreement, got %q", got)
	}
	if !strings.Contains(got, "yours for") {
		c.say(t, buyer, moneyFigure(figs[len(figs)-1]))
		got = strings.Join(c.replies(t, buyer), "\n")
	}
	if !strings.Contains(got, "How do you want to get it?") {
		t.Fatalf("meeting the counter should open fulfilment, got %q", got)
	}
	c.say(t, buyer, "Send it to Koforidua")
	got = strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "What size do you take?") {
		t.Fatalf("a stated size must not be asked again, got %q", got)
	}
	if !strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("the details should open at the name, got %q", got)
	}
	c.say(t, buyer, "Akosua Mensah")
	c.say(t, buyer, "no")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Size: 38") {
		t.Fatalf("the recap should read her stated size back, got %q", got)
	}
}

func TestSizeReadingIsStrict(t *testing.T) {
	p := &catalogue.Product{Sizes: []string{"37", "38", "40", "UK 8"}}
	if got := statedSize(p, "I will pay 40 for it"); got != "" {
		t.Fatalf("a bare offer figure must never read as a size, got %q", got)
	}
	if got := statedSize(p, "my size is 40"); got != "40" {
		t.Fatalf("a labelled size should read, got %q", got)
	}
	if got := matchListedSize(p, "UK 8 please"); got != "UK 8" {
		t.Fatalf("a multi-word size should match whole, got %q", got)
	}
	if got := matchListedSize(p, "the 400 one"); got != "" {
		t.Fatalf("a longer number must not match a size inside it, got %q", got)
	}
}
