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
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/valormarc/hackathon-agent/internal/datadir"
)

const graphVersion = "v21.0"

// Result reports how a send went: DryRun when the message was logged
// to the outbox instead of sent, otherwise the Graph API status.
type Result struct {
	DryRun bool
	Status int
	// MessageID is the id WhatsApp gave the sent message, when
	// the API returned one. It lets the agent recognise a later
	// reply that quotes this message.
	MessageID string
}

// SentRecord is one message this number sent, as the phone
// shows it: the id WhatsApp gave it, who it went to, its body
// and kind, and for a tap-to-open button its display text and
// target. OnSend, when set, is told about every send that
// carries an id, live or dry-run: it is how the agent keeps
// its book of sent messages, so a later reply that quotes one
// of them can be resolved to what it said (see the agent's
// quoted-reply handling). A dry-run send has no WhatsApp id,
// so it is given a local one: the bookkeeping is identical,
// and a simulator can quote a dry-run message exactly the way
// a phone quotes a live one.
type SentRecord struct {
	ID      string
	To      string
	Body    string
	Kind    string
	Buttons []string
	URL     string
	At      string
}

// OnSend is the sent-message recorder, set by the agent.
var OnSend func(SentRecord)

var drySeq uint64

func newDryID() string {
	return fmt.Sprintf("dry-%d-%d", time.Now().UnixNano(), atomic.AddUint64(&drySeq, 1))
}

func noteSent(rec outboxRecord) {
	if OnSend == nil || rec.ID == "" {
		return
	}
	OnSend(SentRecord{ID: rec.ID, To: rec.To, Body: rec.Body, Kind: rec.Kind, Buttons: rec.Buttons, URL: rec.URL, At: rec.At})
}

type outboxRecord struct {
	At     string `json:"at"`
	ID     string `json:"id,omitempty"`
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
	// URL is the target of a tap-to-open button message
	// (Kind "cta_url"); Buttons holds its one display text.
	URL string `json:"url,omitempty"`
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
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "WhatsApp send failed: %d %s\n", res.StatusCode, string(body))
	}
	result := Result{Status: res.StatusCode}
	var sent struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &sent) == nil && len(sent.Messages) > 0 {
		result.MessageID = sent.Messages[0].ID
	}
	// The outbox record carries the id WhatsApp gave the
	// message, so the record of what was said can be found
	// again by the id a later quoted reply points at.
	record.ID = result.MessageID
	appendOutbox(record)
	noteSent(record)
	return result
}

// DownloadMedia fetches the bytes behind an inbound media id and
// returns them as a data URI a model call can carry. Two Graph
// steps: the id resolves to a short-lived download address, and
// the address serves the bytes, both under the same token. Any
// failure, including dry-run mode with no token configured,
// returns false, and callers carry on without the picture itself.
func DownloadMedia(mediaID string) (string, bool) {
	data, mime, ok := DownloadMediaBytes(mediaID)
	if !ok {
		return "", false
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), true
}

// DownloadMediaBytes fetches the raw bytes behind an inbound
// media id, with their content type. Any failure, including
// dry-run mode with no token configured, returns false, and
// callers carry on without the picture itself.
func DownloadMediaBytes(mediaID string) (data []byte, mimeType string, ok bool) {
	token := os.Getenv("WHATSAPP_TOKEN")
	if token == "" || mediaID == "" {
		return nil, "", false
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
		return nil, "", false
	}
	var info struct {
		URL      string `json:"url"`
		MimeType string `json:"mime_type"`
	}
	if err := json.Unmarshal(meta, &info); err != nil || info.URL == "" {
		return nil, "", false
	}
	bytesData, status, err := get(info.URL)
	if err != nil || status < 200 || status >= 300 || len(bytesData) == 0 {
		return nil, "", false
	}
	mime := info.MimeType
	if mime == "" {
		mime = "image/jpeg"
	}
	return bytesData, mime, true
}

// UploadMedia posts bytes to the number's own media store and
// returns the fresh media id WhatsApp assigns them. Media a
// customer sent belongs to her conversation; the id is not
// guaranteed to be sendable onward from our number, so sending
// a received picture to someone else goes through this upload
// and carries the fresh id.
func UploadMedia(data []byte, mime string) (string, bool) {
	token := os.Getenv("WHATSAPP_TOKEN")
	phoneID := os.Getenv("WHATSAPP_PHONE_NUMBER_ID")
	if token == "" || phoneID == "" || len(data) == 0 {
		return "", false
	}
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	if err := mw.WriteField("messaging_product", "whatsapp"); err != nil {
		return "", false
	}
	if err := mw.WriteField("type", mime); err != nil {
		return "", false
	}
	part, err := mw.CreateFormFile("file", "image")
	if err != nil {
		return "", false
	}
	if _, err := part.Write(data); err != nil {
		return "", false
	}
	if err := mw.Close(); err != nil {
		return "", false
	}
	url := fmt.Sprintf("https://graph.facebook.com/%s/%s/media", graphVersion, phoneID)
	req, err := http.NewRequest(http.MethodPost, url, buf)
	if err != nil {
		return "", false
	}
	req.Header.Set("authorization", "Bearer "+token)
	req.Header.Set("content-type", mw.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", false
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil || out.ID == "" {
		return "", false
	}
	return out.ID, true
}

// RefreshMediaRef turns a received media id into one of our
// own: the bytes are downloaded and uploaded under this number,
// and the fresh id is returned. Any failure returns false and
// the caller falls back to the received reference.
func RefreshMediaRef(mediaID string) (string, bool) {
	data, mime, ok := DownloadMediaBytes(mediaID)
	if !ok {
		return "", false
	}
	return UploadMedia(data, mime)
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
		dry.ID = newDryID()
		appendOutbox(dry)
		noteSent(dry)
		fmt.Printf("[dry-run buttons -> %s] %s (%s)\n", to, body, strings.Join(titles, " | "))
		return Result{DryRun: true, MessageID: dry.ID}
	}

	payload := buildButtonsPayload(to, body, buttons)
	return post(record, payload)
}

