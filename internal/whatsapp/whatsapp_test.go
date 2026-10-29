package whatsapp

// The buttons payload is pinned to the Graph API's schema: the
// interactive type must be "button", and each button inside the
// action is of type "reply" carrying its id and title. This test
// exists because the shape once carried "reply_button", which
// Meta refuses as a schema violation; in dry-run nothing ever
// posted, so only a live send could have caught it, and it did,
// the hard way.

import (
	"encoding/json"
	"testing"
)

func TestButtonsPayloadMatchesTheGraphSchema(t *testing.T) {
	payload := buildButtonsPayload("233200000000", "Pick one.", []Button{
		{ID: "door_buy", Title: "I came to buy"},
		{ID: "door_sell", Title: "I want to sell"},
	})
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Type        string `json:"type"`
		Interactive struct {
			Type string `json:"type"`
			Body struct {
				Text string `json:"text"`
			} `json:"body"`
			Action struct {
				Buttons []struct {
					Type  string `json:"type"`
					Reply struct {
						ID    string `json:"id"`
						Title string `json:"title"`
					} `json:"reply"`
				} `json:"buttons"`
			} `json:"action"`
		} `json:"interactive"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "interactive" || got.Interactive.Type != "button" {
		t.Fatalf("types = %q / %q, want interactive / button", got.Type, got.Interactive.Type)
	}
	if got.Interactive.Body.Text != "Pick one." {
		t.Fatalf("body = %q", got.Interactive.Body.Text)
	}
	if len(got.Interactive.Action.Buttons) != 2 {
		t.Fatalf("buttons = %d, want 2", len(got.Interactive.Action.Buttons))
	}
	first := got.Interactive.Action.Buttons[0]
	if first.Type != "reply" || first.Reply.ID != "door_buy" || first.Reply.Title != "I came to buy" {
		t.Fatalf("first button = %+v", first)
	}
}

// The read payload is pinned the same way: marking a message
// read is what gives the sender her blue ticks, and the typing
// indicator only exists as a rider on this call — a wrong
// shape here would fail silently in dry-run and cost the chat
// its most human signal.
func TestReadPayloadMatchesTheGraphSchema(t *testing.T) {
	payload := buildReadPayload("wamid.TEST123")
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		MessagingProduct string `json:"messaging_product"`
		Status           string `json:"status"`
		MessageID        string `json:"message_id"`
		TypingIndicator  struct {
			Type string `json:"type"`
		} `json:"typing_indicator"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.MessagingProduct != "whatsapp" || got.Status != "read" || got.MessageID != "wamid.TEST123" {
		t.Fatalf("read payload = %s", raw)
	}
	if got.TypingIndicator.Type != "text" {
		t.Fatalf("typing indicator = %s", raw)
	}
}

func TestCTAPayloadMatchesTheGraphSchema(t *testing.T) {
	payload := buildCTAPayload("233200000000", "Pay here to complete your order.", "Click to pay GH₵875", "https://pay.example.test/ABC")
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Type        string `json:"type"`
		Interactive struct {
			Type string `json:"type"`
			Body struct {
				Text string `json:"text"`
			} `json:"body"`
			Action struct {
				Name       string `json:"name"`
				Parameters struct {
					DisplayText string `json:"display_text"`
					URL         string `json:"url"`
				} `json:"parameters"`
			} `json:"action"`
		} `json:"interactive"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "interactive" || got.Interactive.Type != "cta_url" || got.Interactive.Action.Name != "cta_url" {
		t.Fatalf("payload shape = %s", raw)
	}
	if got.Interactive.Body.Text != "Pay here to complete your order." ||
		got.Interactive.Action.Parameters.DisplayText != "Click to pay GH₵875" ||
		got.Interactive.Action.Parameters.URL != "https://pay.example.test/ABC" {
		t.Fatalf("payload contents = %s", raw)
	}
}
