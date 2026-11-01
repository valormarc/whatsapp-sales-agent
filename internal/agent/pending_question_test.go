package agent

// Question ownership, replaying the live failure: the
// merchant, mid-listing with kept photos standing, quoted
// her attention answer and said "Clear them". The agent
// opened a clear confirmation AND pushed the walk's list
// price question in the same turn; her answers to the
// confirmation ("Both", "Clear", "Yes clear") were then
// eaten by the price step and the clear never executed.
// These tests run the same turns end to end through
// HandleInbound: one question at a time, her answer read
// inside the question she is answering, and the day's
// figures rounded at the facts boundary so no answer can
// carry a raw float.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/insights"
	"github.com/valormarc/hackathon-agent/internal/money"
)

const clearWorkOwner = "233550009001"

// seedClearWorkState stands the merchant where the live
// merchant stood: a listing in progress at its list price
// step ("Lace Nightdress") and five kept photos waiting to
// be listed.
func seedClearWorkState(t *testing.T, owner string) *Session {
	t.Helper()
	s := &Session{
		Stage:      "merchant",
		MerchantID: MerchantID,
		Onboarding: &OnboardingFlow{Step: "list", Name: "Lace Nightdress", Description: "A lace nightdress."},
		BurstHeld:  []burstItem{{}, {}, {}, {}, {}},
	}
	sessions.put(sessionKey(MerchantID, owner), s)
	return s
}

// attentionAnswerID serves the attention answer the way its
// act serves it and returns the outbox id of the message
// that carried it, the id her phone would quote back.
func attentionAnswerID(t *testing.T, dir, owner string, c *chat) string {
	t.Helper()
	merchant := catalogue.GetMerchant(MerchantID)
	if !inTurn(owner, func() bool {
		return actOnMerchantUnderstanding(owner, merchant, MerchantID, "what needs my attention?", &understood{Intent: intentAttention})
	}) {
		t.Fatalf("the attention act should serve")
	}
	got := c.joined(t, owner)
	for _, want := range []string{"A listing is in progress and not saved yet: Lace Nightdress.", "5 kept photos are waiting to be listed."} {
		if !strings.Contains(got, want) {
			t.Fatalf("the attention answer should name %q, got %q", want, got)
		}
	}
	recs := dispatchRecords(t, dir, owner)
	if len(recs) == 0 {
		t.Fatalf("the attention answer should be in the outbox")
	}
	return recs[len(recs)-1].ID
}

func lastOutboxID(t *testing.T, dir, owner string) string {
	t.Helper()
	recs := dispatchRecords(t, dir, owner)
	if len(recs) == 0 {
		t.Fatalf("the outbox should hold a message to quote")
	}
	return recs[len(recs)-1].ID
}

func allBodiesTo(t *testing.T, dir, owner string) string {
	t.Helper()
	var bodies []string
	for _, rec := range dispatchRecords(t, dir, owner) {
		bodies = append(bodies, rec.Body)
	}
	return strings.Join(bodies, "\n")
}

// openClearConfirmation runs the screenshot's first two
// turns: the attention answer, then her quoted "Clear them"
// against it. It returns the confirmation's outbox id.
func openClearConfirmation(t *testing.T, dir, owner string, c *chat) string {
	t.Helper()
	attnID := attentionAnswerID(t, dir, owner, c)
	HandleInbound(Inbound{From: owner, Text: "Clear them", ContextID: attnID, ContextFrom: agentNumberForTest()})
	replies := c.replies(t, owner)
	if len(replies) != 1 {
		t.Fatalf("the clear turn should send exactly one message — the confirmation — got %d: %q", len(replies), replies)
	}
	if !strings.Contains(replies[0], "Shall I clear them?") || !strings.Contains(replies[0], "Lace Nightdress") || !strings.Contains(replies[0], "5 kept photos") {
		t.Fatalf("the one message should be the clear confirmation naming both targets, got %q", replies[0])
	}
	if strings.Contains(replies[0], "list price") {
		t.Fatalf("the walk's price question must not ride in the same turn, got %q", replies[0])
	}
	return lastOutboxID(t, dir, owner)
}

