package agent

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-4 records, one lane: erasure struck the payment
// session key on orders whose money was still in flight
// (stranding an already-paid order pending forever), and
// two copy families survived the purge — dispute-flow
// notices naming her order under the merchant's party
// with no relay marker and no number, and the ask-buyer
// relay's copy in the outbox.
func TestDeleteMyDataKeepsInflightSettlementAndPurgesByReference(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233557777779"

	ref := "sess_inflight_1"
	orders.Default.RecordOrder("TEST-DEL-4", orders.Order{
		Reference: "TEST-DEL-4", Status: "pending", From: buyer,
		ProductName: "Handmade Beaded Bag", Price: 850, MerchantID: "demo",
		ProviderRef: &ref, SaleID: "sale-del-4",
		CreatedAt: time.Now().Format("2006-01-02") + "T00:00:00Z",
	})
	paidRef := "sess_settled_1"
	orders.Default.RecordOrder("TEST-DEL-5", orders.Order{
		Reference: "TEST-DEL-5", Status: "paid", From: buyer,
		ProductName: "Handmade Beaded Bag", Price: 850, MerchantID: "demo",
		ProviderRef: &paidRef, SaleID: "sale-del-5",
		CreatedAt: time.Now().Format("2006-01-02") + "T00:00:00Z",
	})
	// A dispute-flow notice under the merchant's party:
	// her order's reference, no number, no relay marker.
	recordMsg(msgRecord{ID: "del4-dispute-1", Party: stockOwner, Author: "agent",
		Body: "The buyer confirms your refund for order TEST-DEL-5. The dispute is closed, with no strike."})
	// The ask-buyer relay's outbox copy, addressed to the
	// merchant, her words and the reference, no number.
	outbox := datadir.Path("outbox.jsonl")
	line := `{"to":"` + stockOwner + `","body":"Your buyer answered about order TEST-DEL-4: \"I can only pay 700.\""}` + "\n"
	if err := os.WriteFile(outbox, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	HandleInbound(Inbound{From: buyer, Text: "I want you to delete my data please"})

	pending := orders.Default.GetOrder("TEST-DEL-4")
	if pending == nil || pending.From != "deleted" {
		t.Fatalf("the pending order must still be tombstoned, got %+v", pending)
	}
	if pending.ProviderRef == nil || *pending.ProviderRef != "sess_inflight_1" || pending.SaleID != "sale-del-4" {
		t.Fatalf("an in-flight order must keep its settlement key, got %+v", pending)
	}
	settled := orders.Default.GetOrder("TEST-DEL-5")
	if settled == nil || settled.ProviderRef != nil || settled.SaleID != "" {
		t.Fatalf("a settled order's settlement key is struck with the rest, got %+v", settled)
	}
	if _, ok := lookupMsg("del4-dispute-1", stockOwner); ok {
		t.Fatal("a dispute-flow record naming her order under the merchant's party must go")
	}
	raw, _ := os.ReadFile(outbox)
	if strings.Contains(string(raw), "TEST-DEL-4") {
		t.Fatalf("the relay's outbox copy naming her order must go, outbox holds %q", raw)
	}
}
