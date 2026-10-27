package agent

import (
	"strings"
	"testing"
)

// Group lessons: behaviour taken from observing two real Ghanaian
// selling groups, read-only. Buyers there open with bare
// wants ("32 or 43 inches tv needed"), answer vaguely ("Any type"),
// and demand evidence in two words ("Send pic"). Sellers never go
// blank at any of it: they ask at most one light question, answer
// with what they have, and hold the price up. These tests pin the
// agent to the same conduct.

func TestBareWantNamingAnItemOpensTheHaggle(t *testing.T) {
	_, c := setupTwoShops(t)

	buyer := "233550004301"
	c.say(t, buyer, "SHOP-AMA")
	c.say(t, buyer, "tilapia needed")
	if got := c.last(t, buyer); !strings.Contains(got, "The price is GH₵180") {
		t.Fatalf("bare want quote = %q, want the tilapia quoted and the haggle open", got)
	}
	c.say(t, buyer, "150")
	if got := c.last(t, buyer); strings.Contains(got, "I can help with prices and orders") || !strings.Contains(got, "GH₵") {
		t.Fatalf("offer after a bare want = %q, want the haggle to answer", got)
	}
}

func TestBareWantForSomethingNotSoldHere(t *testing.T) {
	_, c := setupTwoShops(t)

	buyer := "233550004302"
	c.say(t, buyer, "SHOP-AMA")
	c.say(t, buyer, "I need a tv")
	got := c.last(t, buyer)
	if !strings.Contains(got, "Tell me which of these you are after") || !strings.Contains(got, "Smoked Tilapia") {
		t.Fatalf("want for an unstocked item = %q, want the shelf offered back", got)
	}
	if strings.Contains(got, "is yours for") {
		t.Fatalf("a want alone closed a sale: %q", got)
	}
}

func TestVagueReplyMidHaggleHoldsThePrice(t *testing.T) {
	_, c := setupTwoShops(t)

	buyer := "233550004303"
	c.say(t, buyer, "SHOP-AMA")
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.say(t, buyer, "I can offer 100")
	if got := c.last(t, buyer); !strings.Contains(got, "GH₵") {
		t.Fatalf("first counter = %q", got)
	}

	// The group's own vague answer, word for word in spirit.
	c.say(t, buyer, "any type")
	got := c.last(t, buyer)
	if strings.Contains(got, "I can help with prices and orders") {
		t.Fatalf("vague mid-haggle reply hit the generic fallback: %q", got)
	}
	if !strings.Contains(got, "stands at GH₵") {
		t.Fatalf("vague mid-haggle reply = %q, want the standing price held up", got)
	}

	// And the haggle is still alive after it. (Replies now arrive in
	// pieces, so read the whole reply, not just its last message.)
	c.say(t, buyer, "160")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "GH₵") {
		t.Fatalf("offer after the vague turn = %q, want the haggle to continue", got)
	}
}

func TestSendPicAfterViewingAnItem(t *testing.T) {
	_, c := setupTwoShops(t)

	buyer := "233550004304"
	c.say(t, buyer, "SHOP-AMA")
	c.say(t, buyer, "Tilapia smoked")
	c.say(t, buyer, "send pic")
	got := c.last(t, buyer)
	if !strings.Contains(got, "cannot send a photo") || !strings.Contains(got, "Smoked Tilapia") || !strings.Contains(got, "GH₵180") {
		t.Fatalf("send pic reply = %q, want the plain no-photo answer with the item and price", got)
	}
}

func TestSendPicMidHaggle(t *testing.T) {
	_, c := setupTwoShops(t)

	buyer := "233550004305"
	c.say(t, buyer, "SHOP-AMA")
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.say(t, buyer, "send pic")
	got := c.last(t, buyer)
	if !strings.Contains(got, "cannot send a photo") || !strings.Contains(got, "stands at GH₵180") {
		t.Fatalf("mid-haggle send pic = %q, want the no-photo answer holding the standing price", got)
	}
}
