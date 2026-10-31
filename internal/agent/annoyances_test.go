package agent

// The annoyances, pinned: a laugh and an "ok" answered
// with the day book, the allowance reminder riding every
// answer, and an allowance she set on purpose — zero
// included — treated as never chosen. Each test is one of
// those moments, driven the way it happened.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/persona"
)

func TestMerchantAckIsNotAnsweredWithTheDayBook(t *testing.T) {
	owner := "233550008101"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	for _, msg := range []string{"Ok", "😂", "thanks", "Got it."} {
		c.say(t, owner, msg)
		got := strings.Join(c.replies(t, owner), "\n")
		if strings.Contains(got, "sale(s) today") || strings.Contains(got, "walk-away") {
			t.Fatalf("a bare %q was answered with the day book: %q", msg, got)
		}
		if got == "" {
			t.Fatalf("a bare %q was met with silence", msg)
		}
	}
}

func TestAllowanceNagFiresOnceADayAndASetZeroSettlesIt(t *testing.T) {
	owner := "233550008201"
	dir := setupChat(t, owner, shopNoAllowance)
	c := &chat{dir: dir}

	c.say(t, owner, "How is today going?")
	first := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(first, "you have not set a daily giveaway allowance") {
		t.Fatalf("the first day answer should carry the reminder, got %q", first)
	}
	c.say(t, owner, "How is today going?")
	second := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(second, "sale(s) today") {
		t.Fatalf("the second day answer should still answer, got %q", second)
	}
	if strings.Contains(second, "you have not set a daily giveaway allowance") {
		t.Fatalf("the reminder must not ride twice in one day, got %q", second)
	}

	// She sets it to zero on purpose: a choice, settled.
	c.say(t, owner, "set allowance 0")
	c.replies(t, owner)
	shop := catalogue.GetMerchant(MerchantID)
	if shop == nil || !shop.AllowanceSet || shop.DailyGiveawayAllowance != 0 {
		t.Fatalf("a set zero should be recorded as chosen, got %+v", shop)
	}
	c.say(t, owner, "How is today going?")
	third := strings.Join(c.replies(t, owner), "\n")
	if strings.Contains(third, "you have not set a daily giveaway allowance") {
		t.Fatalf("a chosen zero must never be reminded about, got %q", third)
	}
}

func TestAnswerToTheAllowanceNoteSetsIt(t *testing.T) {
	owner := "233550008301"
	dir := setupChat(t, owner, shopNoAllowance)
	c := &chat{dir: dir}

	// Earn the note, then answer it the way she did: quote
	// it, and say "set it 300" in her own words.
	c.say(t, owner, "How is today going?")
	c.replies(t, owner)
	s := sessions.get(sessionKey(MerchantID, owner))
	if s == nil || s.LastNoteMsgID == "" {
		t.Fatal("the day answer should have left the allowance note on the session")
	}
	HandleInbound(Inbound{From: owner, ContextID: s.LastNoteMsgID, Text: "set it 300"})
	got := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "allowance is now GH₵300") {
		t.Fatalf("the note answer should confirm the set, got %q", got)
	}
	shop := catalogue.GetMerchant(MerchantID)
	if shop == nil || shop.DailyGiveawayAllowance != 300 || !shop.AllowanceSet {
		t.Fatalf("the note answer should set the allowance in the book, got %+v", shop)
	}
}

func TestTheAgentKnowsItsName(t *testing.T) {
	merchant := catalogue.GetMerchant(MerchantID)
	if merchant == nil {
		// A bare world: build the prompt from a plain record.
		m := catalogue.Merchant{ID: "x", Name: "Test Shop"}
		merchant = &m
	}
	if got := persona.BuildMerchantPrompt(*merchant, daybook.State{}); !strings.Contains(got, "Your name is Stall") {
		t.Fatalf("the merchant prompt should carry the agent's name, got %q", got[:120])
	}
}
