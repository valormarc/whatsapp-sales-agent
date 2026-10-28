package agent

// The merchant's sale notice is her receipt for the sale, and it
// is laid out the way she reads on her phone: goods and money
// first, the order's details labelled underneath, the day so far
// at the foot. Nothing is buried inside one long paragraph and
// no figure is stated twice in different words.

import (
	"strings"
	"testing"
)

func TestSaleNoticeIsLaidOutInLines(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550009021"
	agreeBagAtList(t, c, buyer)

	tap(t, buyer, "ff_delivery")
	c.last(t, buyer)
	c.say(t, buyer, "Osu")
	c.last(t, buyer)
	c.say(t, buyer, "Akosua Mensah")
	c.last(t, buyer)
	c.say(t, buyer, "Please call when the rider leaves")
	c.say(t, buyer, "yes")
	c.replies(t, buyer)

	notes := c.replies(t, ownerEfua)
	if len(notes) != 1 {
		t.Fatalf("merchant notices = %v, want the one sale notice", notes)
	}
	notice := notes[0]
	if !strings.HasPrefix(notice, "New order\n") {
		t.Fatalf("notice should open with its heading on its own line:\n%s", notice)
	}
	for _, want := range []string{
		"Sold for GH₵",
		"(list GH₵",
		"\nRef: EFUA-0001",
		"\nBuyer: " + buyer,
		"\nDelivery: ",
		"\nPackage name: Akosua Mensah.",
		"\nBuyer's message: Please call when the rider leaves.",
		"\nToday: 1 sale, GH₵",
		"\nGiven away: GH₵",
	} {
		if !strings.Contains(notice, want) {
			t.Fatalf("notice missing %q:\n%s", want, notice)
		}
	}
	for _, banned := range []string{"Day book:", "margin given away", "sale(s)"} {
		if strings.Contains(notice, banned) {
			t.Fatalf("notice still carries the old run-on wording %q:\n%s", banned, notice)
		}
	}
	// The day figures close the notice, below a blank line.
	if !strings.Contains(notice, "Please call when the rider leaves.\n\nToday:") {
		t.Fatalf("the day block should sit at the foot, set off by a blank line:\n%s", notice)
	}
}
