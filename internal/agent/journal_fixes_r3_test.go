package agent

import (
	"os"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-3 record: reconciliation returned every journalled
// line, including lines the take never reached — the
// journal held intent, and a crash inside the take loop
// left intent larger than the movement. Only the lines
// marked taken come back now.
func TestJournalReconciliationReturnsOnlyTakenLines(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	if _, err := catalogue.SetStock("demo", "Handmade Beaded Bag", 5); err != nil {
		t.Fatal(err)
	}
	// Intent was two units; the take landed one before
	// the crash, and no order stands.
	recordSaleCompletion(sessionKey("demo", "233556666670"), "sale-part-1", "TEST-PART-1", bag.ID, "demo",
		[]orders.StockLine{{Name: "Handmade Beaded Bag", Units: 2}})
	if !catalogue.TakeStock("demo", "Handmade Beaded Bag", 1) {
		t.Fatal("the partial take must succeed in the setup")
	}
	recordJournalTake("TEST-PART-1", orders.StockLine{Name: "Handmade Beaded Bag", Units: 1})

	reconcileSaleJournal()
	if got := catalogue.FindByName("demo", "Handmade Beaded Bag"); got == nil || got.Stock != 5 {
		t.Fatalf("only the taken unit may come back (stock 5), got %+v", got)
	}
}

// Entries written before take marks existed carry intent
// only; reconciliation keeps their original semantics and
// returns their lines.
func TestJournalReconciliationKeepsLegacyEntriesWhole(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	if _, err := catalogue.SetStock("demo", "Handmade Beaded Bag", 4); err != nil {
		t.Fatal(err)
	}
	legacy := `[{"key":"demo|233556666671","saleId":"sale-legacy-1","reference":"TEST-LEGACY-1","productId":"beaded-bag","shopId":"demo","lines":[{"name":"Handmade Beaded Bag","units":1}],"at":"2026-10-30T00:00:00Z"}]`
	if err := os.WriteFile(datadir.Path("salecompletions.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if !catalogue.TakeStock("demo", "Handmade Beaded Bag", 1) {
		t.Fatal("the take must succeed in the setup")
	}
	reconcileSaleJournal()
	if got := catalogue.FindByName("demo", "Handmade Beaded Bag"); got == nil || got.Stock != 4 {
		t.Fatalf("a legacy entry's line must come back (stock 4), got %+v", got)
	}
	// And the persisted claim means a second boot gives
	// nothing again.
	reconcileSaleJournal()
	if got := catalogue.FindByName("demo", "Handmade Beaded Bag"); got == nil || got.Stock != 4 {
		t.Fatalf("reconciliation must give back once only, got %+v", got)
	}
}
