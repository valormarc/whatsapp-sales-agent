package model

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/persona"
)

// A local stand-in for the Meta Model API, shaped exactly like the
// real chat completions endpoint (verified against Meta's official
// cookbook). It records what it was sent so the tests can check the
// wire format: base URL + /chat/completions, Bearer key, OpenAI-style
// messages, and the reply read from choices[0].message.content.

type recorded struct {
	url  string
	auth string
	body struct {
		Model    string    `json:"model"`
		Messages []Message `json:"messages"`
	}
}

var (
	handler func(w http.ResponseWriter, r *http.Request)
	last    recorded
)

func stubServer(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string    `json:"model"`
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		last = recorded{url: r.URL.Path, auth: r.Header.Get("authorization")}
		last.body.Model = body.Model
		last.body.Messages = body.Messages
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/v1"
}

func replyWith(content string, status int) {
	handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(status)
		if status == 200 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
			})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "boom"})
		}
	}
}

func useModel(t *testing.T, base string) {
	t.Helper()
	t.Setenv("META_MODEL_API_KEY", "test-key")
	t.Setenv("META_MODEL_API_BASE", base)
}

func strOrNil(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func TestWithoutKeyModelOffAndChatNil(t *testing.T) {
	t.Setenv("META_MODEL_API_KEY", "")
	if Enabled() {
		t.Fatal("model must be off without a key")
	}
	if got := Chat([]Message{{Role: "user", Content: "hi"}}, Options{}); got != nil {
		t.Fatalf("chat = %q, want nil", strOrNil(got))
	}
}

func TestChatPostsOpenAIShapedRequestAndReturnsReply(t *testing.T) {
	base := stubServer(t)
	useModel(t, base)
	t.Setenv("META_MODEL", "")
	replyWith("Hello from the stub", 200)
	out := Chat([]Message{{Role: "user", Content: "Hello, world!"}}, Options{})
	if out == nil || *out != "Hello from the stub" {
		t.Fatalf("chat = %q, want the stub reply", strOrNil(out))
	}
	if last.url != "/v1/chat/completions" {
		t.Fatalf("url %q, want /v1/chat/completions", last.url)
	}
	if last.auth != "Bearer test-key" {
		t.Fatalf("auth %q, want Bearer test-key", last.auth)
	}
	if last.body.Model != "muse-spark-1.3" {
		t.Fatalf("model %q, want muse-spark-1.3", last.body.Model)
	}
	if len(last.body.Messages) != 1 || last.body.Messages[0] != (Message{Role: "user", Content: "Hello, world!"}) {
		t.Fatalf("messages %+v", last.body.Messages)
	}
}

func TestPersonaContextBecomesSystemPrompt(t *testing.T) {
	base := stubServer(t)
	useModel(t, base)
	replyWith("ok", 200)
	Chat([]Message{{Role: "user", Content: "offer"}}, Options{Context: &persona.Context{
		Merchant: catalogue.Merchant{Name: "Efua's Store"},
	}})
	if len(last.body.Messages) != 2 {
		t.Fatalf("messages %+v, want system plus user", last.body.Messages)
	}
	if last.body.Messages[0].Role != "system" {
		t.Fatal("first message must be the system prompt")
	}
	if !regexp.MustCompile(`Efua's Store`).MatchString(last.body.Messages[0].Content) {
		t.Fatal("system prompt must name the shop")
	}
	if last.body.Messages[1].Content != "offer" {
		t.Fatal("second message must be the user message")
	}
}

func TestHTTPErrorReturnsNilNeverThrows(t *testing.T) {
	base := stubServer(t)
	useModel(t, base)
	replyWith("nope", 500)
	if got := Chat([]Message{{Role: "user", Content: "hi"}}, Options{}); got != nil {
		t.Fatalf("chat = %q, want nil on HTTP error", strOrNil(got))
	}
}

func TestParseProposalReadsCleanFencedAndProseJSON(t *testing.T) {
	clean := `{"action":"counter","price":790,"message":"From GH₵850, I have come down to GH₵790 for you."}`
	if p := ParseProposal(&clean); p == nil || p.Price == nil || *p.Price != 790 {
		t.Fatalf("clean JSON: %+v", p)
	}
	fenced := "```json\n" + clean + "\n```"
	if p := ParseProposal(&fenced); p == nil || p.Action != "counter" {
		t.Fatalf("fenced JSON: %+v", p)
	}
	prose := "Here is my step: " + clean + " — done."
	if p := ParseProposal(&prose); p == nil || p.Price == nil || *p.Price != 790 {
		t.Fatalf("prose-wrapped JSON: %+v", p)
	}
}

func TestParseProposalNilOnUnparseable(t *testing.T) {
	for _, s := range []string{"no json here", "{not json}", ""} {
		s := s
		if p := ParseProposal(&s); p != nil {
			t.Fatalf("input %q: got %+v, want nil", s, p)
		}
	}
	if p := ParseProposal(nil); p != nil {
		t.Fatal("nil input must give nil")
	}
	// A non-string price leaves the price unset, which the guardrail
	// then refuses; the reference returns the object with its raw
	// price and the guardrail refuses on type instead. Same outcome.
	s := `{"action":"counter","price":"790"}`
	if p := ParseProposal(&s); p == nil || p.Price != nil {
		t.Fatalf("string price: %+v, want proposal with no numeric price", p)
	}
}
