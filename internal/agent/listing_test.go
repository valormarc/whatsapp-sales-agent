package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// The listing rule: wherever several items are named with
// their prices — a hand opening, a hand agreed, the settled
// items behind a scope question, an order read back — they
// print one per line at their prices, never strung into a
// sentence, so the deal is scanned instead of read out of
// prose. These tests pin the shapes.

func TestHandHaggleListsItemsLineByLine(t *testing.T) {
	dir := setupChat(t, totalHandOwner, totalHandCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009601"
	pickThree(t, c, buyer, "LST1")

	c.say(t, buyer, "I will give you 509")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "For all of them together, then:\nDouble Clothes Rack: GH₵270") ||
		!strings.Contains(got, "Ribbed Off-Shoulder Bodycon Maxi Dress: GH₵130") ||
		!strings.Contains(got, "Halter Pleated Maxi Dress: GH₵150") ||
		!strings.Contains(got, "That is GH₵550 in all.") {
		t.Fatalf("the hand should open as a list, got %q", got)
	}
	if strings.Contains(got, "come to GH₵550 in all") {
		t.Fatalf("the old strung sentence must not return, got %q", got)
	}
	got = meetTheCounter(t, c, buyer, got)
	if !strings.Contains(got, "Double Clothes Rack: GH₵") || !strings.Contains(got, "Halter Pleated Maxi Dress: GH₵") ||
		!strings.Contains(got, "That makes your order GH₵") {
		t.Fatalf("the agreed hand should print as a receipt of shares, got %q", got)
	}
}

func TestScopeQuestionListsTheSettledItems(t *testing.T) {
	dir := setupChat(t, totalHandOwner, totalHandCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009602"
	pickRackAndHalter(t, c, buyer, "LST2")

	c.say(t, buyer, "270")
	c.replies(t, buyer)
	c.say(t, buyer, "I will give you 400 for all")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "These are already settled:\nDouble Clothes Rack: GH₵270") {
		t.Fatalf("the settled items should be listed, got %q", got)
	}
}

