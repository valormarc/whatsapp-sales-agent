package agent

// Marking orders sent from the dispatch reminder itself:
// the reminder carries a button for its own order, a reply
// quoting the reminder binds the mark to that order, and a
// report about the whole waiting set is confirmed once
// instead of answered with the which-order list again. The
// readings that name a report are driven directly here, the
// way the delivery tests drive theirs; the structures under
// them — the button, the quote, the armed question — run
// end to end.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// dispatchRecord is one outbox line with the fields these
// tests assert on: the dry-run id a quote can point at, the
// kind, and the button titles a phone would show.
type dispatchRecord struct {
	ID      string   `json:"id"`
	To      string   `json:"to"`
	Body    string   `json:"body"`
	Kind    string   `json:"kind"`
	Buttons []string `json:"buttons"`
}

func dispatchRecords(t *testing.T, dir, to string) []dispatchRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "data", "outbox.jsonl"))
	if err != nil {
		return nil
	}
	var out []dispatchRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec dispatchRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("outbox line not JSON: %v", err)
		}
		if rec.To == to {
			out = append(out, rec)
		}
	}
	return out
}

// seedStalePaid records a paid order old enough for the
// dispatch watch to push, in the dispatch watch test's shape.
func seedStalePaid(ref, buyer string, now time.Time) {
	orders.Default.RecordOrder(ref, orders.Order{
		From: buyer, Status: "paid", ProductName: "Handmade Beaded Bag",
		Price: 850, Total: 850, MerchantID: MerchantID,
		Fulfilment: "delivery", Location: "Osu",
		CreatedAt: now.Add(-5 * time.Hour).Format(time.RFC3339),
	})
}

func TestDispatchReminderCarriesMarkSentButton(t *testing.T) {
	merchant := "233550007001"
	dir := setupChat(t, merchant, shopWithBag)
	now := time.Date(2026, 11, 1, 15, 0, 0, 0, time.UTC)
	seedStalePaid("EFUA-3001", "233550007002", now)

	CheckDispatchNudges(now)

	var nudge *dispatchRecord
	for _, rec := range dispatchRecords(t, dir, merchant) {
		if strings.Contains(rec.Body, "EFUA-3001") {
			r := rec
			nudge = &r
		}
	}
	if nudge == nil {
		t.Fatalf("the stale order should have been pushed to the merchant")
	}
	if nudge.Kind != "buttons" {
		t.Fatalf("the reminder should be a buttons record, got kind %q", nudge.Kind)
	}
	if len(nudge.Buttons) != 1 || nudge.Buttons[0] != "Mark as sent" {
		t.Fatalf("the reminder's buttons = %v, want exactly [Mark as sent]", nudge.Buttons)
	}
	if !strings.HasPrefix(nudge.Body, "A paid order is still waiting to go out") {
		t.Fatalf("the reminder's body should be the nudge text, got %q", nudge.Body)
	}
}

func TestDispatchSentTapMarksExactlyThatOrder(t *testing.T) {
	owner := "233550002001"
	dir := setupChat(t, owner, deliveryCatalogue)
	c := &chat{dir: dir}
	seedPaidOrder(t, "EFUA-3101", "233550002101")
	seedPaidOrder(t, "EFUA-3102", "233550002102")

	HandleInbound(Inbound{From: owner, ButtonID: "dsent_EFUA-3101"})

	if o := orders.Default.GetOrder("EFUA-3101"); o == nil || o.DeliveryState != "sent" {
		t.Fatalf("the tapped order should stand sent, got %+v", o)
	}
	if o := orders.Default.GetOrder("EFUA-3102"); o == nil || o.DeliveryState != "" {
		t.Fatalf("the other waiting order must not move, got %+v", o)
	}
	if got := strings.Join(c.replies(t, "233550002101"), "\n"); !strings.Contains(got, "has sent your order EFUA-3101") {
		t.Fatalf("the tapped order's buyer should be told it is on its way, got %q", got)
	}
	if got := c.replies(t, "233550002102"); len(got) != 0 {
		t.Fatalf("the other buyer should hear nothing, got %q", got)
	}
	if got := strings.Join(c.replies(t, owner), "\n"); !strings.Contains(got, "Marked as sent: EFUA-3101 (Handmade Beaded Bag). I have told the buyer.") {
		t.Fatalf("the merchant confirmation = %q", got)
	}
}

