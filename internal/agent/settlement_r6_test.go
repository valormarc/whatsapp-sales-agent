package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-6 record: a merchant cancellation reversed a
// multi-item booking as ONE recomputed sale, corrupting
// the day book's sales count and giveaway pool. The
// reversal now answers the booking line by line at the
// figures each line was booked with.
func TestCancelReversalAnswersTheBookingLineByLine(t *testing.T) {
	setupChat(t, shopOrdersOwner, shopOrdersSeed)
	c := &chat{seen: map[string]int{}}

	orders.Default.RecordOrder("EFUA-1001", orders.Order{
		From: shopOrdersBuyer, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ProductName: "Handmade Beaded Bag and Leather Purse", Price: 730, Total: 880,
		Status: "paid", MerchantID: "efua", BookedList: 850,
		Items: []orders.OrderItem{
			{Name: "Handmade Beaded Bag", Price: 730, ListPrice: 850},
			{Name: "Leather Purse", Price: 150, ListPrice: 200},
		},
	})
	// The booking the sale made: one sale per line.
	daybook.Default.RecordSale("efua", daybook.Sale{ListPrice: 850, AgreedPrice: 730, Allowance: 300})
	daybook.Default.RecordSale("efua", daybook.Sale{ListPrice: 200, AgreedPrice: 150, Allowance: 300})
	before := daybook.Default.GetDayState("efua", daybook.Options{Allowance: 300})
	if before.Sales != 2 || before.Revenue != 880 || before.GivenAway != 170 {
		t.Fatalf("seeded day book = %+v", before)
	}

	c.say(t, shopOrdersOwner, "cancel order EFUA-1001")
	got := strings.Join(c.replies(t, shopOrdersOwner), "\n")
	if !strings.Contains(got, "is cancelled") {
		t.Fatalf("merchant cancel reply = %q", got)
	}
	after := daybook.Default.GetDayState("efua", daybook.Options{Allowance: 300})
	if after.Sales != 0 || after.Revenue != 0 || after.GivenAway != 0 {
		t.Fatalf("the reversal must answer the booking exactly: day book = %+v", after)
	}
}

// Run-6 record: the journal rebind attached a completed
// sale's identity to the post-completion agreed session —
// the very session completeSale installs, which carries a
// payment URL — and from there to a genuinely new
// purchase. The rebind now refuses a session carrying a
// payment URL, and opening a confirmation on a different
// product sheds the standing identity.
func TestRebindRefusesPostCompletionSession(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	merchant := catalogue.GetMerchant("demo")
	key := sessionKey("demo", "233550000999")
	bag := catalogue.FindByName("demo", "Beaded Bag")
	recordSaleCompletion(key, "sale-done-1", "DEMO-1001", bag.ID, "demo", nil)

	post := &Session{Stage: "agreed", MerchantID: "demo", Product: bag, PaymentURL: "https://pay.example.test/done"}
	rebindSaleIDFromJournal(key, post)
	if post.SaleID != "" {
		t.Fatalf("a post-completion session must not be rebound, got SaleID %q", post.SaleID)
	}
	pre := &Session{Stage: "agreed", MerchantID: "demo", Product: bag}
	rebindSaleIDFromJournal(key, pre)
	if pre.SaleID != "sale-done-1" {
		t.Fatalf("the pre-completion snapshot must still rebind, got %q", pre.SaleID)
	}

	other := &catalogue.Product{ID: "other-thing", Name: "Other Thing", ListPrice: 100, FloorPrice: 90}
	s := &Session{Stage: "agreed", MerchantID: "demo", Product: bag, SaleID: "sale-done-1", PaymentURL: "https://pay.example.test/done"}
	startConfirmation("233550000999", s, merchant, other, 100, false)
	if s.SaleID != "" || s.PaymentURL != "" {
		t.Fatalf("a confirmation on a new product must shed the old identity, got SaleID %q PaymentURL %q", s.SaleID, s.PaymentURL)
	}
}
