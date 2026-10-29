package agent

// Blue ticks and the typing indicator: a message is marked
// read once, when its turn begins — a redelivery of the
// same message is dropped before it can be marked again,
// and a message with no WhatsApp id (the harness's own
// shape) is never marked at all.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readIDs(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "data", "outbox.jsonl"))
	if err != nil {
		return nil
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec struct {
			Kind   string `json:"kind"`
			ReadID string `json:"readId"`
		}
		if json.Unmarshal([]byte(line), &rec) == nil && rec.Kind == "read" {
			ids = append(ids, rec.ReadID)
		}
	}
	return ids
}

func TestMessageMarkedReadOnceAtPickup(t *testing.T) {
	dir := setupChat(t, shopOrdersOwner, shopOrdersSeed)

	HandleInbound(Inbound{From: shopOrdersOwner, Text: "my catalogue", MessageID: "wamid.READ-T1"})
	if got := readIDs(t, dir); len(got) != 1 || got[0] != "wamid.READ-T1" {
		t.Fatalf("after the first message, read records = %v", got)
	}

	// A redelivery of the same message is dropped whole,
	// before the marking: still one record.
	HandleInbound(Inbound{From: shopOrdersOwner, Text: "my catalogue", MessageID: "wamid.READ-T1"})
	if got := readIDs(t, dir); len(got) != 1 {
		t.Fatalf("a redelivery must not be marked again, read records = %v", got)
	}

	// No WhatsApp id, nothing to mark.
	HandleInbound(Inbound{From: shopOrdersOwner, Text: "my catalogue"})
	if got := readIDs(t, dir); len(got) != 1 {
		t.Fatalf("an id-less message must not be marked, read records = %v", got)
	}

	// A new message is a new pickup.
	HandleInbound(Inbound{From: shopOrdersOwner, Text: "my catalogue", MessageID: "wamid.READ-T2"})
	if got := readIDs(t, dir); len(got) != 2 || got[1] != "wamid.READ-T2" {
		t.Fatalf("the second message should be marked too, read records = %v", got)
	}
}
