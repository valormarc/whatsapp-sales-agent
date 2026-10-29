package agent

// The merchant's sale notice is her receipt for the sale, and
// it is a record: one message on the fixed paper, the same
// lines in the same order every time (see records.go), the
// day so far at the foot.

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
	if !strings.HasPrefix(notice, "New order EFUA-0001\n") {
		t.Fatalf("notice should open with its heading and reference:\n%s", notice)
	}
	for _, want := range []string{
		"Item: Handmade Beaded Bag",
		"Agreed price: GH₵850",
		"\nBuyer: " + buyer,
		"\nDelivery: ",
		"\nPackage name: Akosua Mensah",
		"\nBuyer's message: Please call when the rider leaves",
		"\nTotal: GH₵875",
		"\nPayment: waiting",
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
	if !strings.Contains(notice, "Payment: waiting\n\nToday:") {
		t.Fatalf("the day block should sit at the foot, set off by a blank line:\n%s", notice)
	}
}
