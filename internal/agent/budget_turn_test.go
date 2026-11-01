package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A buyer who states her need and her means in one breath
// ("I need a gift for my mother. I have 200 cedis.") was
// having the figure claimed by the offer road, which asked
// which item the offer was for and showed the shelf: she
// stated a budget, not an offer, and got shown products
// instead of helped. A figure attached to nothing, in a
// message that says more than the number, is now read by
// the thinking first, which carries the budget and
// recommends within it. A bare figure keeps the offer
// road's question: there is nothing else to read.
const budgetTurnCatalogue = `{"merchants":{"efua":{"id":"efua","name":"Efua's Store","shopCode":"EFUA","ownerWhatsApp":"233550009101","payoutPhone":"233550009101","dailyGiveawayAllowance":300,"deliveryFee":25,"deliveryPaidTo":"merchant","products":[{"id":"bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":3},{"id":"dress","name":"Ankara Print Dress","description":"Bright ankara print dress, knee length.","listPrice":220,"floorPrice":180,"stock":2},{"id":"sandals","name":"Leather Sandals","description":"Brown leather sandals, handmade.","listPrice":150,"floorPrice":120,"stock":4},{"id":"scarf","name":"Silk Scarf","description":"Soft silk scarf in gold and green.","listPrice":90,"floorPrice":70,"stock":5}]}}}`

func stubBudgetTurnModel(t *testing.T) {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		text := ""
		for _, m := range body.Messages {
			text += m.Content + "\n"
		}
		her := text
		if i := strings.LastIndex(text, "Her message:"); i >= 0 {
			her = text[i:]
		}
		content := "For a gift within what you have, the Leather Sandals at GH₵150 are handmade and keep well."
		if strings.Contains(text, "thinking of a WhatsApp sales agent") {
			content = `{"intent":"unclear","reply":"Tell me more."}`
			if strings.Contains(strings.ToLower(her), "gift for my mother") {
				content = `{"intent":"recommend","budget":200}`
			}
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		})
	}))
	t.Cleanup(stub.Close)
	t.Setenv("META_MODEL_API_KEY", "stub-key")
	t.Setenv("META_MODEL_API_BASE", stub.URL+"/v1")
}

func TestAStatedBudgetIsReadAsABudgetNotAnOffer(t *testing.T) {
	dir := setupChat(t, "233550009101", budgetTurnCatalogue)
	stubBudgetTurnModel(t)
	c := &chat{dir: dir}
	buyer := "233550009102"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "I need a gift for my mother. I have 200 cedis.")
	got := strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "Which item is that offer for") {
		t.Fatalf("a stated budget must not be claimed as an offer for an unnamed item: %q", got)
	}
	if !strings.Contains(got, "Leather Sandals") && !strings.Contains(got, "Silk Scarf") {
		t.Fatalf("an in-budget item must be put forward: %q", got)
	}
	s := sessions.get(sessionKey("efua", buyer))
	if s == nil || s.Budget != 200 {
		t.Fatalf("the stated budget is carried for the conversation: %+v", s)
	}
}

func TestABareFigureStillGetsTheOfferQuestion(t *testing.T) {
	dir := setupChat(t, "233550009101", budgetTurnCatalogue)
	stubBudgetTurnModel(t)
	c := &chat{dir: dir}
	buyer := "233550009103"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "200")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Which item is that offer for") {
		t.Fatalf("a bare figure with nothing to read stays the offer road's question: %q", got)
	}
}
