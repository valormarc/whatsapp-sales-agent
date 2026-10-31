package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

// walkToConfirm drives a buyer at the Test Shop to the
// confirm step of a bag purchase, with the given package
// name, and leaves her there with the recap unread.
func walkToConfirm(t *testing.T, c *chat, buyer, name string) {
	t.Helper()
	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "Handmade Beaded Bag")
	c.replies(t, buyer)
	c.say(t, buyer, "850")
	c.replies(t, buyer)
	c.say(t, buyer, "Accra") // the shop has no delivery terms: where she is based
	c.replies(t, buyer)
	c.say(t, buyer, name)
	c.replies(t, buyer)
	c.say(t, buyer, "no")
	c.replies(t, buyer)
	s := sessions.get(sessionKey("demo", buyer))
	if s == nil || s.FulfilStep != "confirm" {
		t.Fatalf("session = %+v, want the confirm step", s)
	}
}

// His exact live failure, at the unit layer: at the confirm
// step he quoted the agent's own name question and answered
// it with the right name, twice, and the sale completed with
// the wrong name on the package. The quote itself says which
// part of the order her words belong to: the name she gives
// against it lands, and the order is read back to her again.
func TestQuotedNameAnswerCorrectsThePackageNameAtConfirm(t *testing.T) {
	dir := setupChat(t, "233240000001", shopBagAndHeels)
	c := &chat{dir: dir}
	buyer := "233550007201"

	walkToConfirm(t, c, buyer, "Akosua Mensah")
	questionID := agentMsgID(t, dir, buyer, "What name should go on the package?")
	if questionID == "" {
		t.Fatal("the name question was never sent")
	}
	quoteSay(t, buyer, "Kojo Tsey", questionID, "233209450786")
	got := c.joined(t, buyer)
	if !strings.Contains(got, "Kojo Tsey") {
		t.Fatalf("after the quoted correction, replies = %q, want the recap read back with the new name", got)
	}
	s := sessions.get(sessionKey("demo", buyer))
	if s.PendingFulfil.PackageName != "Kojo Tsey" {
		t.Fatalf("pending name = %q, want Kojo Tsey", s.PendingFulfil.PackageName)
	}

	c.say(t, buyer, "yes")
	c.replies(t, buyer)
	all := ordersOnDisk(t, dir)
	if len(all) != 1 || all[0]["packageName"] != "Kojo Tsey" {
		t.Fatalf("orders = %v, want the order carrying Kojo Tsey", all)
	}
}

// The model's road to the same fix: the stage reading names
// the part she corrected and the value she stated, and the
// confirm step applies it after verifying the value cleans
// as that part. (The live proof exercises the reading
// itself; this pins the application.)
func TestConfirmAppliesAStatedNameCorrectionFromTheReading(t *testing.T) {
	dir := setupChat(t, "233240000001", shopBagAndHeels)
	c := &chat{dir: dir}
	buyer := "233550007202"

	walkToConfirm(t, c, buyer, "Akosua Mensah")
	s := sessions.get(sessionKey("demo", buyer))
	handlePackageDetails(buyer, s, "Make it Kojo Tsey", "make it kojo tsey",
		stageReading{Intent: stageIntentChange, Field: "name", Value: "Kojo Tsey"})
	if s.PendingFulfil.PackageName != "Kojo Tsey" {
		t.Fatalf("pending name = %q, want Kojo Tsey applied from the reading", s.PendingFulfil.PackageName)
	}
	if got := c.joined(t, buyer); !strings.Contains(got, "Kojo Tsey") {
		t.Fatalf("replies = %q, want the recap read back with the corrected name", got)
	}
}

