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
