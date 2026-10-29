// Package model is the Meta Model API client: Muse Spark, reached the
// way Meta's own cookbook prescribes
// (github.com/meta-models/meta-model-cookbook). The API is
// OpenAI-compatible: POST {base}/chat/completions, Bearer key,
// { model, messages } in, reply text at choices[0].message.content.
// The default model is muse-spark-1.3.
//
// Fail-soft by contract: no key, an HTTP error, a timeout or a
// malformed response all return nil, and every caller falls back to
// the rule-based engine. The agent must never go silent because the
// model did. The persona system prompt comes from package persona; the
// model proposes, and package haggle verifies every price before it
// is sent.
package model

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/valormarc/hackathon-agent/internal/haggle"
	"github.com/valormarc/hackathon-agent/internal/persona"
)

const (
	defaultBase      = "https://api.meta.ai/v1"
	defaultModel     = "muse-spark-1.3"
	defaultTimeoutMs = 20000
)

// Message is one OpenAI-style chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Enabled reports whether a Meta Model API key is configured.
func Enabled() bool {
	return os.Getenv("META_MODEL_API_KEY") != ""
}

func endpoint() string {
	base := os.Getenv("META_MODEL_API_BASE")
	if base == "" {
		base = defaultBase
	}
	return strings.TrimRight(base, "/") + "/chat/completions"
}

func chosenModel() string {
	if m := os.Getenv("META_MODEL"); m != "" {
		return m
	}
	return defaultModel
}

// Options carries the optional inputs to Chat. Context, when set, has
// the sales persona built and prepended as the system prompt; System
// supplies a system prompt directly. TimeoutMs overrides the default
// 20 second timeout when set. Kind is the call's budget class (see
// the turn budget below); the zero value is KindUnderstand, the
// scarcer class, so a call site must opt into drafting explicitly.
type Options struct {
	Context   *persona.Context
	System    *string
	TimeoutMs *int
	Kind      int
}

// The per-turn budget, the platform's standing rule made
// mechanical: one model call may UNDERSTAND a turn (read an
// intent, judge a negotiation, search the directory), and
// drafting (a recap, a settlement line, a listing's
// composition) is a separate, separately verified job with
// its own small budget of two. The agent opens every turn
// with BeginTurn; a call past its class budget is refused
// before it is spent, and the caller falls back exactly as
// it does for a failed call, so an over-budget path degrades
// to the rule nets instead of silently doubling the turn's
// cost and latency. Until BeginTurn is called (unit tests,
// one-off tools), calls are uncounted.
const (
	KindUnderstand = iota
	KindDraft
)

const draftTurnCap = 2

var (
	turnArmed      atomic.Bool
	turnUnderstand atomic.Int64
	turnDraft      atomic.Int64
)

// BeginTurn opens a fresh per-turn budget.
func BeginTurn() {
	turnUnderstand.Store(0)
	turnDraft.Store(0)
	turnArmed.Store(true)
}

// spend charges one call of the kind against the turn's
// budget and reports whether it may go ahead.
func spend(kind int) bool {
	if !turnArmed.Load() {
		return true
	}
	if kind == KindDraft {
		if turnDraft.Add(1) <= draftTurnCap {
			return true
		}
		log.Printf("model: turn budget refused a drafting call (cap %d)", draftTurnCap)
		return false
	}
	if turnUnderstand.Add(1) <= 1 {
		return true
	}
	log.Printf("model: turn budget refused an understanding call (cap 1)")
	return false
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func extractReply(data []byte) *string {
	var parsed chatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}
	if len(parsed.Choices) == 0 {
		return nil
	}
	text := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if text == "" {
		return nil
	}
	return &text
}

// Chat makes one chat completion. Messages is an OpenAI-style array.
// It returns the reply text, or nil on any failure.
// modelHTTPClient bounds every model call: a slow model must
// never hold a conversation (or a settlement) hostage for
// longer than a person would wait for a reply.
var modelHTTPClient = &http.Client{Timeout: 25 * time.Second}

func Chat(messages []Message, opts Options) *string {
	if !Enabled() {
		return nil
	}
	if !spend(opts.Kind) {
		return nil
	}
	var system *string
	if opts.Context != nil {
		s := persona.BuildSystemPrompt(*opts.Context)
		system = &s
	} else if opts.System != nil {
		system = opts.System
	}
	wire := make([]Message, 0, len(messages)+1)
	if system != nil {
		wire = append(wire, Message{Role: "system", Content: *system})
	}
	wire = append(wire, messages...)

	timeoutMs := defaultTimeoutMs
	if opts.TimeoutMs != nil {
		timeoutMs = *opts.TimeoutMs
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	body, err := json.Marshal(chatRequest{Model: chosenModel(), Messages: wire})
	if err != nil {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+os.Getenv("META_MODEL_API_KEY"))

	res, err := modelHTTPClient.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil
	}
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil
	}
	return extractReply(data)
}

