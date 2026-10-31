package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

// The buyer's receipt and order checking: at the sale she is
// given the order's reference (her shop's code and the shop's own
// serial), and when she comes back she quotes it, or asks after
// her order, and it is read back to her. Only her own orders, in
// the shop she is standing in.

// buySheaPickup completes a shea butter purchase at Efua's by
// own pickup and returns the completion pieces.
func buySheaPickup(t *testing.T, c *chat, buyer string) string {
	t.Helper()
	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Shea butter")
	c.last(t, buyer)
	c.say(t, buyer, "120")
	c.replies(t, buyer)
	tap(t, buyer, "ff_pickup")
	c.replies(t, buyer) // the pickup-kind buttons, drained
	tap(t, buyer, "ff_self")
	c.finishDetails(t, buyer)
	return strings.Join(c.replies(t, buyer), "\n")
}

func TestBuyerReceivesHerReferenceAtTheSale(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550010001"

	got := buySheaPickup(t, c, buyer)
	ref := orders.Default.NewestFor("efua", buyer).Reference
	if !strings.HasPrefix(ref, "EFUA-0001-") {
		t.Fatalf("her reference should carry the shop's serial and its tail, got %q", ref)
	}
	if !strings.Contains(got, "Your order reference is "+ref+".") {
		t.Fatalf("completion = %q, want her reference given as its own piece", got)
	}
}

func TestReferencesAreSerialPerShop(t *testing.T) {
	_, c := setupTwitterShops(t)

	buySheaPickup(t, c, "233550010002")
	if ref := orders.Default.NewestFor("efua", "233550010002").Reference; !strings.HasPrefix(ref, "EFUA-0001-") {
		t.Fatalf("first Efua order reference = %q, want the EFUA-0001 serial with its tail", ref)
	}
	buySheaPickup(t, c, "233550010003")
	if ref := orders.Default.NewestFor("efua", "233550010003").Reference; !strings.HasPrefix(ref, "EFUA-0002-") {
		t.Fatalf("second Efua order reference = %q, want the EFUA-0002 serial with its tail", ref)
	}
	// Ama's first order carries her own code and starts at one.
	buyer := "233550010004"
	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "Smoked Tilapia")
	c.last(t, buyer)
	c.say(t, buyer, "180")
	c.replies(t, buyer)
	tap(t, buyer, "ff_pickup")
	c.replies(t, buyer) // the pickup-kind buttons, drained
	tap(t, buyer, "ff_self")
	c.finishDetails(t, buyer)
	amaRef := orders.Default.NewestFor("ama", buyer).Reference
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.HasPrefix(amaRef, "AMA-0001-") || !strings.Contains(got, "Your order reference is "+amaRef+".") {
		t.Fatalf("first Ama order = %q, want her AMA-0001 reference with its tail given as its own piece", got)
	}
}

func TestCheckByReferenceAndByAsking(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550010005"
	buySheaPickup(t, c, buyer)
	c.replies(t, ownerEfua) // drain the merchant notice
	ref := orders.Default.NewestFor("efua", buyer).Reference

	c.say(t, buyer, ref)
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Order "+ref+": Raw Shea Butter (500g) at GH₵120.") ||
		!strings.Contains(got, "It is not paid yet.") ||
		!strings.Contains(got, "You are collecting it yourself.") ||
		!strings.Contains(got, "The package carries the name Akosua Mensah.") {
		t.Fatalf("readback by reference = %q", got)
	}

	c.say(t, buyer, "check my order")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Order "+ref+":") {
		t.Fatalf("readback by asking = %q, want her latest order pulled up", got)
	}
}

func TestCheckedOrderShowsPaidOnlyWhenTheLinkSettles(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550010006"
	buySheaPickup(t, c, buyer)
	c.replies(t, ownerEfua)

	// Neither asking for the number nor showing a screenshot
	// moves the order: the check keeps saying unpaid.
	c.say(t, buyer, "I will pay to the number")
	c.replies(t, buyer)
	sayImage(t, buyer, "MEDIA-CHECK-1", "")
	c.last(t, buyer)
	c.say(t, buyer, "check my order")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "It is not paid yet.") {
		t.Fatalf("readback before settlement = %q, want the unpaid line", got)
	}

	// Her report is checked against the payment service itself;
	// when it says the money landed, the order settles and the
	// check then says paid. (The harness walk mints no real
	// session, so the order is given the session reference it
	// would carry in production.)
	minted := orders.Default.NewestFor("efua", buyer).Reference
	session := "EFUA-0001"
	orders.Default.Update(minted, func(o *orders.Order) { o.ProviderRef = &session })
	fakeSeevReadback(t, "completed", 12000)
	c.say(t, buyer, "I have paid")
	c.replies(t, buyer)
	c.say(t, buyer, minted)
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "It is paid. Thank you.") {
		t.Fatalf("readback after settlement = %q, want the paid line", got)
	}
}

func TestCheckingNeverCrossesBuyersOrShops(t *testing.T) {
	_, c := setupTwitterShops(t)
	buySheaPickup(t, c, "233550010007")

	minted := orders.Default.NewestFor("efua", "233550010007").Reference

	// Another buyer in the same shop quoting the reference gets
	// a plain not-found, never the order.
	stranger := "233550010008"
	c.say(t, stranger, "SHOP-EFUA")
	c.replies(t, stranger)
	c.say(t, stranger, minted)
	if got := c.last(t, stranger); !strings.Contains(got, "I cannot find an order with the reference "+minted+" for you") {
		t.Fatalf("cross-buyer check = %q, want a plain not found", got)
	}

	// The buyer herself, standing in another shop, cannot pull
	// the Efua order up there either.
	buyer := "233550010007"
	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, minted)
	if got := c.last(t, buyer); !strings.Contains(got, "I cannot find an order with the reference "+minted+" for you at Ama's Stall") {
		t.Fatalf("cross-shop check = %q, want a plain not found for this shop", got)
	}

	// And a buyer with nothing bought yet is told so plainly.
	fresh := "233550010009"
	c.say(t, fresh, "SHOP-AMA")
	c.replies(t, fresh)
	c.say(t, fresh, "check my order")
	if got := c.last(t, fresh); !strings.Contains(got, "You have no order at Ama's Stall yet.") {
		t.Fatalf("no-order check = %q", got)
	}
}
