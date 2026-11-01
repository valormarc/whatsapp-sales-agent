package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

// The place inside an answer: a buyer who replies to the
// same-or-different question with "Yes accra" has agreed
// and named Accra. The affirmation is her answering the
// question; only the place may be stored, printed or
// shipped. A record that took the whole reply put a town
// called Yes on two real orders and in the buyer's
// remembered details, where every later order inherited
// it without being asked.
func TestCleanPlaceReadsThePlaceInsideAnAnswer(t *testing.T) {
	for raw, want := range map[string]string{
		"Yes accra":    "accra",
		"yes":          "",
		"same":         "",
		"Accra":        "Accra",
		"to Yes accra": "accra",
		"East Legon":   "East Legon",
		"ok Kumasi":    "Kumasi",
	} {
		if got := cleanPlace(raw); got != want {
			t.Fatalf("cleanPlace(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestGuardPlaceCleansAStoredValue(t *testing.T) {
	if got := guardPlace("Yes accra"); got != "accra" {
		t.Fatalf("guardPlace = %q, want the place alone", got)
	}
	if got := guardPlace("East Legon"); got != "East Legon" {
		t.Fatalf("guardPlace = %q, want the stored place untouched", got)
	}
}

// The whole journey, in his own words: a returning buyer
// answers the usual question with an affirmation and the
// place together, and the order that comes out of it
// carries Accra alone — in the recap she confirms and on
// the record the shop fulfils from.
func TestYesAccraStoresAccraAlone(t *testing.T) {
	dir := setupChat(t, basketOwner, basketCatalogue)
	c := &chat{dir: dir}
	seedLastOrder(t)

	got := agreeHeelsReturning(t, c, returningBuyer)
	if !strings.Contains(got, "Same again, or different?") {
		t.Fatalf("her usual should be held up, got %q", got)
	}
	c.say(t, returningBuyer, "Yes accra")
	c.last(t, returningBuyer)
	c.say(t, returningBuyer, "Efua Mensima")
	c.last(t, returningBuyer)
	c.say(t, returningBuyer, "no")
	recap := c.last(t, returningBuyer)
	if strings.Contains(recap, "Yes accra") || !strings.Contains(recap, "Delivery to accra.") {
		t.Fatalf("the recap must carry the place alone, got %q", recap)
	}
	c.say(t, returningBuyer, "yes")
	c.replies(t, returningBuyer)
	o := orders.Default.NewestFor(MerchantID, returningBuyer)
	if o == nil || o.Location != "accra" {
		t.Fatalf("order = %+v, want its place to be Accra alone", o)
	}
}