// The name a buyer used last time lives on her paid order,
// not only in the process memory a deploy wipes: after the
// memory is cleared the way a restart clears it, the name
// question still holds the name up.
func TestPackageNameRecoveredFromTheOrderBook(t *testing.T) {
	dir := setupChat(t, "233240000001", shopBagAndHeels)
	c := &chat{dir: dir}
	buyer := "233550007203"

	walkToConfirm(t, c, buyer, "Kojo Tsey")
	c.say(t, buyer, "yes")
	c.replies(t, buyer)
	all := ordersOnDisk(t, dir)
	if len(all) != 1 {
		t.Fatalf("orders = %v, want one", all)
	}
	ref := all[0]["reference"].(string)
	orders.Default.Update(ref, func(o *orders.Order) { o.Status = "paid" })

	// A restart: the running memory goes, the book stays.
	packageNames.replaceAll(map[string]string{})

	// Her place is still on her session, so the second
	// purchase goes straight to the details: the name
	// question is the first thing it asks.
	c.say(t, buyer, "Handmade Beaded Bag")
	c.replies(t, buyer)
	c.say(t, buyer, "850")
	got := c.joined(t, buyer)
	if !strings.Contains(got, "Last time it was Kojo Tsey") {
		t.Fatalf("name question = %q, want the remembered name held up from the order book", got)
	}
}

// A search that finds nothing ends with a taste of what the
// shops do have. Those goods were shown to her, so they are
// recorded as shown: her next turn can point at one, and the
// thinking's facts can hold them up after a greeting.
func TestDiscoveryTasteIsRecordedAsShown(t *testing.T) {
	dir := setupChat(t, "233240000001", shopBagAndHeels)
	_ = dir
	buyer := "233550007205"
	if !actOnDiscovery(buyer, "", &discoveryUnderstanding{Intent: "search", Item: "laptops"}) {
		t.Fatal("the no-match search was not served")
	}
	pre := sessions.get(sessionKey("", buyer))
	if pre == nil || len(pre.DiscoveryShown) == 0 {
		t.Fatalf("session = %+v, want the tasted goods recorded as shown", pre)
	}
}

// The front door is for a cold arrival: a buyer who opened
// with business and greets the agent mid-search is in a
// conversation already, and the door must not open over it.
func TestDoorStaysShutMidDiscovery(t *testing.T) {
	dir := setupChat(t, "233240000001", shopBagAndHeels)
	c := &chat{dir: dir}
	buyer := "233550007206"

	c.say(t, buyer, "Do you have laptops?")
	c.replies(t, buyer)
	c.say(t, buyer, "Hello")
	got := c.replies(t, buyer)
	if doorShown.get(buyer) {
		t.Fatal("the front door opened over a conversation already under way")
	}
	for _, r := range got {
		if strings.Contains(r, "tap Buy") {
			t.Fatalf("replies = %q, want the search answered, not the front door", got)
		}
	}
	_ = dir
}

// And when the held-up name is there, "same" said the way a
// person says it ("Same message", answering the name
// question) adopts it, instead of being printed on the
// package as if it were a name.
func TestSameAnswerAdoptsTheRememberedName(t *testing.T) {
	dir := setupChat(t, "233240000001", shopBagAndHeels)
	c := &chat{dir: dir}
	buyer := "233550007204"

	walkToConfirm(t, c, buyer, "Kojo Tsey")
	c.say(t, buyer, "yes")
	c.replies(t, buyer)
	all := ordersOnDisk(t, dir)
	ref := all[0]["reference"].(string)
	orders.Default.Update(ref, func(o *orders.Order) { o.Status = "paid" })
	packageNames.replaceAll(map[string]string{})

	c.say(t, buyer, "Handmade Beaded Bag")
	c.replies(t, buyer)
	c.say(t, buyer, "850")
	c.replies(t, buyer)
	c.say(t, buyer, "Same message")
	c.replies(t, buyer)
	c.say(t, buyer, "no")
	got := c.joined(t, buyer)
	if !strings.Contains(got, "Kojo Tsey") || strings.Contains(got, "name Same message") {
		t.Fatalf("recap = %q, want the remembered name on the package, not her words about it", got)
	}
	s := sessions.get(sessionKey("demo", buyer))
	if s.PendingFulfil.PackageName != "Kojo Tsey" {
		t.Fatalf("pending name = %q, want Kojo Tsey", s.PendingFulfil.PackageName)
	}
}