func TestBasketViewListsSettledAndInFront(t *testing.T) {
	dir := setupChat(t, totalHandOwner, totalHandCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009604"
	pickRackAndHalter(t, c, buyer, "BV1")

	c.say(t, buyer, "270")
	c.replies(t, buyer)
	c.say(t, buyer, "show me what's in my basket")
	got := c.last(t, buyer)
	if !strings.Contains(got, "Here is what you have so far:") ||
		!strings.Contains(got, "Double Clothes Rack: GH₵270") ||
		!strings.Contains(got, "settled so far") ||
		!strings.Contains(got, "Picked, not settled yet:") ||
		!strings.Contains(got, "Halter Pleated Maxi Dress: GH₵150") {
		t.Fatalf("the basket should list the settled rack and the waiting halter apart, got %q", got)
	}
}

func TestBasketViewListsPicksOnly(t *testing.T) {
	dir := setupChat(t, totalHandOwner, totalHandCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009605"

	c.say(t, buyer, "SHOP-HAND")
	c.replies(t, buyer)
	HandleInbound(Inbound{From: buyer, ListID: "shelf_rack", MessageID: "wamid.BV2-P1"})
	HandleInbound(Inbound{From: buyer, ListID: "shelf_halter", MessageID: "wamid.BV2-P2"})
	c.replies(t, buyer)
	c.say(t, buyer, "show me my basket")
	got := c.last(t, buyer)
	if !strings.Contains(got, "Double Clothes Rack: GH₵270") ||
		!strings.Contains(got, "Halter Pleated Maxi Dress: GH₵150") ||
		!strings.Contains(got, "at shelf prices") {
		t.Fatalf("the picks should be listed at their shelf prices, got %q", got)
	}
}

func TestBasketViewEmptyBasket(t *testing.T) {
	dir := setupChat(t, totalHandOwner, totalHandCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009606"

	c.say(t, buyer, "SHOP-HAND")
	c.replies(t, buyer)
	c.say(t, buyer, "show me what's in my basket")
	if got := c.last(t, buyer); !strings.Contains(got, "Your basket is empty") {
		t.Fatalf("an empty basket should be answered plainly, got %q", got)
	}
}

func TestDiscoverySearchResultsListedByShop(t *testing.T) {
	dir := setupChat(t, "233265993567", discoveryCatalogue)
	c := &chat{dir: dir}
	buyer := "233550009607"

	acted := inTurn(buyer, func() bool {
		return actOnDiscovery(buyer, "beaded", &discoveryUnderstanding{
			Intent: "search", Item: "beaded",
			Matches: []discoveryMatch{
				{Shop: "LUXE", Product: "Beaded Sandals"},
				{Shop: "LUXE", Product: "HP EliteBook 1030 G8 x360"},
				{Shop: "EFUA", Product: "Handmade Beaded Bag"},
			},
		})
	})
	if !acted {
		t.Fatalf("the search should be served")
	}
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "LUXE has:\nBeaded Sandals: GH₵80\nHP EliteBook 1030 G8 x360: GH₵6,800") ||
		!strings.Contains(got, "EFUA has:\nHandmade Beaded Bag: GH₵850") {
		t.Fatalf("each shop's matches should be listed under the shop, got %q", got)
	}
	if strings.Contains(got, " | ") {
		t.Fatalf("the strung line must not return, got %q", got)
	}
}

func TestDiscoveryTasteIsAList(t *testing.T) {
	setupChat(t, "233265993567", discoveryCatalogue)
	taste, _ := discoveryTaste()
	if !strings.HasPrefix(taste, "Here is some of what the shops have:\n") ||
		!strings.Contains(taste, "\nWhat else are you looking for?") ||
		strings.Contains(taste, "; ") {
		t.Fatalf("the taste should be a list, got %q", taste)
	}
}

func TestPaymentRecordListsSeveralItems(t *testing.T) {
	setupChat(t, totalHandOwner, totalHandCatalogue)
	rec := paymentConfirmedRecord(&orders.Order{
		Reference: "HAND-6008", MerchantID: MerchantID, Price: 330, Total: 330,
		ProductName: "Thong Sandals and Double Clothes Rack",
		Items: []orders.OrderItem{
			{Name: "Thong Sandals", Price: 80},
			{Name: "Double Clothes Rack", Price: 250},
		},
	})
	if !strings.Contains(rec, "Payment confirmed\nThong Sandals: GH₵80\nDouble Clothes Rack: GH₵250\nAmount: GH₵330") {
		t.Fatalf("the payment record should list the items, got %q", rec)
	}
	if strings.Contains(rec, "Item: Thong Sandals and Double Clothes Rack") {
		t.Fatalf("the joined label must not return, got %q", rec)
	}
}

func TestOrderReadbackListsItemsLineByLine(t *testing.T) {
	setupChat(t, totalHandOwner, totalHandCatalogue)
	shop := catalogue.GetMerchant(MerchantID)
	orders.Default.RecordOrder("HAND-6003", orders.Order{
		From: "233550009603", Status: "paid", MerchantID: MerchantID,
		ProductName: "Double Clothes Rack, Ribbed Off-Shoulder Bodycon Maxi Dress and Halter Pleated Maxi Dress",
		Price:       527, Total: 527, Fulfilment: "pickup",
		Items: []orders.OrderItem{
			{Name: "Double Clothes Rack", Price: 260.8},
			{Name: "Ribbed Off-Shoulder Bodycon Maxi Dress", Price: 125.4},
			{Name: "Halter Pleated Maxi Dress", Price: 140.8},
		},
	})
	rb := orderReadback(shop, orders.Default.GetOrder("HAND-6003"))
	if !strings.Contains(rb, "Order HAND-6003:\nDouble Clothes Rack: GH₵260.8") ||
		!strings.Contains(rb, "Halter Pleated Maxi Dress: GH₵140.8") ||
		!strings.Contains(rb, "That is GH₵527 in all.") {
		t.Fatalf("the readback should list the items, got %q", rb)
	}
}
