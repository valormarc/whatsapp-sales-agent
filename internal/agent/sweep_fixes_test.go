package agent

// The five failures of the 2 November final platform test,
// each encoded as the regression that proves its fix. The
// inputs are the sweep's own sentences, with near neighbours
// beside them: the class is fixed, not the instance.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Failure 1, the reading itself: a count stated the way a
// seller actually states it — her product's full name
// between the figure and the stock phrase, or the set
// command spoken as a sentence — must parse.
func TestStockCountReadsTheSellersOwnShapes(t *testing.T) {
	for text, want := range map[string]int{
		"I have 8 pieces of the Ankara Cloth in stock":      8,
		"Please set the stock for Ankara Cloth to 8 pieces": 8,
		"set the stock for Ankara Cloth to 5":               5,
		"update stock of Joggers to 12":                     12,
		"I have 3 pieces of it":                             3,
		"3 pieces of them":                                  3,
		"I have 8 of the Ankara Cloth in stock":             8,
	} {
		if n, ok := parseStockCount(text); !ok || n != want {
			t.Fatalf("parseStockCount(%q) = %d,%v, want %d", text, n, ok, want)
		}
	}
	for _, text := range []string{
		"I sold 3 dresses yesterday",
		"Only few in stock please",
		"the price is 120 for 6 yards",
	} {
		if n, ok := parseStockCount(text); ok {
			t.Fatalf("parseStockCount(%q) = %d, want no count read", text, n)
		}
	}
}

