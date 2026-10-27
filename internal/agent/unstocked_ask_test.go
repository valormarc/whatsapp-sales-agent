package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/wants"
)

// The availability question: "do you have X?" is the standard
// buyer ask in this market, and it carries no want word, so it
// used to fall past the want path into the generic fallback: no
// shelf-side answer in want form, no want written down, no alert
// when the goods landed. These tests pin the fix to the frames
// real buyers use, with stocked and vague controls, in the same
// two shops as the Twitter lessons tests.

func TestDoYouHaveAnUnstockedItemRecordsTheWant(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550007001"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	// The exact sentence from the live proof, in a two-product
	// shop that stocks neither rice nor cookers.
	c.say(t, buyer, "Do you have a rice cooker?")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "I do not have that one here yet") || !strings.Contains(got, "I will alert you") {
		t.Fatalf("availability ask = %q, want the want reply, not the generic fallback", got)
	}
	if !strings.Contains(got, "Smoked Tilapia") {
		t.Fatalf("availability ask = %q, want her own shop's shelf offered", got)
	}

	// The want is in Ama's book under the buyer's own words, and
	// in no other shop's.
	list := wants.Default.List("ama")
	if len(list) != 1 || list[0].Buyer != buyer || !strings.Contains(strings.ToLower(list[0].Text), "rice cooker") {
		t.Fatalf("ama's wants = %+v, want the rice cooker ask recorded once", list)
	}
	if list := wants.Default.List("efua"); len(list) != 0 {
		t.Fatalf("the want leaked into Efua's book: %+v", list)
	}

	// And the alert fires when the goods land, the close the
	// sourcing buyer in my chats never got.
	onboardForTest(t, c, ownerAma, "Rice Cooker", "Electric rice cooker, 2 litres.", "300", "250")
	got = strings.Join(c.replies(t, buyer), "\n")
	if strings.Count(got, "Good news") != 1 || !strings.Contains(got, "Rice Cooker is now at Ama's Stall, GH₵300. Reply to buy.") {
		t.Fatalf("want alert = %q, want exactly one alert naming the product, shop and price", got)
	}
}

func TestAvailabilityFramesRecordWants(t *testing.T) {
	_, c := setupTwitterShops(t)

	asks := []struct {
		buyer string
		ask   string
		names string
	}{
		{"233550007002", "Have you got a blender?", "blender"},
		{"233550007003", "Do you sell phone chargers?", "chargers"},
		{"233550007004", "Is a kettle available?", "kettle"},
		{"233550007005", "Are you selling umbrellas?", "umbrellas"},
		{"233550007006", "I am looking for a standing fan", "standing fan"},
		{"233550007007", "I want a mosquito net", "mosquito net"},
	}
	for _, a := range asks {
		c.say(t, a.buyer, "SHOP-EFUA")
		c.replies(t, a.buyer)
		c.say(t, a.buyer, a.ask)
		got := strings.Join(c.replies(t, a.buyer), "\n")
		if !strings.Contains(got, "I will alert you") {
			t.Fatalf("ask %q = %q, want the want reply", a.ask, got)
		}
	}
	list := wants.Default.List("efua")
	if len(list) != len(asks) {
		t.Fatalf("efua's wants = %+v, want one per ask", list)
	}
	for i, a := range asks {
		if list[i].Buyer != a.buyer || !strings.Contains(strings.ToLower(list[i].Text), a.names) {
			t.Fatalf("want %d = %+v, want buyer %s asking for %s", i, list[i], a.buyer, a.names)
		}
	}
	if list := wants.Default.List("ama"); len(list) != 0 {
		t.Fatalf("wants leaked into Ama's book: %+v", list)
	}
}

func TestBareItemNameWithAQuestionMarkRecordsTheWant(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550007008"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "Rice cooker?")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "I will alert you") {
		t.Fatalf("bare item ask = %q, want the want reply", got)
	}
	if list := wants.Default.List("ama"); len(list) != 1 || list[0].Buyer != buyer {
		t.Fatalf("ama's wants = %+v, want the bare ask recorded", list)
	}
}

func TestDoYouHaveAStockedItemAnswersTheItemAndRecordsNoWant(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550007009"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	for _, ask := range []string{"Do you have tilapia?", "Do you sell tilapia?"} {
		c.say(t, buyer, ask)
		got := strings.Join(c.replies(t, buyer), "\n")
		if !strings.Contains(got, "Smoked Tilapia") || !strings.Contains(got, "GH₵180") {
			t.Fatalf("stocked ask %q = %q, want the tilapia answered with its price", ask, got)
		}
		if strings.Contains(got, "I will alert you") {
			t.Fatalf("stocked ask %q took the want path: %q", ask, got)
		}
	}
	if list := wants.Default.List("ama"); len(list) != 0 {
		t.Fatalf("a stocked ask recorded a want: %+v", list)
	}
}

func TestVagueAvailabilityQuestionsRecordNoWant(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550007010"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	for _, ask := range []string{"Do you have anything nice?", "Do you have any discounts?", "Do you have it?"} {
		c.say(t, buyer, ask)
		got := strings.Join(c.replies(t, buyer), "\n")
		if strings.Contains(got, "I will alert you") {
			t.Fatalf("vague ask %q took the want path: %q", ask, got)
		}
	}
	if list := wants.Default.List("ama"); len(list) != 0 {
		t.Fatalf("a vague ask recorded a want: %+v", list)
	}
}

func TestAvailabilityAskMidHaggleHoldsTheHaggleAndRecordsNoWant(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550007011"

	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.last(t, buyer)
	c.say(t, buyer, "I can offer 100")
	c.last(t, buyer)
	c.say(t, buyer, "Do you have a rice cooker?")
	got := c.last(t, buyer)
	if !strings.Contains(got, "Smoked Tilapia") || !strings.Contains(got, "GH₵") {
		t.Fatalf("availability ask mid-haggle = %q, want the holding reply on the tilapia", got)
	}
	if strings.Contains(got, "I will alert you") {
		t.Fatalf("availability ask mid-haggle took the want path: %q", got)
	}
	if list := wants.Default.List("ama"); len(list) != 0 {
		t.Fatalf("a mid-haggle ask recorded a want: %+v", list)
	}
	// The haggle is exactly where she left it: a meeting offer
	// on the tilapia still closes.
	c.say(t, buyer, "170")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "GH₵") {
		t.Fatalf("haggle after the availability ask = %q", got)
	}
}
