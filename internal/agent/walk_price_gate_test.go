package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

// A listing walk waiting on a price must not eat the
// merchant's other business: his live loop had "Order
// delivered", a pending-delivery question, "What need my
// attention", a stock question and a stats question all
// answered with "What is the list price for Lace
// Nightdress?". At a price step, a message that is not
// shaped as the price is read by her ordinary readers
// first, and the walk stays open behind them for the
// figure that resumes it.
func stubWalkGateModel(t *testing.T) {
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
		for _, marker := range []string{"The owner's message:", "Her message:"} {
			if i := strings.LastIndex(text, marker); i >= 0 {
				her = text[i:]
			}
		}
		lower := strings.ToLower(her)
		content := `{"intent":"unclear","reply":"Say that again for me."}`
		switch {
		case strings.Contains(lower, "order delivered"):
			content = `{"intent":"order_sent"}`
		case strings.Contains(lower, "attention"):
			content = `{"intent":"attention"}`
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

func TestWalkPriceStepDoesNotEatMerchantBusiness(t *testing.T) {
	merchant := "233550009101"
	dir := setupChat(t, merchant, shopWithBag)
	stubWalkGateModel(t)
	c := &chat{dir: dir}
	buyer := "233550009102"
	orders.Default.RecordOrder("TEST-WALK-1", orders.Order{
		From: buyer, Status: "paid", ProductName: "Handmade Beaded Bag",
		Price: 850, Total: 875, DeliveryFee: 25, MerchantID: MerchantID,
		Fulfilment: "delivery", Location: "Osu", CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	sessions.put(sessionKey(MerchantID, merchant), &Session{
		Stage: "merchant", MerchantID: MerchantID,
		Onboarding: &OnboardingFlow{Step: "list", Name: "Lace Nightdress", Description: "A lace nightdress."},
	})

	c.say(t, merchant, "Order delivered")
	got := strings.Join(c.replies(t, merchant), "\n")
	if strings.Contains(got, "list price for Lace Nightdress") {
		t.Fatalf("the delivery report must not be answered with the price question: %q", got)
	}
	if o := orders.Default.GetOrder("TEST-WALK-1"); o == nil || o.DeliveryState != "sent" {
		t.Fatalf("the order must be marked sent: %+v", o)
	}
	s := sessions.get(sessionKey(MerchantID, merchant))
	if s == nil || s.Onboarding == nil || s.Onboarding.Step != "list" {
		t.Fatalf("the walk stays open behind the errand: %+v", s)
	}

	c.say(t, merchant, "What need my attention")
	got = strings.Join(c.replies(t, merchant), "\n")
	if strings.Contains(got, "list price for Lace Nightdress") {
		t.Fatalf("the attention question must not be answered with the price question: %q", got)
	}

	c.say(t, merchant, "120")
	got = strings.Join(c.replies(t, merchant), "\n")
	s = sessions.get(sessionKey(MerchantID, merchant))
	if s == nil || s.Onboarding == nil || s.Onboarding.Step != "lastprice" || s.Onboarding.ListPrice != 120 {
		t.Fatalf("a shaped figure resumes the walk where it left off: %+v", s)
	}
	if !strings.Contains(got, "last price") {
		t.Fatalf("the walk asks its next question: %q", got)
	}
}
