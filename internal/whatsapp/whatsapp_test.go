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
	"time"
)

// The pace is per recipient: a first send never waits, a
// second send to the same person waits out the gap, another
// person is unaffected, and once the gap has passed there
// is nothing to wait for. The reservation is what is tested
// here; the sleep itself is one line on top of it.
func TestPaceSlotReservesTheGapPerRecipient(t *testing.T) {
	old := sendPaceGap
	sendPaceGap = 300 * time.Millisecond
	defer func() { sendPaceGap = old }()
	to := "233200000777"
	if wait := reservePaceSlot(to); wait != 0 {
		t.Fatalf("the first send should not wait, got %v", wait)
	}
	if wait := reservePaceSlot(to); wait <= 0 || wait > sendPaceGap {
		t.Fatalf("the second send should wait out the gap, got %v", wait)
	}
	if wait := reservePaceSlot("233200000778"); wait != 0 {
		t.Fatalf("another recipient should not wait, got %v", wait)
	}
	paceMu.Lock()
	paceLast[to] = time.Now().Add(-time.Second)
	paceMu.Unlock()
	if wait := reservePaceSlot(to); wait != 0 {
		t.Fatalf("once the gap has passed there is no wait, got %v", wait)
	}
	if wait := reservePaceSlot(""); wait != 0 {
		t.Fatalf("a record with no recipient is never paced, got %v", wait)
	}
}

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

// The list payload is pinned the same way: the slide-up
// sheet is how a buyer browses a shelf, and a schema slip
// here fails silently in dry-run — the sheet would simply
// never appear on the phone.
func TestListPayloadMatchesTheGraphSchema(t *testing.T) {
	payload := buildListPayload("233200000000", "This is the shelf.", "See the shelf", []ListSection{
		{Title: "Shelf 1 of 2", Rows: []ListRow{
			{ID: "shelf_bag", Title: "Beaded Bag", Description: "GH₵850"},
			{ID: "shelfmore_2", Title: "More items", Description: "See Shelf 2 of 2"},
		}},
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
				Button   string `json:"button"`
				Sections []struct {
					Title string `json:"title"`
					Rows  []struct {
						ID          string `json:"id"`
						Title       string `json:"title"`
						Description string `json:"description"`
					} `json:"rows"`
				} `json:"sections"`
			} `json:"action"`
		} `json:"interactive"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "interactive" || got.Interactive.Type != "list" {
		t.Fatalf("types = %q / %q, want interactive / list", got.Type, got.Interactive.Type)
	}
	if got.Interactive.Action.Button != "See the shelf" {
		t.Fatalf("button = %q", got.Interactive.Action.Button)
	}
	if len(got.Interactive.Action.Sections) != 1 || len(got.Interactive.Action.Sections[0].Rows) != 2 {
		t.Fatalf("sections = %+v", got.Interactive.Action.Sections)
	}
	row := got.Interactive.Action.Sections[0].Rows[0]
	if row.ID != "shelf_bag" || row.Title != "Beaded Bag" || row.Description != "GH₵850" {
		t.Fatalf("first row = %+v", row)
	}
}

// The flow payload is pinned the same way: a form panel is
// how details are collected in one go, and a schema slip
// here fails silently in dry-run — the panel would simply
// never open on the phone.
func TestFlowPayloadMatchesTheGraphSchema(t *testing.T) {
	payload := buildFlowPayload("233200000000", "Fill them once.", "Fill your details", "1234567890", "delivery-abc123", "DETAILS")
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
					Version string `json:"flow_message_version"`
					Token   string `json:"flow_token"`
					ID      string `json:"flow_id"`
					CTA     string `json:"flow_cta"`
					Act     string `json:"flow_action"`
					Payload struct {
						Screen string `json:"screen"`
					} `json:"flow_action_payload"`
				} `json:"parameters"`
			} `json:"action"`
		} `json:"interactive"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "interactive" || got.Interactive.Type != "flow" || got.Interactive.Action.Name != "flow" {
		t.Fatalf("types = %q / %q / %q, want interactive / flow / flow", got.Type, got.Interactive.Type, got.Interactive.Action.Name)
	}
	p := got.Interactive.Action.Parameters
	if p.Version != "3" || p.Token != "delivery-abc123" || p.ID != "1234567890" || p.CTA != "Fill your details" || p.Act != "navigate" || p.Payload.Screen != "DETAILS" {
		t.Fatalf("parameters = %+v", p)
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
