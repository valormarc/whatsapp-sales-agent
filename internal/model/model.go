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
	"net/http"
	"os"
	"strings"
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
// 20 second timeout when set.
type Options struct {
	Context   *persona.Context
	System    *string
	TimeoutMs *int
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
func Chat(messages []Message, opts Options) *string {
	if !Enabled() {
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

	res, err := http.DefaultClient.Do(req)
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
