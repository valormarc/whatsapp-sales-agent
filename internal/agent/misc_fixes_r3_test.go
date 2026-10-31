package agent

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-3 record: fact equality folded a photo into a burst
// item even when the identity judge had said "different".
func TestBurstFoldNeverOverridesADifferentVerdict(t *testing.T) {
	stubDraftListing(t, func(caption, openName string) *model.ListingDraft {
		return &model.ListingDraft{Name: openName, SameAsOpen: "different"}
	})
	item := burstItem{Name: "Bag", Description: "Beaded", ListPrice: 100, Unit: "", BaseQty: 0}
	draft := throwDraft{Name: "Bag", Description: "Beaded", ListPrice: 100}
	if !sameBurstFacts(item, draft) {
		t.Fatal("the setup must have equal facts, or the test proves nothing")
	}
	if burstFoldOK("data:image/jpeg;base64,x", "a bag", item, draft, "") {
		t.Fatal("a different verdict must never fold on fact equality")
	}
}

// Run-3 record: a merchant's "set area fee" command — a
// place and a figure, a quote answer's own shape — was
// consumed as the oldest open quote's answer.
func TestAreaFeeCommandIsNotAFeeQuoteAnswer(t *testing.T) {
	merchant := "233550003041"
	setupChat(t, merchant, shopQuoteStyle)
	openFeeQuote(feeQuote{ShopID: "demo", Buyer: "233550003042", Item: "Handmade Beaded Bag", Price: 850, Place: "Osu"})
	m := catalogue.GetMerchant("demo")
	msg := "set area fee Kasoa 80"
	if inTurn(merchant, func() bool { return handleMerchantFeeQuote(merchant, m, msg, strings.ToLower(msg)) }) {
		t.Fatal("a standing-fee command must not be consumed as a quote answer")
	}
	if n := len(openFeeQuotes("demo")); n != 1 {
		t.Fatalf("the quote must still stand open, got %d", n)
	}
}

// Run-3 record: "not mine" at the signup holder
// confirmation became the MoMo holder name itself.
func TestConfirmHolderNotMineIsARefusal(t *testing.T) {
	setupChat(t, "233550003051", shopWithBag)
	s := &Session{Signup: &SignupFlow{Step: "confirmholder", Name: "Test Shop", HolderName: "Akosua Mensah"}}
	advanceSignup("233550003051", s, "not mine", "not mine")
	if s.Signup.Step != "holder" {
		t.Fatalf("a refusal must return to the holder step, got %q", s.Signup.Step)
	}
	if s.Signup.HolderName == "not mine" {
		t.Fatal("the refusal must never be recorded as the holder name")
	}
}

// Run-3 record: cancelling an erased buyer's order sent
// its notice to the literal "deleted" and reported her
// as told.
func TestCancelOfAnErasedBuyersOrderTellsNoTombstone(t *testing.T) {
	dir := setupChat(t, stockOwner, shopWithBag)
	c := &chat{dir: dir}
	orders.Default.RecordOrder("TEST-CXL-1", orders.Order{
		Reference: "TEST-CXL-1", Status: "pending", From: "deleted",
		ProductName: "Handmade Beaded Bag", Price: 850, MerchantID: "demo",
		CreatedAt: time.Now().Format("2006-01-02") + "T00:00:00Z",
	})
	cancelMerchantOrder(stockOwner, catalogue.GetMerchant("demo"), "TEST-CXL-1")
	raw, _ := os.ReadFile(datadir.Path("outbox.jsonl"))
	if strings.Contains(string(raw), `"to":"deleted"`) {
		t.Fatal("no notice may be addressed to the tombstone")
	}
	got := strings.Join(c.replies(t, stockOwner), "\n")
	if !strings.Contains(got, "could not be told") {
		t.Fatalf("the merchant must hear the buyer could not be told, got %q", got)
	}
}

// Run-3 record: cancelling a link_failed order reversed
// a day-book sale that was never booked, erasing a
// genuine sale's figures.
func TestCancelOfALinkFailedOrderLeavesTheDayBookAlone(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	daybook.Default.RecordSale("demo", daybook.Sale{ListPrice: 850, AgreedPrice: 850, Allowance: 300})
	before := daybook.Default.GetDayState("demo", daybook.Options{Allowance: 300})
	orders.Default.RecordOrder("TEST-CXL-2", orders.Order{
		Reference: "TEST-CXL-2", Status: "link_failed", From: "233550003061",
		ProductName: "Handmade Beaded Bag", Price: 500, MerchantID: "demo",
		CreatedAt: time.Now().Format("2006-01-02") + "T00:00:00Z",
	})
	cancelMerchantOrder(stockOwner, catalogue.GetMerchant("demo"), "TEST-CXL-2")
	after := daybook.Default.GetDayState("demo", daybook.Options{Allowance: 300})
	if after.Sales != before.Sales || after.Revenue != before.Revenue {
		t.Fatalf("an unbooked order's cancellation must not touch the book: before %+v after %+v", before, after)
	}
}

// Run-3 record: the pending-pic slot came from the photo
// count, overwriting a surviving photo when numbering
// had a gap.
func TestNextPhotoSlotSkipsGaps(t *testing.T) {
	p := &catalogue.Product{ID: "bag", Photos: []string{
		"photos/demo/bag-1.jpg", "photos/demo/bag-3.jpg",
	}}
	if got := nextPhotoSlot(p); got != 4 {
		t.Fatalf("the next slot must be one past the highest, got %d", got)
	}
}

// Run-3 record: the sweep's fee-quote expiry pre-pass
// was unbounded. One pass now actions at most the cap.
func TestFeeQuoteExpiryIsBoundedPerPass(t *testing.T) {
	merchant := "233550003071"
	setupChat(t, merchant, shopQuoteStyle)
	for i := 0; i < maxPrePassPerSweep+5; i++ {
		// A distinct buyer each: opening a quote cancels
		// a buyer's earlier open one.
		openFeeQuote(feeQuote{ShopID: "demo", Buyer: "2335500041" + string(rune('0'+i/10)) + string(rune('0'+i%10)), Item: "Bag", Price: 10, Place: "Osu"})
	}
	all := loadFeeQuotes()
	for i := range all {
		all[i].CreatedAt = time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	}
	saveFeeQuotes(all)
	CheckFeeQuotes(time.Now().UTC())
	expired, open := 0, 0
	for _, q := range loadFeeQuotes() {
		if q.Status == "expired" {
			expired++
		} else if q.Status == "open" {
			open++
		}
	}
	if expired != maxPrePassPerSweep || open != 5 {
		t.Fatalf("one pass must expire exactly the cap (%d) and carry the rest, got expired=%d open=%d", maxPrePassPerSweep, expired, open)
	}
}