// ListingDraft is the model's draft of a shop listing from a
// seller's product photo: a name and a short description. It
// deliberately carries no price. A price is money, and money comes
// only from the seller's own words, read by code; the model's draft
// is a proposal the seller confirms or corrects before anything
// goes live.
type ListingDraft struct {
	Name        string
	Description string
	// Unit is the model's judgement of how the goods are
	// sold (piece, yard, bottle, kilo...), and UnitCertain
	// whether that judgement can be trusted without asking
	// the seller. The judgement is the model's, never a
	// table in code: only the seller's own words outrank it,
	// and a genuinely untellable case is the only one that
	// earns a question.
	Unit        string
	UnitCertain bool
	// Colour is the main colour of the goods in THIS photo,
	// as a buyer would name it ("white", "rose gold"), or
	// empty when no single colour dominates. It is the
	// model's reading of the picture, per photo: a listing
	// whose photos report several colours is one item in
	// several colours, and its name and description are
	// shaped accordingly at save (see applyColourRule).
	Colour string
	// Sizes are the sizes the goods come in, read from the
	// seller's words or the goods themselves (a shoebox
	// label, a size range in her caption), in her own terms.
	// Empty when the goods do not come in sizes.
	Sizes []string
}

// JudgeShape asks the model, in ONE call, how a named,
// described product is sold and which sizes it comes in,
// for listings that arrive as words alone with no photo to
// read. It is listing composition, a drafting-class call.
// It returns the unit word, whether the unit call is
// certain, and the judged sizes; an unavailable model or
// an unparsable reply returns certain=false and no sizes,
// and the seller is asked, as before. Only sizes the words
// themselves state count: a range like "sizes 37 to 42"
// becomes each size in it; words carrying no sizes return
// an empty list.
func JudgeShape(name, description string) (unit string, certain bool, sizes []string) {
	system := "You help a market seller list her goods. Read the product below and judge how it is sold, the way the seller herself would say it, and which sizes it comes in. " +
		"Reply with exactly one JSON object, no other text: {\"unit\": \"...\", \"certain\": true, \"sizes\": [\"...\"]}. " +
		"The unit is one word or two: piece for clothes, shoes and bags, yard for fabric, bottle for creams and oils, kilo or bag for foodstuffs, and so on. " +
		"certain is false only when how it is sold genuinely cannot be told from the name and description. " +
		"sizes lists only sizes the words themselves state, in the seller's own terms (shoe numbers, UK sizes, S M L, waist numbers): a stated range like 37 to 42 becomes each size in it. When the words state no sizes, or the goods do not come in sizes, give an empty list. Never guess sizes such goods usually come in."
	reply := Chat([]Message{{Role: "user", Content: "Product: " + name + "\nDescription: " + description}}, Options{System: &system, Kind: KindDraft})
	if reply == nil {
		return "", false, nil
	}
	s := *reply
	open := strings.Index(s, "{")
	closeIdx := strings.LastIndex(s, "}")
	if open == -1 || closeIdx <= open {
		return "", false, nil
	}
	var raw struct {
		Unit    string   `json:"unit"`
		Certain *bool    `json:"certain"`
		Sizes   []string `json:"sizes"`
	}
	if err := json.Unmarshal([]byte(s[open:closeIdx+1]), &raw); err != nil {
		return "", false, nil
	}
	sizes = cleanSizes(raw.Sizes)
	unit = strings.ToLower(strings.TrimSpace(raw.Unit))
	if unit == "" {
		return "", false, sizes
	}
	if raw.Certain != nil {
		return unit, *raw.Certain, sizes
	}
	return unit, true, sizes
}

// cleanSizes keeps a judged size list honest: trimmed,
// deduplicated, and capped, so a runaway reply can never
// become the product's facts.
func cleanSizes(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || len(s) > 12 || seen[strings.ToUpper(s)] {
			continue
		}
		seen[strings.ToUpper(s)] = true
		out = append(out, s)
		if len(out) == 16 {
			break
		}
	}
	return out
}

type visionPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

type visionMessage struct {
	Role    string       `json:"role"`
	Content []visionPart `json:"content"`
}

type visionRequest struct {
	Model    string          `json:"model"`
	Messages []visionMessage `json:"messages"`
}

// ChatVision makes one chat completion that carries an image (a
// data URI) beside the text. It returns the reply text, or nil on
// any failure, under the same fail-soft contract as Chat.
func ChatVision(system, text, imageDataURI string) *string {
	if !Enabled() || imageDataURI == "" {
		return nil
	}
	// Vision serves listing composition (DraftListing), a
	// drafting-class call under the turn budget.
	if !spend(KindDraft) {
		return nil
	}
	img := struct {
		URL string `json:"url"`
	}{URL: imageDataURI}
	msgs := []visionMessage{}
	if system != "" {
		msgs = append(msgs, visionMessage{Role: "system", Content: []visionPart{{Type: "text", Text: system}}})
	}
	msgs = append(msgs, visionMessage{Role: "user", Content: []visionPart{
		{Type: "text", Text: text},
		{Type: "image_url", ImageURL: &img},
	}})

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(defaultTimeoutMs)*time.Millisecond)
	defer cancel()
	body, err := json.Marshal(visionRequest{Model: chosenModel(), Messages: msgs})
	if err != nil {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+os.Getenv("META_MODEL_API_KEY"))

	res, err := modelHTTPClient.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil
	}
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil
	}
	return extractReply(data)
}

