package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-3 record: reissueLink wrote Status='pending' from
// its pre-mint copy unconditionally, reopening an order
// that had settled or been cancelled while the fresh link
// was minting. The write now transitions only a stored
// order that is still open; a moved-on order is answered
// from its stored state and no button is sent.
func TestReissueDoesNotReopenAnOrderThatSettledMeanwhile(t *testing.T) {
	merchant := "233550001831"
	shopJSON := strings.Replace(shopWithBag, `"dailyGiveawayAllowance":300`, `"dailyGiveawayAllowance":300,"payoutPhone":"233550001831"`, 1)
	dir := setupChat(t, merchant, shopJSON)
	c := &chat{dir: dir}
	buyer := "233550001832"
	seedOpenOrder(t, "TEST-3101", buyer, 1)

	// The order settles inside the mint window: the
	// stored record is paid before the re-issue's write.
	orders.Default.Update("TEST-3101", func(o *orders.Order) { o.Status = "paid" })
	stale := orders.Default.GetOrder("TEST-3101")
	stale.Status = "pending" // the caller's pre-mint copy

	if !reissueLink(buyer, &Session{Stage: "agreed"}, catalogue.GetMerchant(MerchantID), stale) {
		t.Fatal("the re-issue must still answer the buyer")
	}
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "already been paid") {
		t.Fatalf("a settled order must be told as settled, got %q", got)
	}
	if strings.Contains(got, "fresh one") {
		t.Fatalf("no fresh link may be offered for a settled order, got %q", got)
	}
	o := orders.Default.GetOrder("TEST-3101")
	if o == nil || o.Status != "paid" || o.LinkAttempts != 1 {
		t.Fatalf("the stored order must stand paid with its attempts untouched, got %+v", o)
	}
}

// Run-5 record (medium): the resume road's rebind was
// unconditional — an order that ended or settled during
// the mint was rebound to a brand-new live session and
// its buyer sent a working pay button for it. The rebind
// is now conditional on the stored record still standing
// open, decided inside the book's lock, exactly as the
// re-issue road has done since run-3.
func TestResumeDoesNotRebindAnOrderThatEndedMeanwhile(t *testing.T) {
	merchant := "233550001851"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550001852"
	realRef := "PAY-REAL-9"
	orders.Default.RecordOrder("TEST-3103", orders.Order{
		From: buyer, Status: "pending", ProductName: "Handmade Beaded Bag",
		Price: 850, Total: 850, ProviderRef: &realRef, DryRun: false,
		MerchantID: MerchantID,
	})
	// The caller's copy says the order is open; the
	// stored record has since been cancelled (the
	// merchant ended it inside the mint window).
	stale := orders.Default.GetOrder("TEST-3103")
	orders.Default.Update("TEST-3103", func(o *orders.Order) { o.Status = "cancelled" })

	resumeAgreementOrder(buyer, &Session{Stage: "agreed", MerchantID: MerchantID}, catalogue.GetMerchant(MerchantID), stale)

	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "has ended") {
		t.Fatalf("an order that ended meanwhile must be told as ended, got %q", got)
	}
	if strings.Contains(got, "pay.example.test") {
		t.Fatalf("no pay button may be sent for an ended order, got %q", got)
	}
	stored := orders.Default.GetOrder("TEST-3103")
	if stored == nil || stored.Status != "cancelled" || stored.ProviderRef == nil || *stored.ProviderRef != realRef || stored.DryRun {
		t.Fatalf("the ended order's session binding must stand untouched, got %+v", stored)
	}
}

// Run-3 record: the resume road updated ProviderRef
// alone, leaving a stale DryRun flag from an earlier mint
// riding on whatever session came next. Both session
// attributes now rebind together at every re-mint — here
// a dry re-mint (checkout unconfigured in the harness)
// must clear the real reference AND set the flag.
func TestResumeRebindsDryRunWithTheReference(t *testing.T) {
	merchant := "233550001841"
	dir := setupChat(t, merchant, shopWithBag)
	_ = dir
	buyer := "233550001842"
	realRef := "PAY-REAL-1"
	orders.Default.RecordOrder("TEST-3102", orders.Order{
		From: buyer, Status: "link_failed", ProductName: "Handmade Beaded Bag",
		Price: 850, Total: 850, ProviderRef: &realRef, DryRun: false,
		MerchantID: MerchantID,
	})
	o := orders.Default.GetOrder("TEST-3102")
	s := &Session{Stage: "agreed", MerchantID: MerchantID}
	resumeAgreementOrder(buyer, s, catalogue.GetMerchant(MerchantID), o)

	got := orders.Default.GetOrder("TEST-3102")
	if got == nil || !got.DryRun || got.ProviderRef != nil {
		t.Fatalf("a dry re-mint must rebind both attributes (DryRun true, no reference), got %+v", got)
	}
}
