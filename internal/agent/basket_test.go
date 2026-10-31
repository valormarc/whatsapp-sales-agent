package agent

// More than one item in an order. Each item is settled on
// its own terms, then the basket completes as one order:
// one fulfilment, one set of details, one link for the
// combined total. Adding is confirmed before anything is
// parked, a declined add leaves the order untouched, and
// walking away from one item never throws the agreed ones
// away with it.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

const basketCatalogue = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","ownerWhatsApp":"233550002001","dailyGiveawayAllowance":300,"deliveryFee":25,"deliveryNote":"Sent by bus to your station","products":[{"id":"heels","name":"Ankle Strap Heels","description":"Pointed ankle-strap heels in mesh.","listPrice":120,"floorPrice":100,"stock":3,"sizes":["37","38","39","40","41","42"]},{"id":"bag","name":"Leather Bag","description":"A structured leather handbag.","listPrice":850,"floorPrice":800,"stock":2}]}}}`

const basketOwner = "233550002001"

func agreeHeels(t *testing.T, c *chat, buyer string) float64 {
	t.Helper()
	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Ankle Strap Heels")
	c.last(t, buyer)
	agreed, ask := haggleToAgreement(t, c, buyer, 110)
	if !strings.Contains(ask, "How do you want to get it?") {
		t.Fatalf("agreement should open fulfilment, got %q", ask)
	}
	return agreed
}

func finishBasketDetails(t *testing.T, c *chat, buyer string) {
	t.Helper()
	c.say(t, buyer, "Send it to Koforidua")
	if got := c.last(t, buyer); !strings.Contains(got, "What size for the Ankle Strap Heels?") {
		t.Fatalf("the sized item should be asked for by name, got %q", got)
	}
	c.say(t, buyer, "40")
	if got := c.last(t, buyer); !strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("after the size, the name, got %q", got)
	}
	c.say(t, buyer, "Efua Mensima")
	c.last(t, buyer)
	c.say(t, buyer, "no")
	c.last(t, buyer)
}

func TestBasketTwoItemsOneOrder(t *testing.T) {
	dir := setupChat(t, basketOwner, basketCatalogue)
	c := &chat{dir: dir}
	buyer := "233550004001"

	agreed := agreeHeels(t, c, buyer)

	c.say(t, buyer, "add the Leather Bag")
	got := c.last(t, buyer)
	if !strings.Contains(got, "Add the Leather Bag at GH₵850") || !strings.Contains(got, "in all, before delivery") {
		t.Fatalf("the add should be confirmed with the running total, got %q", got)
	}
	c.say(t, buyer, "yes")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Added.") || !strings.Contains(got, "Leather Bag - GH₵850") {
		t.Fatalf("a yes should park the heels and show the bag, got %q", got)
	}
	c.say(t, buyer, "850")
	got = strings.Join(c.replies(t, buyer), "\n")
	sum := agreed + 850
	if !strings.Contains(got, "yours for GH₵850") || !strings.Contains(got, "That makes your order GH₵"+moneyFigure(sum)+" in all.") {
		t.Fatalf("the second deal should carry the order total, got %q", got)
	}

	finishBasketDetails(t, c, buyer)
	c.say(t, buyer, "yes")
	buyerGot := strings.Join(c.replies(t, buyer), "\n")

	all := orders.Default.All()
	if len(all) != 1 {
		t.Fatalf("the basket sale should record one order, got %d", len(all))
	}
	order := &all[0]
	if len(order.Items) != 2 || order.Price != sum {
		t.Fatalf("order = %+v, want two items totalling %v", order, sum)
	}
	if order.Items[0].Name != "Ankle Strap Heels" || order.Items[0].Size != "40" || order.Items[1].Name != "Leather Bag" {
		t.Fatalf("order items = %+v", order.Items)
	}
	if order.ProductName != "Ankle Strap Heels and Leather Bag" {
		t.Fatalf("order name = %q", order.ProductName)
	}
	notes := strings.Join(c.replies(t, basketOwner), "\n")
	if !strings.Contains(notes, "Item: Ankle Strap Heels, size 40 - GH₵111") || !strings.Contains(notes, "Item: Leather Bag - GH₵850") ||
		!strings.Contains(notes, "Agreed price: GH₵"+moneyFigure(sum)) {
		t.Fatalf("the seller's notice should list both items with the size, got %q", notes)
	}
	if !strings.Contains(buyerGot, "Ankle Strap Heels - GH₵111") || !strings.Contains(buyerGot, "Leather Bag - GH₵850") || !strings.Contains(buyerGot, "Total: GH₵961") {
		t.Fatalf("the buyer should hear her items back, got %q", buyerGot)
	}
}

func TestBasketAddDeclinedKeepsSingleOrder(t *testing.T) {
	dir := setupChat(t, basketOwner, basketCatalogue)
	c := &chat{dir: dir}
	buyer := "233550004002"

	agreed := agreeHeels(t, c, buyer)
	c.say(t, buyer, "add the Leather Bag")
	c.last(t, buyer)
	c.say(t, buyer, "no")
	if got := c.last(t, buyer); !strings.Contains(strings.ToLower(got), "how do you want to get it") {
		t.Fatalf("a no should return to the open question, got %q", got)
	}
	c.say(t, buyer, "Send it to Koforidua")
	if got := c.last(t, buyer); !strings.Contains(got, "What size do you take?") {
		t.Fatalf("a lone sized item keeps its plain question, got %q", got)
	}
	c.say(t, buyer, "40")
	c.last(t, buyer)
	c.say(t, buyer, "Efua Mensima")
	c.last(t, buyer)
	c.say(t, buyer, "no")
	c.last(t, buyer)
	c.say(t, buyer, "yes")
	c.replies(t, buyer)

	all := orders.Default.All()
	if len(all) != 1 || len(all[0].Items) != 0 || all[0].Price != agreed {
		t.Fatalf("orders = %+v, want the single heels order at %v", all, agreed)
	}
}

func TestBasketWalkAwayKeepsAgreedItems(t *testing.T) {
	dir := setupChat(t, basketOwner, basketCatalogue)
	c := &chat{dir: dir}
	buyer := "233550004003"

	agreeHeels(t, c, buyer)
	c.say(t, buyer, "add the Leather Bag")
	c.last(t, buyer)
	c.say(t, buyer, "yes")
	c.replies(t, buyer)
	// Haggle the bag down to its final stand, then offer
	// below it: the bag is let go, the heels are not.
	for i := 0; i < 5; i++ {
		c.say(t, buyer, "100")
		c.replies(t, buyer)
	}
	c.say(t, buyer, "50")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "let that one go") {
		t.Fatalf("the bag should be let go, got %q", got)
	}
	if !strings.Contains(got, "How do you want to get it?") {
		t.Fatalf("the agreed heels should reopen their fulfilment, got %q", got)
	}
	c.say(t, buyer, "Send it to Koforidua")
	if got := c.last(t, buyer); !strings.Contains(got, "What size do you take?") {
		t.Fatalf("the rescued sale is a single-item sale again, got %q", got)
	}
	c.say(t, buyer, "40")
	c.last(t, buyer)
	c.say(t, buyer, "Efua Mensima")
	c.last(t, buyer)
	c.say(t, buyer, "no")
	c.last(t, buyer)
	c.say(t, buyer, "yes")
	c.replies(t, buyer)

	all := orders.Default.All()
	if len(all) != 1 || len(all[0].Items) != 0 || all[0].ProductName != "Ankle Strap Heels" {
		t.Fatalf("orders = %+v, want the heels alone", all)
	}
}
