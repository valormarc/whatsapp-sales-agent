package agent

import (
	"strings"
	"testing"
)

// When nothing of a past order can be repeated, the
// refusal is followed by the shelf itself, presented as
// the sheet she can pick from — never the whole
// catalogue dumped as text. His live reorder: twenty
// two items arrived as one wall of text behind a
// doubled "here is what we have".
func TestReorderAllMissingPresentsTheShelfSheet(t *testing.T) {
	seed := `{"merchants":{"efua":{"id":"efua","name":"Efua's Store","shopCode":"EFUA","ownerWhatsApp":"` + reorderOwner + `","payoutPhone":"` + reorderOwner + `","dailyGiveawayAllowance":300,"deliveryFee":25,"deliveryPaidTo":"merchant","products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":0},{"id":"heels","name":"Ankle Strap Heels","description":"Black ankle strap heels.","listPrice":120,"floorPrice":100,"stock":2}]}}}`
	dir := setupChat(t, reorderOwner, seed)
	c := &chat{dir: dir}
	seedReorderPast(reorderBuyer, 850)
	c.say(t, reorderBuyer, "SHOP-EFUA")
	c.replies(t, reorderBuyer)
	c.say(t, reorderBuyer, "Can I buy this again?")
	got := c.joined(t, reorderBuyer)
	if !strings.Contains(got, "cannot be repeated as it stands. Here is what they have now:") {
		t.Fatalf("the refusal must be spoken plainly first: %q", got)
	}
	if strings.Contains(got, "Here is what we have:") || strings.Contains(got, "Ankle Strap Heels - GH₵120") {
		t.Fatalf("the shelf must not arrive as a text dump: %q", got)
	}
	foundSheet := false
	pastRefusal := false
	for _, rec := range rawOutboxRecords(t, dir) {
		if rec.To != reorderBuyer {
			continue
		}
		if strings.Contains(rec.Body, "cannot be repeated as it stands") {
			pastRefusal = true
			continue
		}
		if pastRefusal && rec.Kind == "list" {
			foundSheet = true
		}
	}
	if !foundSheet {
		t.Fatal("the shelf sheet (a list message) must follow the refusal")
	}
}
