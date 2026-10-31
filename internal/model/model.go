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
	"sync"
	"sync/atomic"
	"strconv"
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
	// Turn is the budget token of the turn making the call
	// (see BeginTurn); empty means uncounted.
	Turn string
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

// A turn's budget is its own. Budgets used to be one
// process-global pair of counters that BeginTurn reset, so
// any background pass that opened a "turn" (the unpaid
// sweep did) silently reset the budget of a live buyer's
// turn mid-flight, and two turns running in parallel shared
// one budget. Now BeginTurn mints a token, the token rides
// on every call the turn makes (Options.Turn), and each
// token's counts are its own. A call carrying no token —
// unit tests, one-off tools, internal composition — is
// uncounted, as calls were before the first BeginTurn.
type turnBudget struct {
	understand atomic.Int64
	draft      atomic.Int64
	born       time.Time
}

var turnBudgets sync.Map // token string -> *turnBudget

var turnSeq atomic.Uint64

// BeginTurn opens a fresh per-turn budget and returns its
// token. Stale budgets (a turn that never closed) are
// swept as new ones open.
func BeginTurn() string {
	now := time.Now()
	turnBudgets.Range(func(k, v any) bool {
		if now.Sub(v.(*turnBudget).born) > 15*time.Minute {
			turnBudgets.Delete(k)
		}
		return true
	})
	token := "turn-" + strconv.FormatUint(turnSeq.Add(1), 10)
	turnBudgets.Store(token, &turnBudget{born: now})
	return token
}

// EndTurn closes a turn's budget.
func EndTurn(token string) {
	if token != "" {
		turnBudgets.Delete(token)
	}
}