func TestClearChoiceReplayBothClearsBothTargets(t *testing.T) {
	dir := setupChat(t, clearWorkOwner, shopWithBag)
	c := &chat{dir: dir}
	s := seedClearWorkState(t, clearWorkOwner)

	confID := openClearConfirmation(t, dir, clearWorkOwner, c)
	if s.PendingMerchantQ == nil || s.PendingMerchantQ.Kind != "clearwork" {
		t.Fatalf("the confirmation should stand as the pending question, got %+v", s.PendingMerchantQ)
	}

	HandleInbound(Inbound{From: clearWorkOwner, Text: "Both", ContextID: confID, ContextFrom: agentNumberForTest()})
	got := c.joined(t, clearWorkOwner)
	if !strings.Contains(got, "Cleared:") || !strings.Contains(got, "the Lace Nightdress listing is dropped") || !strings.Contains(got, "the 5 kept photos are gone") {
		t.Fatalf("a quoted Both should clear both targets, got %q", got)
	}
	if s.Onboarding != nil {
		t.Fatalf("the listing should be dropped, got %+v", s.Onboarding)
	}
	if len(s.BurstHeld) != 0 {
		t.Fatalf("the kept photos should be cleared, got %d standing", len(s.BurstHeld))
	}
	if s.PendingMerchantQ != nil {
		t.Fatalf("the question should be settled, got %+v", s.PendingMerchantQ)
	}
	if all := allBodiesTo(t, dir, clearWorkOwner); strings.Contains(all, "I could not read that as a price") {
		t.Fatalf("no price reader may ever see her answer, got %q", all)
	}
}

func TestClearChoiceContinueResumesTheWalk(t *testing.T) {
	dir := setupChat(t, clearWorkOwner, shopWithBag)
	c := &chat{dir: dir}
	s := seedClearWorkState(t, clearWorkOwner)

	openClearConfirmation(t, dir, clearWorkOwner, c)

	c.say(t, clearWorkOwner, "continue")
	got := c.joined(t, clearWorkOwner)
	if !strings.Contains(got, "Alright, nothing is cleared.") {
		t.Fatalf("a continue should settle the question with nothing cleared, got %q", got)
	}
	if !strings.Contains(got, "What is the list price for Lace Nightdress?") {
		t.Fatalf("the walk should then ask its price question, got %q", got)
	}
	if s.Onboarding == nil || len(s.BurstHeld) != 5 || s.PendingMerchantQ != nil {
		t.Fatalf("everything should stand and the question be settled, got walk=%+v held=%d q=%+v", s.Onboarding, len(s.BurstHeld), s.PendingMerchantQ)
	}

	c.say(t, clearWorkOwner, "450")
	got = c.joined(t, clearWorkOwner)
	if strings.Contains(got, "could not read that as a price") {
		t.Fatalf("the resumed walk should accept her figure, got %q", got)
	}
	if !strings.Contains(got, "last price for Lace Nightdress") {
		t.Fatalf("the walk should move on to its last price question, got %q", got)
	}
	if s.Onboarding == nil || s.Onboarding.ListPrice != 450 {
		t.Fatalf("the figure should stand as the list price, got %+v", s.Onboarding)
	}
}

func TestClearChoiceQuotedYesClearExecutes(t *testing.T) {
	dir := setupChat(t, clearWorkOwner, shopWithBag)
	c := &chat{dir: dir}
	s := seedClearWorkState(t, clearWorkOwner)

	confID := openClearConfirmation(t, dir, clearWorkOwner, c)

	HandleInbound(Inbound{From: clearWorkOwner, Text: "Yes clear", ContextID: confID, ContextFrom: agentNumberForTest()})
	got := c.joined(t, clearWorkOwner)
	if !strings.Contains(got, "Cleared:") || !strings.Contains(got, "the Lace Nightdress listing is dropped") || !strings.Contains(got, "the 5 kept photos are gone") {
		t.Fatalf("a quoted Yes clear should execute the clear, got %q", got)
	}
	if s.Onboarding != nil || len(s.BurstHeld) != 0 {
		t.Fatalf("both targets should be gone, got walk=%+v held=%d", s.Onboarding, len(s.BurstHeld))
	}
	if all := allBodiesTo(t, dir, clearWorkOwner); strings.Contains(all, "I could not read that as a price") {
		t.Fatalf("no price reader may ever see her answer, got %q", all)
	}
}

