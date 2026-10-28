// Package whatsapp is the Meta WhatsApp Cloud API client.
//
// Dry-run mode: with no token configured, outbound messages are
// appended to data/outbox.jsonl and logged, so the whole flow runs
// locally with no credentials. The simulator reads the outbox to show
// the conversation.
package whatsapp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/valormarc/hackathon-agent/internal/datadir"
)

const graphVersion = "v21.0"

// Result reports how a send went: DryRun when the message was logged
// to the outbox instead of sent, otherwise the Graph API status.
type Result struct {
	DryRun bool
	Status int
}

type outboxRecord struct {
	At     string `json:"at"`
	To     string `json:"to"`
	Body   string `json:"body"`
	DryRun bool   `json:"dryRun,omitempty"`
	// Kind is "image" for an image send (empty means a plain text
	// message, as before). MediaRef is the media reference the
	// image carries; Body holds its caption.
	Kind     string `json:"kind,omitempty"`
	MediaRef string `json:"mediaRef,omitempty"`
	// Buttons holds the reply button titles of an interactive
	// buttons message (Kind "buttons"); Body holds its text.
	Buttons []string `json:"buttons,omitempty"`
}

type sendPayload struct {
	MessagingProduct string `json:"messaging_product"`
	To               string `json:"to"`
	Type             string `json:"type"`
	Text             struct {
		Body string `json:"body"`
	} `json:"text"`
}

type imageSendPayload struct {
	MessagingProduct string `json:"messaging_product"`
	To               string `json:"to"`
	Type             string `json:"type"`
	Image            struct {
		ID      string `json:"id"`
		Caption string `json:"caption,omitempty"`
	} `json:"image"`
}

func nowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func appendOutbox(rec outboxRecord) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(rec)
	f, err := os.OpenFile(datadir.Path("outbox.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(buf.Bytes())
}

// post delivers one prepared payload to the Graph API and writes
// the outbox record either way, mirroring SendText's discipline:
// the outbox is the record of what the agent tried to say.
func post(record outboxRecord, payload any) Result {
	token := os.Getenv("WHATSAPP_TOKEN")
	phoneID := os.Getenv("WHATSAPP_PHONE_NUMBER_ID")
	data, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://graph.facebook.com/%s/%s/messages", graphVersion, phoneID)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(data)))
	if err != nil {
		appendOutbox(record)
		return Result{}
	}
	req.Header.Set("authorization", "Bearer "+token)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		appendOutbox(record)
		return Result{}
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		text, _ := io.ReadAll(res.Body)
		fmt.Fprintf(os.Stderr, "WhatsApp send failed: %d %s\n", res.StatusCode, string(text))
	}
	appendOutbox(record)
	return Result{Status: res.StatusCode}
}

// DownloadMedia fetches the bytes behind an inbound media id and
// returns them as a data URI a model call can carry. Two Graph
// steps: the id resolves to a short-lived download address, and
// the address serves the bytes, both under the same token. Any
// failure, including dry-run mode with no token configured,
// returns false, and callers carry on without the picture itself.
func DownloadMedia(mediaID string) (string, bool) {
	token := os.Getenv("WHATSAPP_TOKEN")
	if token == "" || mediaID == "" {
		return "", false
	}
	get := func(url string) ([]byte, int, error) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer res.Body.Close()
		data, err := io.ReadAll(io.LimitReader(res.Body, 10<<20))
		return data, res.StatusCode, err
	}
	meta, status, err := get(fmt.Sprintf("https://graph.facebook.com/%s/%s", graphVersion, mediaID))
	if err != nil || status < 200 || status >= 300 {
		return "", false
	}
	var info struct {
		URL      string `json:"url"`
		MimeType string `json:"mime_type"`
	}
	if err := json.Unmarshal(meta, &info); err != nil || info.URL == "" {
		return "", false
	}
	bytesData, status, err := get(info.URL)
	if err != nil || status < 200 || status >= 300 || len(bytesData) == 0 {
		return "", false
	}
	mime := info.MimeType
	if mime == "" {
		mime = "image/jpeg"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(bytesData), true
}

// Button is one reply button on an interactive buttons message.
// The title is what the reader sees and taps; the id is what the
// tap reports back in the webhook.
type Button struct {
	ID    string
	Title string
}

