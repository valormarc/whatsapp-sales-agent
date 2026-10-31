package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/wants"
)

// Regression for the audit finding that the privacy page's
// "delete my data" promise was implemented nowhere: the ask
// was answered with the browsing fallback and every book
// kept the buyer verbatim. The lane now erases her across
// the books and says so.
func TestDeleteMyDataErasesTheBuyer(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233557777777"

	orders.Default.RecordOrder("TEST-DEL-1", orders.Order{
		Reference: "TEST-DEL-1", Status: "paid", From: buyer,
		ProductName: "Handmade Beaded Bag", Price: 850, MerchantID: "demo",
		Location: "Osu, Accra", Collector: "Ama Serwaa", PackageName: "Ama",
		Note: "call when close", DeliveryPhone: buyer, Landmark: "near the mall",
	})
	if !wants.Default.Record("demo", buyer, "a beaded bag in red") {
		t.Fatal("the want should record")
	}
	recordMsg(msgRecord{ID: "del-msg-1", Party: buyer, Author: "user", Body: "do you have the beaded bag"})
	if _, ok := lookupMsg("del-msg-1", buyer); !ok {
		t.Fatal("the seeded message must resolve before deletion")
	}

	HandleInbound(Inbound{From: buyer, Text: "delete my data"})

	rawOut, _ := os.ReadFile(datadir.Path("outbox.jsonl"))
	joined := string(rawOut)
	if !strings.Contains(joined, "deleted the personal information") {
		t.Fatalf("the buyer must be told her data was deleted, got:\n%s", joined)
	}

	o := orders.Default.GetOrder("TEST-DEL-1")
	if o == nil {
		t.Fatal("the order itself survives as the shop's anonymised record")
	}
	if o.From == buyer || o.Location != "" || o.Collector != "" || o.PackageName != "" || o.Note != "" || o.DeliveryPhone != "" || o.Landmark != "" {
		t.Fatalf("every personal detail must be struck from the order, got %+v", o)
	}
	if o.ProductName != "Handmade Beaded Bag" || o.Price != 850 {
		t.Fatal("the anonymised record keeps only goods and amounts")
	}
	if got := wants.Default.List("demo"); len(got) != 0 {
		t.Fatalf("her wants must be gone, got %+v", got)
	}
	if _, ok := lookupMsg("del-msg-1", buyer); ok {
		t.Fatal("her messages must no longer resolve after deletion")
	}
	raw, _ := os.ReadFile(datadir.Path("messages.jsonl"))
	if strings.Contains(string(raw), "do you have the beaded bag") {
		t.Fatal("her message text must be gone from the message book file")
	}
	if matched := wants.Default.MatchAndMark("demo", "Handmade Beaded Bag"); len(matched) != 0 {
		t.Fatal("a later stocking must never alert a deleted buyer")
	}
}