func TestClearChoiceBareClearExecutesUnquoted(t *testing.T) {
	dir := setupChat(t, clearWorkOwner, shopWithBag)
	c := &chat{dir: dir}
	s := seedClearWorkState(t, clearWorkOwner)

	openClearConfirmation(t, dir, clearWorkOwner, c)

	// No quote this time: the pending question is the live
	// one by recency, and it owns the answer the same way.
	c.say(t, clearWorkOwner, "Clear")
	got := c.joined(t, clearWorkOwner)
	if !strings.Contains(got, "Cleared:") || !strings.Contains(got, "the 5 kept photos are gone") {
		t.Fatalf("a bare Clear should resolve the pending question, got %q", got)
	}
	if s.Onboarding != nil || len(s.BurstHeld) != 0 {
		t.Fatalf("both targets should be gone, got walk=%+v held=%d", s.Onboarding, len(s.BurstHeld))
	}
}

func TestDayFiguresRoundedAtTheFactsBoundary(t *testing.T) {
	dir := setupChat(t, clearWorkOwner, shopWithBag)
	merchant := catalogue.GetMerchant(MerchantID)

	// The live day: a GH₵26 giveaway the book's subtraction
	// left as a raw float's tail.
	book := `{"demo":{"date":"` + daybook.TodayString() + `","sales":1,"revenue":100,"givenAway":25.999999999999986,"walkAways":0,"allowance":300}}`
	if err := os.WriteFile(filepath.Join(dir, "data", "daybook.json"), []byte(book), 0o644); err != nil {
		t.Fatal(err)
	}

	// The prepared report itself carries GH₵26 and no tail.
	direct := insights.DayReportText(daybook.State{Sales: 1, Revenue: 100, GivenAway: 25.999999999999986, Allowance: 300})
	if !strings.Contains(direct, "GH₵26") || strings.Contains(direct, "25.999") {
		t.Fatalf("the day report should carry the rounded figure, got %q", direct)
	}

	// The attention answer — the live message that printed
	// the raw float — is composed from the same facts.
	got := merchantAttentionText(merchant, nil)
	if !strings.Contains(got, "Given away in haggling: GH₵26 of your GH₵300 allowance, GH₵274 left.") {
		t.Fatalf("the attention answer should carry GH₵26, got %q", got)
	}
	if strings.Contains(got, "25.999") {
		t.Fatalf("no long decimal may survive into the attention answer, got %q", got)
	}

	// The facts handed to a drafting call, and its pools,
	// are rounded whole cedis.
	text, moneyPool, _ := merchantDayFigures(merchant, MerchantID)
	if !strings.Contains(text, "GH₵26") {
		t.Fatalf("the drafted facts should carry GH₵26, got %q", text)
	}
	saw26 := false
	for _, v := range moneyPool {
		if v != money.Round(v) {
			t.Fatalf("a fact pool figure is not whole money: %v in %v", v, moneyPool)
		}
		if v == 26 {
			saw26 = true
		}
	}
	if !saw26 {
		t.Fatalf("the money pool should carry the rounded 26, got %v", moneyPool)
	}

	// And at the send side: even a draft that copies the
	// book's raw figure fails verification now, while the
	// rounded figure passes.
	raw := "Given away in haggling: GH₵25.999999999999986 of your GH₵300 allowance, GH₵274 left."
	pools := []float64{100, 26, 300, 274}
	counts := []float64{1, 0}
	if recapFiguresClean(raw, pools, counts) {
		t.Fatalf("a long-decimal figure must not survive the recap gate")
	}
	if figuresClean(raw, pools...) {
		t.Fatalf("a long-decimal figure must not survive the figures gate")
	}
	rounded := "Given away in haggling: GH₵26 of your GH₵300 allowance, GH₵274 left."
	if !recapFiguresClean(rounded, pools, counts) || !figuresClean(rounded, pools...) {
		t.Fatalf("the rounded figure should verify, as it did before")
	}
}
