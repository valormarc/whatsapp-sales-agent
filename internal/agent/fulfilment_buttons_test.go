package agent

import (
	"strings"
	"testing"
)

// The fulfilment choice as buttons, and the package details that
// follow it in words: the name on the package, and a message for
// the seller. Buttons carry the choice only; the details are
// collected in her own words, ride on the order, and reach the
// merchant in her notice.

func tap(t *testing.T, from, buttonID string) {
	t.Helper()
	HandleInbound(Inbound{From: from, ButtonID: buttonID})
}

// agreeBagAtList walks a buyer in Efua's shop to the fulfilment
// ask for the beaded bag at its list price.
func agreeBagAtList(t *testing.T, c *chat, buyer string) {
	t.Helper()
	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Handmade Beaded Bag")
	c.last(t, buyer)
	c.say(t, buyer, "850")
	c.replies(t, buyer)
}

func TestFulfilmentChoiceArrivesAsButtons(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550009001"
	agreeBagAtList(t, c, buyer)

	recs := doorRecords(t, dir, buyer)
	shown, titles := buttonsShown(recs)
	if !shown {
		t.Fatalf("the fulfilment choice should arrive as buttons, got %+v", recs)
	}
	want := []string{"Delivery", "Pick up", "Meet up"}
	if len(titles) != len(want) {
		t.Fatalf("fulfilment buttons = %v, want %v", titles, want)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Fatalf("fulfilment buttons = %v, want %v", titles, want)
		}
	}
	last := recs[len(recs)-1]
	if !strings.Contains(last.Body, "How do you want to get it?") {
		t.Fatalf("buttons body = %q, want the fulfilment question in words", last.Body)
	}
}

func TestTapDeliveryThenDetailsThenSale(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550009002"
	agreeBagAtList(t, c, buyer)

	tap(t, buyer, "ff_delivery")
	if got := c.last(t, buyer); got != "Where should it go?" {
		t.Fatalf("delivery tap = %q, want the place question", got)
	}
	c.say(t, buyer, "Osu")
	if got := c.last(t, buyer); !strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("after the place = %q, want the package name question", got)
	}
	c.say(t, buyer, "Akosua Mensah")
	if got := c.last(t, buyer); !strings.Contains(got, "Any message for the seller") {
		t.Fatalf("after the name = %q, want the message question", got)
	}
	c.say(t, buyer, "Please call when the rider leaves")
	c.say(t, buyer, "yes")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "The package will carry the name Akosua Mensah.") ||
		!strings.Contains(got, "Your message for the seller is on the order.") ||
		!strings.Contains(got, "Pay here") {
		t.Fatalf("completion = %q", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 {
		t.Fatalf("orders = %v, want one", orders)
	}
	o := orders[0]
	if o["fulfilment"] != "delivery" || o["location"] != "Osu" ||
		o["packageName"] != "Akosua Mensah" || o["note"] != "Please call when the rider leaves" {
		t.Fatalf("order = %v, want delivery to Osu, package Akosua Mensah, with her note", o)
	}
	notes := strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(notes, "Package name: Akosua Mensah") ||
		!strings.Contains(notes, "Buyer's message: Please call when the rider leaves") {
		t.Fatalf("merchant notice = %q, want the package name and her message", notes)
	}
}

func TestTapPickupSplitsThenSomebody(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550009003"
	agreeBagAtList(t, c, buyer)

	tap(t, buyer, "ff_pickup")
	recs := doorRecords(t, dir, buyer)
	_, titles := buttonsShown(recs[len(recs)-1:])
	if len(titles) != 2 || titles[0] != "I will come myself" || titles[1] != "Somebody will come" {
		t.Fatalf("pickup split buttons = %v", titles)
	}
	tap(t, buyer, "ff_other")
	if got := c.last(t, buyer); got != "Who is coming for it?" {
		t.Fatalf("somebody tap = %q, want the who question", got)
	}
	// The collector's name is the package name, so the name
	// question is skipped and the message question comes next.
	c.say(t, buyer, "Kwame")
	if got := c.last(t, buyer); !strings.Contains(got, "Any message for the seller") {
		t.Fatalf("after the collector = %q, want the message question, not the name question", got)
	}
	c.say(t, buyer, "no")
	c.say(t, buyer, "yes")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Pay here") {
		t.Fatalf("completion = %q", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["collector"] != "Kwame" || orders[0]["packageName"] != "Kwame" {
		t.Fatalf("orders = %v, want Kwame as collector and package name", orders)
	}
	if _, present := orders[0]["note"]; present {
		t.Fatalf("a declined message was recorded as a note: %v", orders[0])
	}
}

func TestTapMeetThenPlaceThenDetails(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550009004"
	agreeBagAtList(t, c, buyer)

	tap(t, buyer, "ff_meet")
	if got := c.last(t, buyer); got != "Where should we meet you?" {
		t.Fatalf("meet tap = %q, want the meet place question", got)
	}
	c.say(t, buyer, "Circle")
	c.finishDetails(t, buyer)
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Pay here") {
		t.Fatalf("completion = %q", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["fulfilment"] != "meet" || orders[0]["location"] != "Circle" ||
		orders[0]["packageName"] != "Akosua Mensah" {
		t.Fatalf("orders = %v, want a meet at Circle with the package name", orders)
	}
}

func TestPackageNameIsRememberedForTheNextOrder(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550009005"

	agreeBagAtList(t, c, buyer)
	tap(t, buyer, "ff_self")
	c.finishDetails(t, buyer)
	c.replies(t, buyer)

	// A second purchase: the name question holds up the name she
	// used last time, and "same" keeps it.
	c.say(t, buyer, "Handmade Beaded Bag")
	c.last(t, buyer)
	c.say(t, buyer, "850")
	c.replies(t, buyer)
	tap(t, buyer, "ff_self")
	if got := c.last(t, buyer); !strings.Contains(got, "Last time it was Akosua Mensah") {
		t.Fatalf("second order name question = %q, want the remembered name held up", got)
	}
	c.say(t, buyer, "same")
	c.say(t, buyer, "no")
	c.say(t, buyer, "yes")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "The package will carry the name Akosua Mensah.") {
		t.Fatalf("second completion = %q", got)
	}
}

func TestDetailsDeclineAndCancel(t *testing.T) {
	dir, c := setupTwitterShops(t)

	// A plain "no message" completes with no note on the order.
	buyer := "233550009006"
	agreeBagAtList(t, c, buyer)
	tap(t, buyer, "ff_self")
	c.say(t, buyer, "Akosua Mensah")
	c.say(t, buyer, "no message")
	c.say(t, buyer, "yes")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Pay here") {
		t.Fatalf("no-message completion = %q", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 {
		t.Fatalf("orders = %v, want one", orders)
	}
	if _, present := orders[0]["note"]; present {
		t.Fatalf("'no message' was recorded as a note: %v", orders[0])
	}

	// A cancel at the details questions still drops the sale.
	buyer2 := "233550009007"
	agreeBagAtList(t, c, buyer2)
	tap(t, buyer2, "ff_self")
	c.say(t, buyer2, "cancel")
	if got := c.last(t, buyer2); !strings.Contains(got, "Okay, cancelled") {
		t.Fatalf("cancel at the name question = %q, want the sale dropped", got)
	}
	if got := len(ordersOnDisk(t, dir)); got != 1 {
		t.Fatalf("orders after a details cancel = %d, want still one", got)
	}
}