type buttonsPayload struct {
	MessagingProduct string `json:"messaging_product"`
	To               string `json:"to"`
	Type             string `json:"type"`
	Interactive      struct {
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

// SendButtons sends one interactive reply-buttons message (the
// reader taps a button instead of typing), or logs it to the
// outbox in dry-run mode with its button titles recorded, so tests
// and simulators see exactly what a phone would show.
func SendButtons(to, body string, buttons []Button) Result {
	token := os.Getenv("WHATSAPP_TOKEN")
	phoneID := os.Getenv("WHATSAPP_PHONE_NUMBER_ID")
	_ = os.MkdirAll(datadir.Dir(), 0o755)
	titles := make([]string, 0, len(buttons))
	for _, b := range buttons {
		titles = append(titles, b.Title)
	}
	record := outboxRecord{At: nowISO(), To: to, Body: body, Kind: "buttons", Buttons: titles}

	if token == "" || phoneID == "" {
		dry := record
		dry.DryRun = true
		appendOutbox(dry)
		fmt.Printf("[dry-run buttons -> %s] %s (%s)\n", to, body, strings.Join(titles, " | "))
		return Result{DryRun: true}
	}

	payload := buttonsPayload{MessagingProduct: "whatsapp", To: to, Type: "interactive"}
	payload.Interactive.Type = "reply_button"
	payload.Interactive.Body.Text = body
	for _, b := range buttons {
		var btn struct {
			Type  string `json:"type"`
			Reply struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"reply"`
		}
		btn.Type = "reply"
		btn.Reply.ID = b.ID
		btn.Reply.Title = b.Title
		payload.Interactive.Action.Buttons = append(payload.Interactive.Action.Buttons, btn)
	}
	return post(record, payload)
}

// SendText sends one text message, or logs it to the outbox in
// dry-run mode when no token or phone number id is configured.
func SendText(to, body string) Result {
	token := os.Getenv("WHATSAPP_TOKEN")
	phoneID := os.Getenv("WHATSAPP_PHONE_NUMBER_ID")
	_ = os.MkdirAll(datadir.Dir(), 0o755)
	record := outboxRecord{At: nowISO(), To: to, Body: body}

	if token == "" || phoneID == "" {
		dry := record
		dry.DryRun = true
		appendOutbox(dry)
		fmt.Printf("[dry-run -> %s] %s\n", to, body)
		return Result{DryRun: true}
	}

	payload := sendPayload{MessagingProduct: "whatsapp", To: to, Type: "text"}
	payload.Text.Body = body
	return post(record, payload)
}

// SendImage sends one image message carrying a media reference,
// with the caption as its text, or logs it to the outbox in dry-run
// mode (an entry of kind "image" with the reference and caption,
// which is what tests assert against).
//
// Live-mode limitation, stated plainly: the reference this is used
// with is the media id from the buyer's own inbound message, and
// WhatsApp does not promise that an id received on one message can
// be re-sent as-is by this number. A fully robust live flow would
// download the buyer's media and re-upload it under our own number
// first, then send the fresh id. That download and re-upload step
// is not built here; in live mode the send is attempted with the
// received reference and recorded in the outbox either way, so the
// merchant always has the claim text even if the picture itself
// needs the follow-up work.
func SendImage(to, mediaRef, caption string) Result {
	token := os.Getenv("WHATSAPP_TOKEN")
	phoneID := os.Getenv("WHATSAPP_PHONE_NUMBER_ID")
	_ = os.MkdirAll(datadir.Dir(), 0o755)
	record := outboxRecord{At: nowISO(), To: to, Body: caption, Kind: "image", MediaRef: mediaRef}

	if token == "" || phoneID == "" {
		dry := record
		dry.DryRun = true
		appendOutbox(dry)
		fmt.Printf("[dry-run image -> %s] %s (media %s)\n", to, caption, mediaRef)
		return Result{DryRun: true}
	}

	payload := imageSendPayload{MessagingProduct: "whatsapp", To: to, Type: "image"}
	payload.Image.ID = mediaRef
	payload.Image.Caption = caption
	return post(record, payload)
}