// spend charges one call of the kind against the budget of
// the turn named by token, and reports whether it may go
// ahead. No token, or a token whose turn has closed, is
// uncounted.
func spend(kind int, token string) bool {
	if token == "" {
		return true
	}
	v, ok := turnBudgets.Load(token)
	if !ok {
		return true
	}
	b := v.(*turnBudget)
	if kind == KindDraft {
		if b.draft.Add(1) <= draftTurnCap {
			return true
		}
		log.Printf("model: turn budget refused a drafting call (cap %d)", draftTurnCap)
		return false
	}
	if b.understand.Add(1) <= 1 {
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
	if !spend(opts.Kind, opts.Turn) {
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
	// SameAsOpen is the model's identity judgement when the
	// photo was read against a listing the seller already
	// has open: "same" when this photo shows that very
	// product, "different" when it shows another product,
	// "unsure" when it cannot tell, and empty when no
	// listing was open or no judgement was made. Whether
	// two photos show the same goods is a judgement of
	// meaning and belongs to the model; code only routes
	// on the verdict, never re-judges it from word overlap.
	SameAsOpen string
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
func JudgeShape(name, description, turn string) (unit string, certain bool, sizes []string) {
	system := "You help a market seller list her goods. Read the product below and judge how it is sold, the way the seller herself would say it, and which sizes it comes in. " +
		"Reply with exactly one JSON object, no other text: {\"unit\": \"...\", \"certain\": true, \"sizes\": [\"...\"]}. " +
		"The unit is one word or two: piece for clothes, shoes and bags, yard for fabric, bottle for creams and oils, kilo or bag for foodstuffs, and so on. " +
		"certain is false only when how it is sold genuinely cannot be told from the name and description. " +
		"sizes lists only sizes the words themselves state, in the seller's own terms (shoe numbers, UK sizes, S M L, waist numbers): a stated range like 37 to 42 becomes each size in it. When the words state no sizes, or the goods do not come in sizes, give an empty list. Never guess sizes such goods usually come in."
	reply := Chat([]Message{{Role: "user", Content: "Product: " + name + "\nDescription: " + description}}, Options{System: &system, Kind: KindDraft, Turn: turn})
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

// ChatVisionTurn makes one chat completion that carries
// an image (a data URI) beside the text, charged against
// one turn's budget (see Options.Turn). It returns the
// reply text, or nil on any failure, under the same
// fail-soft contract as Chat. There is deliberately no
// tokenless form: the fourth audit found the old one
// spending uncounted — spend("") waves a call through —
// so every vision call now names the turn it belongs to.
func ChatVisionTurn(system, text, imageDataURI, turn string) *string {
	if !Enabled() || imageDataURI == "" {
		return nil
	}
	// Vision serves listing composition (DraftListing), a
	// drafting-class call under the turn budget.
	if !spend(KindDraft, turn) {
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

// DraftListingAgainst asks the model to draft a listing
// from a seller's product photo and the words she sent
// with it, if any, with the seller's already-open listing
// in view when there is one: the model drafts this
// photo's listing and judges whether the photo shows
// that same product. The judgement rides in the same
// single call, so a photo turn still costs one model
// call. Her own words come first in the prompt and the
// reply is read as exactly one JSON object,
// { name, description }; anything else returns nil and
// the caller works from her words alone. Prices are
// never asked for and never read from the reply. As with
// ChatVisionTurn there is no tokenless form: every call
// names its turn and spends against its budget.
func DraftListingAgainst(imageDataURI, caption, openName, openDescription, turn string) *ListingDraft {
	system := "You help a market seller turn a photo of her goods into a shop listing. " +
		"Reply with exactly one JSON object, no other text: {\"name\": \"...\", \"description\": \"...\", \"unit\": \"...\", \"unit_certain\": true, \"sizes\": [\"...\"], \"colour\": \"...\"}. " +
		"\"colour\" is the main colour of the goods in this one photo, as a buyer would name it (white, black, rose gold); give an empty string when no single colour dominates, as with a multicoloured print. " +
		"\"sizes\" lists the sizes the goods come in, in the seller's own terms (shoe numbers, UK sizes, S M L, waist numbers), only when her words or the photo itself (a label, a size chart) state them; a stated range like 37 to 42 becomes each size in it. When the goods do not come in sizes, or no sizes are stated, give an empty list. Never guess sizes the goods might come in. " +
		"The name is the short name a buyer would ask for, a few words at most. " +
		"DESCRIPTION RULE: never copy the forwarded caption or any part of its wording wholesale. Summarise it into one short line of at most 20 words, clean enough to sit on a shop shelf, with every emoji and decoration removed. For a laptop give only the generation and processor, the RAM, the storage, the screen and the condition. For anything else give the item, its colour or material, and its condition or key detail, in the same tight shape. Only what the photo or her words support. " +
		"The unit is the word the seller herself would use for how one sale is measured: piece for clothes, shoes and bags, yard for fabric, bottle for creams and oils, and so on, judged from the goods in front of you. " +
		"unit_certain is false only when how the goods are sold genuinely cannot be told from the photo, as with some loose foodstuffs. " +
		"Never mention a price. Never invent a detail you cannot see."
	if openName != "" {
		system += " The seller already has a listing open for \"" + openName + "\""
		if strings.TrimSpace(openDescription) != "" {
			system += " (" + strings.TrimSpace(openDescription) + ")"
		}
		system += ". Add \"same_as_open\" to the JSON: \"same\" when this photo shows that very same product — the same goods a buyer would receive, including the same design in another colour — " +
			"\"different\" when it shows another product, and \"unsure\" when you cannot tell. Judge by construction, not by kind: compare how the goods are made — the shape, the arrangement of straps or parts, closures and hardware, pattern and material. " +
			"\"same\" only when this photo could be another angle, or another colour, of the very item in the open listing. If any visible part of the construction differs — a thong post where the other has a T-bar, studs where the other is plain, a different print — it is \"different\", even when the kind, the price and most of the look agree."
	}
	text := "Draft the listing for the product in this photo."
	if strings.TrimSpace(caption) != "" {
		text = "The seller sent this photo with her own words: \"" + strings.TrimSpace(caption) + "\". " +
			"Draft the listing for the product in the photo. Where her words name or describe the product, use her words first."
	}
	reply := ChatVisionTurn(system, text, imageDataURI, turn)
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
		SameAsOpen  string   `json:"same_as_open"`
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
	switch strings.ToLower(strings.TrimSpace(raw.SameAsOpen)) {
	case "same", "different", "unsure":
		d.SameAsOpen = strings.ToLower(strings.TrimSpace(raw.SameAsOpen))
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
