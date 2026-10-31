package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// Run-2 regression: the delete-my-data phrase was honoured
// only as the whole message, so a person who asked inside
// an ordinary sentence got no erasure and no signal. The
// phrase now counts where a person actually puts it, and
// narrations and negations still do not trigger it.
func TestDeleteMyDataPhraseShapes(t *testing.T) {
	asks := []string{
		"delete my data",
		"Please delete my data.",
		"I want you to delete my data please",
		"please delete my data and stop messaging me",
		"Hi, delete my data",
		"Could you erase all of my information?",
		"I need you to remove my details",
	}
	for _, s := range asks {
		if !isDeleteMyDataAsk(s) {
			t.Errorf("an ask must be recognised: %q", s)
		}
	}
	notAsks := []string{
		"she said delete my data",
		"do not delete my data",
		"don't delete my data yet",
		"never delete my data",
		"how do I delete my data",
		"what does delete my data mean",
		"I will delete my data tomorrow",
	}
	for _, s := range notAsks {
		if isDeleteMyDataAsk(s) {
			t.Errorf("not an ask, must not trigger erasure: %q", s)
		}
	}
}

// Run-2 HIGH: the lane erased a flagged seller's trust
// record with everything else — one message and her
// strikes and flag were gone, and every future buyer lost
// the warning. The judged record is Stall's own, and it
// stands.
func TestDeleteMyDataKeepsJudgedStrikes(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	seller := "233559999991"
	addStrike(seller)
	addStrike(seller)
	addStrike(seller)
	if !flaggedNumber(seller) {
		t.Fatal("three strikes must flag the number before the test means anything")
	}

	HandleInbound(Inbound{From: seller, Text: "delete my data"})

	if got := strikesOf(seller); got != 3 {
		t.Fatalf("judged strikes must survive a delete-my-data request, got %d", got)
	}
	if !flaggedNumber(seller) {
		t.Fatal("the flag must survive a delete-my-data request")
	}
	rawOut, _ := os.ReadFile(datadir.Path("outbox.jsonl"))
	if !strings.Contains(string(rawOut), "disputes judged on this number") {
		t.Fatal("the seller must be told plainly that the judged record stays")
	}
}

