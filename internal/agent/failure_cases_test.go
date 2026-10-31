package agent

// Monday's failure cases (pulled forward): the roads where
// something outside the agent fails — the model is down,
// the model answers garbage, the payment mint fails — and
// the conversation must still be carried honestly by the
// deterministic nets. Webhook abuse (bad signature, held
// settlements, idempotent settle) is pinned in the
// seevwebhook package; corrupt books and restart restore
// are pinned by the store and restore tests.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// runBuyerJourney drives the full deterministic purchase:
// entry, price question, the offer ladder, pickup,
// package name, no note, confirm. It returns the final
// turn's replies.
func runBuyerJourney(t *testing.T, c *chat, buyer string) []string {
	t.Helper()
	legs := []string{
		"SHOP-TEST",
		"How much is the beaded bag?",
		"I can offer 500",
		"600",
		"700",
		"730",
		"I will pick it up myself",
		"Akosua Mensah",
		"no",
	}
	for _, leg := range legs {
		c.say(t, buyer, leg)
		if got := c.replies(t, buyer); len(got) == 0 {
			t.Fatalf("no answer to %q: a failed dependency must never mean silence", leg)
		}
	}
	c.say(t, buyer, "yes")
	return c.replies(t, buyer)
}

func TestFailureModelDownJourneyStillCompletes(t *testing.T) {
	dir := setupChat(t, stockOwner, shopWithBag)
	// The model is configured but unreachable: every
	// thinking call fails at the connection, and the nets
	// must carry the whole purchase.
	t.Setenv("META_MODEL_API_KEY", "stub-key")
	t.Setenv("META_MODEL_API_BASE", "http://127.0.0.1:1/v1")
	c := &chat{dir: dir}
	buyer := "233550008001"

	final := runBuyerJourney(t, c, buyer)
	joined := strings.Join(final, "\n")
	if !strings.Contains(joined, "pay.example.test") {
		t.Fatalf("the journey must still reach the payment link with the model down, got %q", joined)
	}
	o := orders.Default.NewestFor("demo", buyer)
	if o == nil || o.Status != "pending" {
		t.Fatalf("the order stands pending after a model-down journey, got %+v", o)
	}
}

func TestFailureModelGarbageFallsBackHonestly(t *testing.T) {
	dir := setupChat(t, stockOwner, shopWithBag)
	garbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{this is not the model's json"))
	}))
	defer garbage.Close()
	t.Setenv("META_MODEL_API_KEY", "stub-key")
	t.Setenv("META_MODEL_API_BASE", garbage.URL+"/v1")
	c := &chat{dir: dir}
	buyer := "233550008002"

	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	// A judged ask the nets cannot read: the fallback puts
	// the shelf in front of her rather than going blank.
	c.say(t, buyer, "I need something nice for my mother's birthday")
	got := c.joined(t, buyer)
	if !strings.Contains(got, "Handmade Beaded Bag") {
		t.Fatalf("a garbage model answer falls back to the shelf, got %q", got)
	}
	// And the plain availability net still answers exactly.
	c.say(t, buyer, "Is the handmade beaded bag available?")
	got = c.joined(t, buyer)
	if !strings.Contains(got, "Still available") {
		t.Fatalf("the availability net carries its turn, got %q", got)
	}
}

func TestFailureModelErrorsDiscoveryStillAnswers(t *testing.T) {
	dir := setupChat(t, stockOwner, shopWithBag)
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()
	t.Setenv("META_MODEL_API_KEY", "stub-key")
	t.Setenv("META_MODEL_API_BASE", broken.URL+"/v1")
	c := &chat{dir: dir}
	buyer := "233550008003"
	// An unbound buyer asking what there is: when the
	// thinking cannot run, the discovery net still names
	// the shops.
	c.say(t, buyer, "Hello, what shops do you have?")
	got := c.joined(t, buyer)
	if !strings.Contains(got, "```\nTEST\nHandmade Beaded Bag - GH₵850\n```") {
		t.Fatalf("discovery must answer with the model erroring, got %q", got)
	}
}

func TestFailureSeevMintFailureIsHonestOnBothSides(t *testing.T) {
	dir := setupChat(t, stockOwner, shopWithBag)
	seev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer seev.Close()
	t.Setenv("SEEV_SECRET_KEY", "test-bogus-key")
	t.Setenv("SEEV_API_BASE", seev.URL)
	c := &chat{dir: dir}
	buyer := "233550008004"

	final := runBuyerJourney(t, c, buyer)
	joined := strings.Join(final, "\n")
	if strings.Contains(joined, "pay.example.test") {
		t.Fatalf("no link may be sent when the mint failed, got %q", joined)
	}
	if !strings.Contains(joined, "payment link is not coming up") {
		t.Fatalf("the buyer is told the truth about the link, got %q", joined)
	}
	o := orders.Default.NewestFor("demo", buyer)
	if o == nil || o.Status != "link_failed" {
		t.Fatalf("the agreed deal stands as link_failed, got %+v", o)
	}
	merchantGot := strings.Join(c.replies(t, stockOwner), "\n")
	if !strings.Contains(merchantGot, "Payment link failed") {
		t.Fatalf("the merchant is told at once, got %q", merchantGot)
	}
	// No sale is booked in the day book for a deal the
	// customer cannot pay for.
	merchant := catalogue.GetMerchant("demo")
	if got := merchantAttentionText(merchant, nil); !strings.Contains(got, "0 sale(s) today") {
		t.Fatalf("the day book books nothing for a failed mint: %q", got)
	}
}