// buildButtonsPayload shapes one reply-buttons message the way
// the Graph API's schema demands: the interactive type is
// "button" (the buttons inside the action are each of type
// "reply"). Any other word in that field, "reply_button" say, is
// refused by Meta as a schema violation and the message silently
// never reaches the phone, which is how this shape earned a test.
func buildButtonsPayload(to, body string, buttons []Button) buttonsPayload {
	payload := buttonsPayload{MessagingProduct: "whatsapp", To: to, Type: "interactive"}
	payload.Interactive.Type = "button"
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
	return payload
}

type ctaPayload struct {
	MessagingProduct string `json:"messaging_product"`
	To               string `json:"to"`
	Type             string `json:"type"`
	Interactive      struct {
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

// SendPayButton sends one tap-to-pay message: a short body
// and a single button that opens the payment page when
// tapped, its display text carrying what she is about to
// pay. In dry-run mode it is logged to the outbox as kind
// "cta_url" with the display text and the target recorded,
// so tests and simulators see exactly what a phone shows.
func SendPayButton(to, body, displayText, url string) Result {
	token := os.Getenv("WHATSAPP_TOKEN")
	phoneID := os.Getenv("WHATSAPP_PHONE_NUMBER_ID")
	_ = os.MkdirAll(datadir.Dir(), 0o755)
	record := outboxRecord{At: nowISO(), To: to, Body: body, Kind: "cta_url", Buttons: []string{displayText}, URL: url}

	if token == "" || phoneID == "" {
		dry := record
		dry.DryRun = true
		dry.ID = newDryID()
		appendOutbox(dry)
		noteSent(dry)
		fmt.Printf("[dry-run cta -> %s] %s (%s)\n", to, body, displayText)
		return Result{DryRun: true, MessageID: dry.ID}
	}

	return post(record, buildCTAPayload(to, body, displayText, url))
}

// buildCTAPayload shapes one tap-to-open button message the
// way the Graph API's schema demands: the interactive type
// is "cta_url" and the action carries the same name, with
// the display text and the target as its parameters.
func buildCTAPayload(to, body, displayText, url string) ctaPayload {
	payload := ctaPayload{MessagingProduct: "whatsapp", To: to, Type: "interactive"}
	payload.Interactive.Type = "cta_url"
	payload.Interactive.Body.Text = body
	payload.Interactive.Action.Name = "cta_url"
	payload.Interactive.Action.Parameters.DisplayText = displayText
	payload.Interactive.Action.Parameters.URL = url
	return payload
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
		dry.ID = newDryID()
		appendOutbox(dry)
		noteSent(dry)
		fmt.Printf("[dry-run -> %s] %s\n", to, body)
		return Result{DryRun: true, MessageID: dry.ID}
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
// The reference sent is expected to be one of this number's own
// media ids: callers forwarding a picture a customer sent refresh
// it first with RefreshMediaRef (download and re-upload), because
// WhatsApp does not promise that an id received on one message can
// be re-sent as-is by this number. The send is recorded in the
// outbox either way, so the merchant always has the caption
// text even if the picture itself fails.
func SendImage(to, mediaRef, caption string) Result {
	token := os.Getenv("WHATSAPP_TOKEN")
	phoneID := os.Getenv("WHATSAPP_PHONE_NUMBER_ID")
	_ = os.MkdirAll(datadir.Dir(), 0o755)
	record := outboxRecord{At: nowISO(), To: to, Body: caption, Kind: "image", MediaRef: mediaRef}

	if token == "" || phoneID == "" {
		dry := record
		dry.DryRun = true
		dry.ID = newDryID()
		appendOutbox(dry)
		noteSent(dry)
		fmt.Printf("[dry-run image -> %s] %s (media %s)\n", to, caption, mediaRef)
		return Result{DryRun: true, MessageID: dry.ID}
	}

	payload := imageSendPayload{MessagingProduct: "whatsapp", To: to, Type: "image"}
	payload.Image.ID = mediaRef
	payload.Image.Caption = caption
	return post(record, payload)
}