// DraftListing asks the model to draft a listing from a seller's
// product photo and the words she sent with it, if any. Her own
// words come first in the prompt and the reply is read as exactly
// one JSON object, { name, description }; anything else returns
// nil and the caller works from her words alone. Prices are never
// asked for and never read from the reply.
func DraftListing(imageDataURI, caption string) *ListingDraft {
	system := "You help a market seller turn a photo of her goods into a shop listing. " +
		"Reply with exactly one JSON object, no other text: {\"name\": \"...\", \"description\": \"...\", \"unit\": \"...\", \"unit_certain\": true, \"sizes\": [\"...\"], \"colour\": \"...\"}. " +
		"\"colour\" is the main colour of the goods in this one photo, as a buyer would name it (white, black, rose gold); give an empty string when no single colour dominates, as with a multicoloured print. " +
		"\"sizes\" lists the sizes the goods come in, in the seller's own terms (shoe numbers, UK sizes, S M L, waist numbers), only when her words or the photo itself (a label, a size chart) state them; a stated range like 37 to 42 becomes each size in it. When the goods do not come in sizes, or no sizes are stated, give an empty list. Never guess sizes the goods might come in. " +
		"The name is the short name a buyer would ask for, a few words at most. " +
		"DESCRIPTION RULE: never copy the forwarded caption or any part of its wording wholesale. Summarise it into one short line of at most 20 words, clean enough to sit on a shop shelf, with every emoji and decoration removed. For a laptop give only the generation and processor, the RAM, the storage, the screen and the condition. For anything else give the item, its colour or material, and its condition or key detail, in the same tight shape. Only what the photo or her words support. " +
		"The unit is the word the seller herself would use for how one sale is measured: piece for clothes, shoes and bags, yard for fabric, bottle for creams and oils, and so on, judged from the goods in front of you. " +
		"unit_certain is false only when how the goods are sold genuinely cannot be told from the photo, as with some loose foodstuffs. " +
		"Never mention a price. Never invent a detail you cannot see."
	text := "Draft the listing for the product in this photo."
	if strings.TrimSpace(caption) != "" {
		text = "The seller sent this photo with her own words: \"" + strings.TrimSpace(caption) + "\". " +
			"Draft the listing for the product in the photo. Where her words name or describe the product, use her words first."
	}
	reply := ChatVision(system, text, imageDataURI)
	if reply == nil {
		return nil
	}
	s := *reply
	open := strings.Index(s, "{")
	closeIdx := strings.LastIndex(s, "}")
	if open == -1 || closeIdx <= open {
		return nil
	}
	var raw struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Unit        string   `json:"unit"`
		UnitCertain *bool    `json:"unit_certain"`
		Sizes       []string `json:"sizes"`
		Colour      string   `json:"colour"`
	}
	if err := json.Unmarshal([]byte(s[open:closeIdx+1]), &raw); err != nil {
		return nil
	}
	d := &ListingDraft{
		Name:        strings.TrimSpace(raw.Name),
		Description: strings.TrimSpace(raw.Description),
		Unit:        strings.ToLower(strings.TrimSpace(raw.Unit)),
		Sizes:       cleanSizes(raw.Sizes),
		Colour:      strings.ToLower(strings.TrimSpace(raw.Colour)),
	}
	if raw.UnitCertain != nil {
		d.UnitCertain = *raw.UnitCertain
	} else {
		d.UnitCertain = d.Unit != ""
	}
	if len(d.Name) > 80 {
		d.Name = strings.TrimSpace(d.Name[:80])
	}
	if len(d.Description) > 300 {
		d.Description = strings.TrimSpace(d.Description[:300])
	}
	if d.Name == "" && d.Description == "" {
		return nil
	}
	return d
}

// ParseProposal reads a negotiation proposal out of a model reply. The
// model is asked for exactly one JSON object, { action, price,
// message }; clean JSON, fenced JSON and JSON with prose around it are
// all accepted. Anything else returns nil, and the caller falls back
// to the engine. Shape is checked here; the price itself is verified
// in haggle.CheckProposal.
func ParseProposal(text *string) *haggle.Proposal {
	if text == nil {
		return nil
	}
	s := *text
	open := strings.Index(s, "{")
	closeIdx := strings.LastIndex(s, "}")
	if open == -1 || closeIdx <= open {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s[open:closeIdx+1]), &raw); err != nil || raw == nil {
		return nil
	}
	p := &haggle.Proposal{}
	if rawAction, ok := raw["action"]; ok {
		var a string
		if err := json.Unmarshal(rawAction, &a); err == nil {
			p.Action = a
		}
	}
	if rawPrice, ok := raw["price"]; ok {
		var f float64
		if err := json.Unmarshal(rawPrice, &f); err == nil {
			p.Price = &f
		}
	}
	if rawMessage, ok := raw["message"]; ok {
		var m string
		if err := json.Unmarshal(rawMessage, &m); err == nil {
			p.Message = m
		}
	}
	return p
}
