package agent

// The merchant's attention list: composed from the books
// alone — disputes first, paid orders waiting to be sent,
// her own unfinished work from the session, orders waiting
// on buyers, and the day's figures to close. The intent
// wiring is exercised through the act itself; the live
// model's classification of her question is proven live,
// as every classification is.

import (
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

const attentionOwner = "233550005111"
const attentionBuyer = "233550005112"

const attentionSeed = `{"merchants":{"efua":{"id":"efua","name":"Efua's Store","shopCode":"EFUA","ownerWhatsApp":"` + attentionOwner + `","payoutPhone":"` + attentionOwner + `","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":1}]}}}`

func attentionOrder(ref, status string, mutate func(*orders.Order)) {
	o := orders.Order{
		From: attentionBuyer, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ProductName: "Handmade Beaded Bag", Price: 730, Total: 730,
		Status: status, MerchantID: "efua",
	}
	if mutate != nil {
		mutate(&o)
	}
	orders.Default.RecordOrder(ref, o)
}

func TestAttentionNothingNeedsHer(t *testing.T) {
	setupChat(t, attentionOwner, attentionSeed)
	merchant := catalogue.GetMerchant("efua")
	got := merchantAttentionText(merchant, nil)
	if !strings.HasPrefix(got, "Nothing needs you just now.") {
		t.Fatalf("empty attention = %q", got)
	}
	if !strings.Contains(got, "Today so far:") || !strings.Contains(got, "0 sale(s) today") {
		t.Fatalf("empty attention lacks the day line: %q", got)
	}
}

func TestAttentionOrdersInUrgencyOrder(t *testing.T) {
	setupChat(t, attentionOwner, attentionSeed)
	merchant := catalogue.GetMerchant("efua")

	attentionOrder("EFUA-1001", "paid", func(o *orders.Order) { o.DisputeState = "open" })
	attentionOrder("EFUA-1002", "paid", nil)
	attentionOrder("EFUA-1003", "pending", nil)

	got := merchantAttentionText(merchant, nil)
	disputeAt := strings.Index(got, "EFUA-1001")
	sendAt := strings.Index(got, "EFUA-1002")
	waitAt := strings.Index(got, "waiting for buyers to pay")
	dayAt := strings.Index(got, "Today so far:")
	if disputeAt < 0 || sendAt < 0 || waitAt < 0 || dayAt < 0 {
		t.Fatalf("attention list missing a section: %q", got)
	}
	if !(disputeAt < sendAt && sendAt < waitAt && waitAt < dayAt) {
		t.Fatalf("attention sections out of order: %q", got)
	}
	for _, want := range []string{"a dispute is open on it", "paid, waiting to be sent", "GH₵730 in all", "Here is what needs your attention:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("attention list lacks %q: %q", want, got)
		}
	}
	// The disputed order is paid and unsent too, but it is
	// counted once, in the dispute section.
	if strings.Count(got, "EFUA-1001") != 1 {
		t.Fatalf("disputed order listed twice: %q", got)
	}
}

func TestAttentionNamesHerUnfinishedWork(t *testing.T) {
	setupChat(t, attentionOwner, attentionSeed)
	merchant := catalogue.GetMerchant("efua")
	s := &Session{
		Onboarding: &OnboardingFlow{Step: "lastprice", Name: "Silk Scarf", ListPrice: 120},
		BurstBatch: []burstItem{{}, {}},
		BurstHeld:  []burstItem{{}},
	}
	got := merchantAttentionText(merchant, s)
	for _, want := range []string{
		"A listing is in progress and not saved yet: Silk Scarf at GH₵120.",
		"2 kept items from your photos are waiting for their last prices.",
		"1 kept photo is waiting to be listed.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("attention list lacks %q: %q", want, got)
		}
	}
}

func TestAttentionActServesTheList(t *testing.T) {
	setupChat(t, attentionOwner, attentionSeed)
	c := &chat{seen: map[string]int{}}
	merchant := catalogue.GetMerchant("efua")
	attentionOrder("EFUA-1002", "paid", nil)
	if !inTurn(attentionOwner, func() bool { return actOnMerchantUnderstanding(attentionOwner, merchant, "efua", "what needs my attention today?", &understood{Intent: intentAttention}) }) {
		t.Fatal("attention act did not serve")
	}
	got := strings.Join(c.replies(t, attentionOwner), "\n")
	if !strings.Contains(got, "EFUA-1002") || !strings.Contains(got, "paid, waiting to be sent") {
		t.Fatalf("attention act reply = %q", got)
	}
}