// Run-2 regressions in one lane walk: the dispute field on
// her orders, the package names remembered for her, a
// question another session had armed about her, a relay
// copy under another party, and a preserved corrupt copy
// carrying her number all go with the rest.
func TestDeleteMyDataReachesTheCopies(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233557777778"

	orders.Default.RecordOrder("TEST-DEL-2", orders.Order{
		Reference: "TEST-DEL-2", Status: "paid", From: buyer,
		ProductName: "Handmade Beaded Bag", Price: 850, MerchantID: "demo",
		DisputeBy: buyer,
	})
	// Production writers key package names by shop-and-
	// speaker session key (fulfilment.go), never by the
	// bare number — the third audit found the erasure lane
	// deleting the bare key while the real entry, keyed as
	// here, survived. Seed the production shape.
	packageNames.put(sessionKey("demo", buyer), "Ama")
	merchantSess := &Session{MerchantID: "demo", Stage: "merchant",
		YesNoQ: &YesNoQuestion{ID: "ynq_test", Kind: "inhand", Ref: buyer, Body: "the bag at GH₵850"}}
	sessions.put(sessionKey("demo", stockOwner), merchantSess)
	recordMsg(msgRecord{ID: "del2-relay-1", Party: stockOwner, Author: "agent",
		Body: "Your buyer " + buyer + " asked about the bag"})
	// The ask-buyer relay: her words quoted under the
	// merchant's party with her order's reference but not
	// her number — the third audit found this copy
	// surviving the purge.
	recordMsg(msgRecord{ID: "del2-relay-2", Party: stockOwner, Author: "agent",
		Body: "Your buyer answered about order TEST-DEL-2: \"I can only pay 700.\""})
	corrupt := filepath.Join(datadir.Dir(), "trust.json.corrupt-20260101-000000")
	if err := os.WriteFile(corrupt, []byte(`{"`+buyer+`":{"strikes":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sibling := datadir.Path("outbox.jsonl.1")
	if err := os.WriteFile(sibling, []byte(`{"to":"`+stockOwner+`","body":"buyer `+buyer+` is waiting"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	HandleInbound(Inbound{From: buyer, Text: "I want you to delete my data please"})

	o := orders.Default.GetOrder("TEST-DEL-2")
	if o == nil || o.DisputeBy != "" {
		t.Fatalf("the dispute field must be struck from the anonymised order, got %+v", o)
	}
	if _, ok := packageNames.lookup(sessionKey("demo", buyer)); ok {
		t.Fatal("remembered package names must go")
	}
	if got := sessions.get(sessionKey("demo", stockOwner)); got == nil || got.YesNoQ != nil {
		t.Fatal("a question armed about the deleted buyer must be disarmed on the merchant's session")
	}
	if _, ok := lookupMsg("del2-relay-1", stockOwner); ok {
		t.Fatal("a relay copy naming her under another party must go")
	}
	if _, ok := lookupMsg("del2-relay-2", stockOwner); ok {
		t.Fatal("the ask-buyer relay carrying her order reference must go")
	}
	if _, err := os.Stat(corrupt); !os.IsNotExist(err) {
		t.Fatal("a preserved corrupt copy carrying her number must go")
	}
	rawSibling, _ := os.ReadFile(sibling)
	if strings.Contains(string(rawSibling), buyer) {
		t.Fatal("rotated outbox siblings must be purged too")
	}
}

// Run-2 needs-validation posture, closed in code: a book
// that does not parse is restored from its last known good
// copy instead of reading as empty for the life of the
// process.
func TestLoadBookJSONRestoresFromBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	if err := os.WriteFile(path, []byte("{ torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", []byte(`{"233550000001":{"number":"233550000001","strikes":3,"flagged":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := loadTrustFrom(path)
	if out == nil {
		t.Fatal("a corrupt book with a good backup must restore, not read empty")
	}
}

// Run-2: the outbox purge reaches rotated siblings and
// lines addressed to others that carry her number, and
// leaves everyone else's lines alone.
func TestPurgeOutboxReachesSiblingsAndRelays(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	buyer := "233557777779"
	live := datadir.Path("outbox.jsonl")
	_ = os.WriteFile(live, []byte(`{"to":"`+buyer+`","body":"your order is ready"}`+"\n"+`{"to":"`+stockOwner+`","body":"a buyer paid"}`+"\n"), 0o600)
	sibling := datadir.Path("outbox.jsonl.1")
	_ = os.WriteFile(sibling, []byte(`{"to":"`+stockOwner+`","body":"buyer `+buyer+` asked about delivery"}`+"\n"+`{"to":"`+stockOwner+`","body":"stock arrived"}`+"\n"), 0o600)

	if n := whatsapp.PurgeOutbox(buyer); n != 2 {
		t.Fatalf("two lines belong to the buyer across the files, purged %d", n)
	}
	rawLive, _ := os.ReadFile(live)
	if strings.Contains(string(rawLive), buyer) || !strings.Contains(string(rawLive), "a buyer paid") {
		t.Fatalf("the live file must keep other people's lines only, got %s", rawLive)
	}
	rawSibling, _ := os.ReadFile(sibling)
	if strings.Contains(string(rawSibling), buyer) || !strings.Contains(string(rawSibling), "stock arrived") {
		t.Fatalf("the sibling must keep other people's lines only, got %s", rawSibling)
	}
}

// loadTrustFrom is a tiny seam for the restore test: it
// loads a trust-shaped book from an arbitrary path through
// the same loader the trust book uses.
func loadTrustFrom(path string) map[string]*trustRecord {
	out := map[string]*trustRecord{}
	if !loadBookJSON(path, &out) {
		return nil
	}
	if out["233550000001"] == nil || out["233550000001"].Strikes != 3 {
		return nil
	}
	return out
}