func TestQuotedDispatchReminderBindsTheMark(t *testing.T) {
	owner := "233550002001"
	dir := setupChat(t, owner, deliveryCatalogue)
	c := &chat{dir: dir}
	shop := catalogue.GetMerchant(MerchantID)
	now := time.Date(2026, 11, 1, 15, 0, 0, 0, time.UTC)
	seedStalePaid("EFUA-3201", "233550002201", now)
	seedStalePaid("EFUA-3202", "233550002202", now)

	CheckDispatchNudges(now)
	c.replies(t, owner) // drain the two nudges

	// The dry-run id of the first order's own reminder, the
	// id her phone would quote back.
	nudgeID := ""
	for _, rec := range dispatchRecords(t, dir, owner) {
		if rec.Kind == "buttons" && strings.Contains(rec.Body, "EFUA-3201") {
			nudgeID = rec.ID
		}
	}
	if nudgeID == "" {
		t.Fatalf("the first order's reminder should be in the outbox with its id")
	}
	q := resolveQuote(Inbound{From: owner, ContextID: nudgeID, ContextFrom: agentNumberForTest()})
	if q == nil || !q.Known || q.Order == nil || q.Order.Reference != "EFUA-3201" {
		t.Fatalf("the quoted reminder should resolve to its order, got %+v", q)
	}
	// Bare "Delivered" reaches merchantMarkSent here the way
	// the delivery tests reach it: the reading is supplied
	// directly, with the turn's quote set the way HandleInbound
	// sets it from ContextID/ContextFrom, because the stub
	// harness has no model to read the turn.
	turnQuotes.put(owner, q)
	defer turnQuotes.del(owner)
	if !inTurn(owner, func() bool {
		return actOnMerchantUnderstanding(owner, shop, MerchantID, "Delivered", &understood{Intent: intentOrderSent})
	}) {
		t.Fatalf("the quoted report should be served")
	}
	if o := orders.Default.GetOrder("EFUA-3201"); o == nil || o.DeliveryState != "sent" {
		t.Fatalf("the quoted order should stand sent, got %+v", o)
	}
	if o := orders.Default.GetOrder("EFUA-3202"); o == nil || o.DeliveryState != "" {
		t.Fatalf("the unquoted order must not move, got %+v", o)
	}
	got := strings.Join(c.replies(t, owner), "\n")
	if strings.Contains(got, "Which order has gone?") {
		t.Fatalf("a quoted report must not be asked which order, got %q", got)
	}
	if !strings.Contains(got, "Marked as sent: EFUA-3201") {
		t.Fatalf("the merchant confirmation = %q", got)
	}
}

// armDispatchAll drives a whole-set report with three orders
// waiting and returns the armed question.
func armDispatchAll(t *testing.T, owner string, c *chat) *YesNoQuestion {
	t.Helper()
	shop := catalogue.GetMerchant(MerchantID)
	seedPaidOrder(t, "EFUA-3301", "233550002301")
	seedPaidOrder(t, "EFUA-3302", "233550002302")
	seedPaidOrder(t, "EFUA-3303", "233550002303")
	if !inTurn(owner, func() bool {
		return actOnMerchantUnderstanding(owner, shop, MerchantID, "all", &understood{Intent: intentOrderSent})
	}) {
		t.Fatalf("the whole-set report should be served")
	}
	s := sessions.get(sessionKey(MerchantID, owner))
	if s == nil || s.YesNoQ == nil || s.YesNoQ.Kind != "dispatchall" {
		t.Fatalf("the whole-set question should be armed, got %+v", s)
	}
	return s.YesNoQ
}

func assertNoneMarked(t *testing.T) {
	t.Helper()
	for _, ref := range []string{"EFUA-3301", "EFUA-3302", "EFUA-3303"} {
		if o := orders.Default.GetOrder(ref); o == nil || o.DeliveryState != "" {
			t.Fatalf("order %s must still be waiting, got %+v", ref, o)
		}
	}
}

