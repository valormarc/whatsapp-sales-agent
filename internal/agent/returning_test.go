package agent

// Returning-buyer recognition. Her last paid order at the
// shop is the agent's memory of her: she is welcomed back,
// last time's way of getting the goods is held up to
// confirm or change, and the size she took for the same
// product is read back in the recap instead of asked again.
// A first-time buyer hears none of it.

import (
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

const returningBuyer = "233550005001"

func seedLastOrder(t *testing.T) {
	t.Helper()
	orders.Default.RecordOrder("TEST-0001", orders.Order{
		Reference:   "TEST-0001",
		From:        returningBuyer,
		CreatedAt:   time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339),
		ProductName: "Ankle Strap Heels",
		Price:       110,
		Status:      "paid",
		MerchantID:  "demo",
		Fulfilment:  "delivery",
		Location:    "Koforidua",
		PackageName: "Efua Mensima",
		Size:        "40",
	})
}

func agreeHeelsReturning(t *testing.T, c *chat, buyer string) string {
	t.Helper()
	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Ankle Strap Heels")
	c.last(t, buyer)
	c.say(t, buyer, "110")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "yours for") {
		figs := figuresIn(got)
		if len(figs) == 0 {
			t.Fatalf("first offer reply = %q", got)
		}
		c.say(t, buyer, moneyFigure(figs[len(figs)-1]))
		got = strings.Join(c.replies(t, buyer), "\n")
	}
	return got
}

func TestReturningBuyerIsGreetedAndOfferedHerUsual(t *testing.T) {
	dir := setupChat(t, basketOwner, basketCatalogue)
	c := &chat{dir: dir}
	seedLastOrder(t)

	c.say(t, returningBuyer, "SHOP-TEST")
	if got := c.replies(t, returningBuyer); !strings.Contains(strings.Join(got, "\n"), "Welcome back to Test Shop.") {
		t.Fatalf("a returning buyer should be welcomed back, got %q", got)
	}
	c.say(t, returningBuyer, "Ankle Strap Heels")
	c.last(t, returningBuyer)
	c.say(t, returningBuyer, "110")
	got := strings.Join(c.replies(t, returningBuyer), "\n")
	if !strings.Contains(got, "yours for") {
		figs := figuresIn(got)
		c.say(t, returningBuyer, moneyFigure(figs[len(figs)-1]))
		got = strings.Join(c.replies(t, returningBuyer), "\n")
	}
	if !strings.Contains(got, "Last time you had it delivered to Koforidua. Same again, or different?") {
		t.Fatalf("her usual should be held up, got %q", got)
	}
	c.say(t, returningBuyer, "same")
	if got := c.last(t, returningBuyer); !strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("same again should go straight to the details (her size remembered, no size question), got %q", got)
	}
	c.say(t, returningBuyer, "Efua Mensima")
	c.last(t, returningBuyer)
	c.say(t, returningBuyer, "no")
	recap := c.last(t, returningBuyer)
	if !strings.Contains(recap, "Size: 40.") || !strings.Contains(recap, "Delivery to Koforidua.") {
		t.Fatalf("the recap should read back her remembered size and place, got %q", recap)
	}
	c.say(t, returningBuyer, "yes")
	c.replies(t, returningBuyer)
	o := orders.Default.GetOrder("TEST-0002")
	if o == nil || o.Fulfilment != "delivery" || o.Location != "Koforidua" || o.Size != "40" {
		t.Fatalf("order = %+v, want her usual carried onto it", o)
	}
}

func TestReturningBuyerCanChangeHerUsual(t *testing.T) {
	dir := setupChat(t, basketOwner, basketCatalogue)
	c := &chat{dir: dir}
	seedLastOrder(t)

	got := agreeHeelsReturning(t, c, returningBuyer)
	if !strings.Contains(got, "Same again, or different?") {
		t.Fatalf("her usual should be held up, got %q", got)
	}
	c.say(t, returningBuyer, "different")
	if got := c.last(t, returningBuyer); !strings.Contains(got, "How do you want to get it?") {
		t.Fatalf("different should open the ordinary choice, got %q", got)
	}
	c.say(t, returningBuyer, "I will pick it up myself")
	c.last(t, returningBuyer)
	c.say(t, returningBuyer, "Efua Mensima")
	c.last(t, returningBuyer)
	c.say(t, returningBuyer, "no")
	c.last(t, returningBuyer)
	c.say(t, returningBuyer, "yes")
	c.replies(t, returningBuyer)
	o := orders.Default.GetOrder("TEST-0002")
	if o == nil || o.Fulfilment != "pickup" {
		t.Fatalf("order = %+v, want her changed choice on it", o)
	}
}

func TestFirstTimeBuyerIsNotOfferedAUsual(t *testing.T) {
	dir := setupChat(t, basketOwner, basketCatalogue)
	c := &chat{dir: dir}
	fresh := "233550005009"

	c.say(t, fresh, "SHOP-TEST")
	if got := strings.Join(c.replies(t, fresh), "\n"); !strings.Contains(got, "Welcome to Test Shop.") || strings.Contains(got, "Welcome back") {
		t.Fatalf("a first-time buyer gets the plain welcome, got %q", got)
	}
	got := agreeHeelsReturning(t, c, fresh)
	if strings.Contains(got, "Last time") || !strings.Contains(got, "How do you want to get it?") {
		t.Fatalf("a first-time buyer goes straight to the choice, got %q", got)
	}
}