const shopWithAnkara = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"products":[{"id":"ankara-cloth","name":"Ankara Cloth","description":"Bright wax print cotton.","listPrice":120,"floorPrice":100,"stock":1}]}}}`

// Failure 1, the shelf: both of the sweep's sentences land
// on the named product, with the confirmation spoken and
// the store checked — not the day book.
func TestMerchantStockStatementsLandOnTheShelf(t *testing.T) {
	merchant := "233550002801"
	dir := setupChat(t, merchant, shopWithAnkara)
	c := &chat{dir: dir}
	_ = dir

	c.say(t, merchant, "I have 8 pieces of the Ankara Cloth in stock")
	if got := c.last(t, merchant); !strings.Contains(got, "now has 8 in stock") {
		t.Fatalf("stock statement reply = %q, want the applied confirmation", got)
	}
	if p := catalogue.FindByName(MerchantID, "Ankara Cloth"); p == nil || p.Stock != 8 {
		t.Fatalf("ankara stock = %+v, want 8", p)
	}

	c.say(t, merchant, "Please set the stock for Ankara Cloth to 5 pieces")
	if got := c.last(t, merchant); !strings.Contains(got, "now has 5 in stock") {
		t.Fatalf("set stock reply = %q, want the applied confirmation", got)
	}
	if p := catalogue.FindByName(MerchantID, "Ankara Cloth"); p == nil || p.Stock != 5 {
		t.Fatalf("ankara stock = %+v, want 5", p)
	}
}

// Failure 1, the walk: a count riding in the name step's
// answer is the listing's stock, and the clause that
// carried it is neither the name nor the description.
func TestNameStepCountClauseRidesToTheSave(t *testing.T) {
	merchant := "233550002802"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.last(t, merchant)
	c.say(t, merchant, "Kente Stole, and I have 3 pieces of it")
	c.last(t, merchant)
	c.say(t, merchant, "Handwoven stole in bright colours")
	c.last(t, merchant)
	c.say(t, merchant, "200")
	c.last(t, merchant)
	c.say(t, merchant, "170")
	finishWalkToShelf(t, c, merchant)

	p := catalogue.FindByName(MerchantID, "Kente Stole")
	if p == nil {
		t.Fatalf("the listing never saved (dir %s)", dir)
	}
	if p.Stock != 3 {
		t.Fatalf("saved stock = %d, want her stated 3", p.Stock)
	}
	if strings.Contains(p.Description, "pieces of it") || strings.Contains(p.Description, "Kente Stole, and") {
		t.Fatalf("saved description = %q, want her description, not the stock sentence", p.Description)
	}
	if p.Description != "Handwoven stole in bright colours" {
		t.Fatalf("saved description = %q", p.Description)
	}
}

// Failure 2: at the package name question, an answer that
// is another shop's code word is her answer, not travel.
// The checkout survives, the name is written on the order,
// and the shop never changes.
func TestPackageNameThatIsAShopCodeStaysInTheCheckout(t *testing.T) {
	dir, c := setupTwoShops(t)
	buyer := "233550002811"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "Smoked Tilapia")
	c.last(t, buyer)
	c.say(t, buyer, "180")
	c.replies(t, buyer)
	tap(t, buyer, "ff_delivery")
	c.say(t, buyer, "Osu")
	if got := c.last(t, buyer); !strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("the walk must reach the package name question, got %q", got)
	}
	c.say(t, buyer, "Efua Mensah")
	got := strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "You have left") {
		t.Fatalf("a package name must never move the buyer, got %q", got)
	}
	c.say(t, buyer, "no")
	if got := c.last(t, buyer); !strings.Contains(got, "Efua Mensah") || !strings.Contains(got, "Smoked Tilapia") {
		t.Fatalf("recap = %q, want the name read back on her own order", got)
	}
	c.say(t, buyer, "yes")
	c.replies(t, buyer)
	got2 := ordersOnDisk(t, dir)
	if len(got2) != 1 || got2[0]["packageName"] != "Efua Mensah" {
		t.Fatalf("orders = %v, want one order under the name she gave", got2)
	}
}

// Failure 2, the other side: deliberate travel still works
// mid-checkout — the explicit SHOP-<code> marker is travel
// in any company.
func TestExplicitCodeStillMovesABuyerMidCheckout(t *testing.T) {
	_, c := setupTwoShops(t)
	buyer := "233550002812"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "Smoked Tilapia")
	c.last(t, buyer)
	c.say(t, buyer, "180")
	c.replies(t, buyer)
	tap(t, buyer, "ff_delivery")
	c.say(t, buyer, "Osu")
	c.last(t, buyer)
	c.say(t, buyer, "SHOP-EFUA")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "You have left") || !strings.Contains(got, "Efua's Store") {
		t.Fatalf("an explicit code must still move her, got %q", got)
	}
}

// Failure 3: the merchant's in-hand answer in a full
// sentence releases the gate exactly as the button does.
func TestInHandSentenceReleasesTheGate(t *testing.T) {
	merchant := "233550002821"
	dir := setupChat(t, merchant, shopOneOff)
	c := &chat{dir: dir}
	buyer := "233550002822"

	buyOneOff(t, c, buyer)
	c.replies(t, buyer)
	if got := strings.Join(c.replies(t, merchant), "\n"); !strings.Contains(got, "Do you have it in hand now?") {
		t.Fatalf("the seller must be asked, got %q", got)
	}
	c.say(t, merchant, "Yes, the Kente Cloth is in hand, she can pay")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Pay here") {
		t.Fatalf("her sentence must send the payment button, got %q", got)
	}
	if n := len(orders.Default.All()); n != 1 {
		t.Fatalf("the order must be recorded after her yes, got %d (dir %s)", n, dir)
	}
}

// Failure 3, the refusal in a sentence ends the sale the
// same plain way.
func TestInHandSentenceRefusalEndsTheSale(t *testing.T) {
	merchant := "233550002823"
	dir := setupChat(t, merchant, shopOneOff)
	c := &chat{dir: dir}
	buyer := "233550002824"

	buyOneOff(t, c, buyer)
	c.replies(t, buyer)
	c.replies(t, merchant)

	c.say(t, merchant, "No, the Kente Cloth is not in hand")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "does not have the Kente Cloth in hand") || !strings.Contains(got, "Nothing has been charged") {
		t.Fatalf("a sentence no must end the sale plainly, got %q", got)
	}
	if n := len(orders.Default.All()); n != 0 {
		t.Fatal("no order may stand after a no")
	}
}

// Failure 4: the merchant's own shelf ask, in the word
// shelf, serves her catalogue — never the day book, never
// a disclaimer about what she cannot see.
func TestMerchantShelfAskServesHerCatalogue(t *testing.T) {
	merchant := "233550002831"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "Show me my shelf")
	got := c.last(t, merchant)
	if !strings.Contains(got, "Handmade Beaded Bag") || !strings.Contains(got, "850") {
		t.Fatalf("shelf ask reply = %q, want her catalogue", got)
	}
	if strings.Contains(got, "Today's book") || strings.Contains(got, "do not have your full shelf") {
		t.Fatalf("shelf ask reply = %q, want no day book and no disclaimer", got)
	}
}

func TestMerchantMyShelfServesHerCatalogue(t *testing.T) {
	merchant := "233550002832"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "my shelf")
	got := c.last(t, merchant)
	if !strings.Contains(got, "Handmade Beaded Bag") {
		t.Fatalf("my shelf reply = %q, want her catalogue", got)
	}
}

// Failure 5, the reading itself: the affirmation and the
// place, separated by a comma, read exactly as they do
// without one — in the place, the stored value's guard,
// the package name and the collector alike.
func TestAffirmationReadingSurvivesPunctuation(t *testing.T) {
	for raw, want := range map[string]string{
		"Yes, Accra":       "Accra",
		"yes, accra":       "accra",
		"Ok, Kumasi":       "Kumasi",
		"Yeah, East Legon": "East Legon",
		"Yes,Accra":        "Accra",
	} {
		if got := cleanPlace(raw); got != want {
			t.Fatalf("cleanPlace(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := guardPlace("Yes, Accra"); got != "Accra" {
		t.Fatalf("guardPlace = %q, want the place alone", got)
	}
	if got := cleanPackageName("Yes, Efua Mensah"); got != "Efua Mensah" {
		t.Fatalf("cleanPackageName = %q, want the name alone", got)
	}
	if got := cleanCollector("Yes, Kwame"); got != "Kwame" {
		t.Fatalf("cleanCollector = %q, want the name alone", got)
	}
}

// Failure 5, the journey: at the delivery walk's own
// "Where should it go?", "Yes, Accra" is an answer, and
// the order carries Accra alone.
func TestYesCommaAccraAtTheWhereStep(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550002841"
	agreeBagAtList(t, c, buyer)
	tap(t, buyer, "ff_delivery")
	c.say(t, buyer, "Yes, Accra")
	if got := c.last(t, buyer); !strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("the place must be taken, not re-asked, got %q (dir %s)", got, dir)
	}
	c.say(t, buyer, "Akosua Mensah")
	c.last(t, buyer)
	c.say(t, buyer, "no")
	if got := c.last(t, buyer); !strings.Contains(got, "Delivery to Accra.") || strings.Contains(got, "Yes") {
		t.Fatalf("recap = %q, want Accra alone", got)
	}
	c.say(t, buyer, "yes")
	c.replies(t, buyer)
	all := orders.Default.All()
	if len(all) != 1 || all[0].Location != "Accra" {
		t.Fatalf("orders = %+v, want one order placed in Accra alone", all)
	}
}
