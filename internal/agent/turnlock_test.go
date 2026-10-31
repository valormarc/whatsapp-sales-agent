package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Regression for the audit finding that one global lock
// serialised every conversation: a merchant's model-free
// command waited 2.7s behind a single buyer's slow model
// turn. Turns now lock per speaker, so the merchant's turn
// completes while the buyer's is still waiting on the model.
func TestTurnsForDifferentSpeakersRunInParallel(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "in red please") {
			time.Sleep(3 * time.Second)
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{
				"content": `{"intent":"ask","item":"Handmade Beaded Bag","reply":""}`,
			}}},
		})
	}))
	t.Cleanup(stub.Close)
	t.Setenv("META_MODEL_API_KEY", "test-key")
	t.Setenv("META_MODEL_API_BASE", stub.URL+"/v1")

	buyer := "233556666666"
	bindings.put(buyer, "demo")
	sessions.put(sessionKey("demo", buyer), &Session{Stage: "browsing", MerchantID: "demo"})

	done := make(chan struct{})
	go func() {
		defer close(done)
		HandleInbound(Inbound{From: buyer, Text: "do you have the beaded bag in red please", MessageID: "conc-buyer-1"})
	}()

	// The buyer's turn is inside its slow model call now.
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	HandleInbound(Inbound{From: stockOwner, Text: "my catalogue", MessageID: "conc-merchant-1"})
	merchantTook := time.Since(start)
	if merchantTook > 1500*time.Millisecond {
		t.Fatalf("the merchant's model-free turn waited %v behind the buyer's slow turn; turns must lock per speaker", merchantTook)
	}
	<-done
}

// The same speaker's turns still serialise: two messages
// from one person can never race her own conversation.
func TestTurnsForOneSpeakerSerialise(t *testing.T) {
	lock := turnLockFor("233550000001")
	if turnLockFor("233550000001") != lock {
		t.Fatal("one speaker must always map to the same turn lock")
	}
	if turnLockFor("233550000001") == turnLockFor("233550000002") {
		// Different speakers may share a stripe by hash
		// chance (128 stripes); that costs a little
		// parallelism, never correctness. This pair simply
		// documents the stripe count is not 1.
		t.Log("stripe collision for this pair; acceptable but noted")
	}
}
