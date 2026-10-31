package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-6 records, fulfilment family: the same-details
// adoption left the delivery form's token live; a stale
// armed question's yes fell through into the listing
// walk; a missing order could never be confirmed
// received while a missing report was accepted on a
// never-dispatched order; the pic-request road ignored
// the photo cap.

func TestAdoptSameDetailsSpendsTheFormToken(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233550000891"
	// A remembered fulfilment from a paid order.
	orders.Default.RecordOrder("DEMO-2001", orders.Order{
		From: buyer, CreatedAt: time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
		ProductName: "Beaded Bag", Price: 700, Status: "paid", MerchantID: "demo",
		Fulfilment: "delivery", Location: "Osu", PackageName: "Ama Serwaa",
	})
	merchant := catalogue.GetMerchant("demo")
	s := &Session{
		Stage: "fulfilment", MerchantID: "demo", FulfilStep: "pkgname",
		PendingFlow: "tok-123", FlowKind: flowKindDelivery,
		Product: catalogue.FindByName("demo", "Beaded Bag"),
	}
	sessions.put(sessionKey("demo", buyer), s)
	inTurn(buyer, func() bool {
		if !adoptSameDetails(buyer, s, merchant) {
			t.Fatal("the adoption must take the turn")
		}
		return true
	})
	got := sessions.get(sessionKey("demo", buyer))
	if got.PendingFlow != "" || got.FlowKind != "" {
		t.Fatalf("the adoption must spend the form token, got PendingFlow=%q FlowKind=%q", got.PendingFlow, got.FlowKind)
	}
}

func TestStaleArmedYesDoesNotSaveTheListing(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	c := &chat{seen: map[string]int{}}
	// The merchant is mid-walk at the confirm step while
	// a foreign (remove) question stands armed but stale
	// — its product is already gone.
	flow := &OnboardingFlow{Step: "confirm", Name: "Silk Scarf", ListPrice: 200, FloorPrice: 150}
	s := &Session{Stage: "onboarding", MerchantID: "demo", Onboarding: flow}
	s.YesNoQ = &YesNoQuestion{Kind: "remove", Ref: "Silk Scarf", Body: "Remove the Silk Scarf?"}
	sessions.put(sessionKey("demo", stockOwner), s)

	c.say(t, stockOwner, "yes")
	got := strings.Join(c.replies(t, stockOwner), "\n")
	if strings.Contains(got, "is live") {
		t.Fatalf("the stale question's yes must not save the listing: %q", got)
	}
	if !strings.Contains(got, "no longer open") {
		t.Fatalf("the stale word must be consumed plainly: %q", got)
	}
	if p := catalogue.FindByName("demo", "Silk Scarf"); p != nil {
		t.Fatal("no product may be saved by a stale yes")
	}
}

func TestMissingOrderCanBeConfirmedReceived(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233550000892"
	merchant := catalogue.GetMerchant("demo")
	orders.Default.RecordOrder("DEMO-2002", orders.Order{
		From: buyer, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ProductName: "Beaded Bag", Price: 700, Status: "paid", MerchantID: "demo",
		DeliveryState: "missing", SentAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		DisputeState: "open", DisputeAt: time.Now().UTC().Format(time.RFC3339),
	})
	inTurn(buyer, func() bool {
		return buyerConfirmReceived(buyer, merchant, "demo", "DEMO-2002")
	})
	o := orders.Default.GetOrder("DEMO-2002")
	if o.DeliveryState != "received" {
		t.Fatalf("a missing order must be confirmable, state = %q", o.DeliveryState)
	}
	if o.DisputeState != "resolved_received" {
		t.Fatalf("its dispute must close clean, state = %q", o.DisputeState)
	}
}

func TestMissingReportOnAnUndispatchedPaidOrderIsTaken(t *testing.T) {
	// The seventh audit re-argued the missing-report
	// decline on the merits and carried it: taking the
	// report never required advancing the state, and the
	// "missing" stamp silently removed a paid order from
	// every dispatch worklist while no dispute opened to
	// arbitrate it. The settled posture (see
	// buyerReportMissing): the report is always taken and
	// the seller always told at once, as a chase in the
	// order's true position, and only a dispatched order
	// enters the missing state. This test pins that
	// posture; it replaces the run-6 pin, which asserted
	// the state advance itself.
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233550000893"
	merchant := catalogue.GetMerchant("demo")
	orders.Default.RecordOrder("DEMO-2003", orders.Order{
		From: buyer, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ProductName: "Beaded Bag", Price: 700, Status: "paid", MerchantID: "demo",
	})
	inTurn(buyer, func() bool {
		return buyerReportMissing(buyer, merchant, "demo", "DEMO-2003")
	})
	o := orders.Default.GetOrder("DEMO-2003")
	if o.DeliveryState != "" {
		t.Fatalf("an undispatched order stays on the waiting lists, state = %q", o.DeliveryState)
	}
}

func TestPicRequestRespectsThePhotoCap(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	shop := catalogue.GetMerchant("demo")
	p := catalogue.FindByName("demo", "Beaded Bag")
	full := []string{"1.jpg", "2.jpg", "3.jpg", "4.jpg", "5.jpg", "6.jpg"}
	if err := catalogue.SetProductPhotos("demo", p.ID, full); err != nil {
		t.Fatal(err)
	}
	picRequests["demo"] = picRequest{Buyer: "233550000894", ProductID: p.ID, At: time.Now()}
	c := &chat{seen: map[string]int{}}
	handled := inTurn(stockOwner, func() bool {
		return servePendingPicRequest(stockOwner, shop, "media-capped")
	})
	if !handled {
		t.Fatal("the capped request must still be claimed and answered")
	}
	got := strings.Join(c.replies(t, stockOwner), "\n")
	if !strings.Contains(got, "already holds the most photos") {
		t.Fatalf("the merchant must hear the honest cap refusal: %q", got)
	}
	after := catalogue.FindByName("demo", "Beaded Bag")
	if len(after.Photos) != maxProductPhotos {
		t.Fatalf("the product must hold the cap and no more, got %d", len(after.Photos))
	}
}