func assertAllMarked(t *testing.T, c *chat, owner string) {
	t.Helper()
	for _, ref := range []string{"EFUA-3301", "EFUA-3302", "EFUA-3303"} {
		if o := orders.Default.GetOrder(ref); o == nil || o.DeliveryState != "sent" {
			t.Fatalf("order %s should stand sent, got %+v", ref, o)
		}
	}
	for _, buyer := range []string{"233550002301", "233550002302", "233550002303"} {
		if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "has sent your order") {
			t.Fatalf("buyer %s should be told her order is on its way, got %q", buyer, got)
		}
	}
	if got := strings.Join(c.replies(t, owner), "\n"); !strings.Contains(got, "Marked as sent: all 3 orders. I have told the buyers.") {
		t.Fatalf("the merchant summary = %q", got)
	}
}

func TestDispatchAllArmsOneConfirmationAndYesTapMarksAll(t *testing.T) {
	owner := "233550002001"
	dir := setupChat(t, owner, deliveryCatalogue)
	c := &chat{dir: dir}
	q := armDispatchAll(t, owner, c)

	shown := false
	for _, rec := range dispatchRecords(t, dir, owner) {
		if rec.Kind == "buttons" && rec.Body == "Mark all 3 orders waiting as sent?" {
			shown = true
			if len(rec.Buttons) != 2 || rec.Buttons[0] != "Yes" || rec.Buttons[1] != "Change" {
				t.Fatalf("the confirmation buttons = %v, want [Yes Change]", rec.Buttons)
			}
		}
	}
	if !shown {
		t.Fatalf("the whole-set confirmation should have gone out on buttons")
	}
	assertNoneMarked(t)
	c.replies(t, owner) // drain the question before the answer's replies

	HandleInbound(Inbound{From: owner, ButtonID: "ynq_" + q.ID + "_y"})
	assertAllMarked(t, c, owner)
}

func TestDispatchAllChangeTapAsksWhichOrder(t *testing.T) {
	owner := "233550002001"
	dir := setupChat(t, owner, deliveryCatalogue)
	c := &chat{dir: dir}
	q := armDispatchAll(t, owner, c)
	c.replies(t, owner) // drain the question

	HandleInbound(Inbound{From: owner, ButtonID: "ynq_" + q.ID + "_n"})
	assertNoneMarked(t)
	got := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "Which order has gone? EFUA-3301 (Handmade Beaded Bag), EFUA-3302 (Handmade Beaded Bag), EFUA-3303 (Handmade Beaded Bag). Tell me the reference and I will mark it.") {
		t.Fatalf("a Change answer should re-ask which order, got %q", got)
	}
	_ = dir
}

func TestDispatchAllTypedYesMarksAll(t *testing.T) {
	owner := "233550002001"
	dir := setupChat(t, owner, deliveryCatalogue)
	c := &chat{dir: dir}
	armDispatchAll(t, owner, c)
	c.replies(t, owner) // drain the question

	c.say(t, owner, "yes")
	assertAllMarked(t, c, owner)
	_ = dir
}

func TestDispatchAllWithOneWaitingMarksItDirectly(t *testing.T) {
	owner := "233550002001"
	dir := setupChat(t, owner, deliveryCatalogue)
	c := &chat{dir: dir}
	shop := catalogue.GetMerchant(MerchantID)
	seedPaidOrder(t, "EFUA-3401", "233550002401")

	if !inTurn(owner, func() bool {
		return actOnMerchantUnderstanding(owner, shop, MerchantID, "all", &understood{Intent: intentOrderSent})
	}) {
		t.Fatalf("the whole-set report should be served")
	}
	if o := orders.Default.GetOrder("EFUA-3401"); o == nil || o.DeliveryState != "sent" {
		t.Fatalf("the one waiting order should be marked at once, got %+v", o)
	}
	for _, rec := range dispatchRecords(t, dir, owner) {
		if strings.Contains(rec.Body, "Mark all") {
			t.Fatalf("no confirmation question should be asked for one order, got %q", rec.Body)
		}
	}
	if got := strings.Join(c.replies(t, owner), "\n"); !strings.Contains(got, "Marked as sent: EFUA-3401 (Handmade Beaded Bag). I have told the buyer.") {
		t.Fatalf("the merchant confirmation = %q", got)
	}
}
