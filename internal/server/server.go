// Package server is the webhook server for the Meta WhatsApp Cloud
// API.
//
//	GET  /webhook        Meta verification handshake (hub.challenge)
//	POST /webhook        inbound messages; acks fast, processes after
//	GET  /healthz        liveness
//	POST /webhooks/seev  Seev payment events: marks orders paid, notifies
//	GET  /privacy        privacy policy page
//	GET  /terms          terms of service page
package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/agent"
	"github.com/valormarc/hackathon-agent/internal/seevwebhook"
)

// verifyToken is the webhook verification token, read from the
// WHATSAPP_VERIFY_TOKEN variable. There is no fallback: when it is
// unset, verification fails closed rather than trusting a guessable
// default.
func verifyToken() string {
	return os.Getenv("WHATSAPP_VERIFY_TOKEN")
}

func signatureOK(rawBody []byte, header string) bool {
	secret := os.Getenv("WHATSAPP_APP_SECRET")
	if secret == "" {
		return true // dev mode only; set the secret in production
	}
	if !strings.HasPrefix(header, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if len(expected) != len(header) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(header)) == 1
}

type webhookPayload struct {
	Entry []struct {
		Changes []struct {
			Value struct {
				Messages []struct {
					From string `json:"from"`
					ID   string `json:"id"`
					Type string `json:"type"`
					Text *struct {
						Body string `json:"body"`
					} `json:"text"`
					Image *struct {
						ID      string `json:"id"`
						Caption string `json:"caption"`
					} `json:"image"`
				} `json:"messages"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

// Handler returns the HTTP handler for the agent.
func Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /privacy", pageHandler(privacyPage))

	mux.HandleFunc("GET /terms", pageHandler(termsPage))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("GET /webhook", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		mode := q.Get("hub.mode")
		token := q.Get("hub.verify_token")
		challenge := q.Get("hub.challenge")
		if mode == "subscribe" && verifyToken() != "" && token == verifyToken() {
			w.Header().Set("content-type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(challenge))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	})

	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !signatureOK(raw, r.Header.Get("x-hub-signature-256")) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("bad signature"))
			return
		}
		// Ack fast, then process. The response is flushed first so the
		// ack reaches Meta before any handling work begins, as in the
		// reference implementation.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("EVENT_RECEIVED"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		var payload webhookPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return
		}
		for _, entry := range payload.Entry {
			for _, change := range entry.Changes {
				for _, msg := range change.Value.Messages {
					func() {
						defer func() {
							if rec := recover(); rec != nil {
								fmt.Fprintf(os.Stderr, "inbound handling failed: %v\n", rec)
							}
						}()
						switch msg.Type {
						case "text":
							body := ""
							if msg.Text != nil {
								body = msg.Text.Body
							}
							agent.HandleInbound(agent.Inbound{From: msg.From, Text: body, MessageID: msg.ID})
						case "image":
							var imageID, caption string
							if msg.Image != nil {
								imageID = msg.Image.ID
								caption = msg.Image.Caption
							}
							agent.HandleInbound(agent.Inbound{From: msg.From, ImageID: imageID, Caption: caption, MessageID: msg.ID})
						}
					}()
				}
			}
		}
	})

	mux.HandleFunc("POST /webhooks/seev", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		status := seevwebhook.Handle(string(raw), r.Header.Get("x-seev-timestamp"), r.Header.Get("x-seev-signature"))
		w.WriteHeader(status)
		if status == 200 {
			_, _ = w.Write([]byte("ok"))
		} else {
			_, _ = w.Write([]byte("rejected"))
		}
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	})

	return mux
}

// Port returns the port to listen on: the PORT variable, or 4000.
func Port() int {
	if p := os.Getenv("PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			return n
		}
	}
	return 4000
}

// Run starts the server and blocks.
func Run() error {
	port := Port()
	fmt.Printf("hackathon-agent listening on :%d\n", port)
	return http.ListenAndServe(":"+strconv.Itoa(port), Handler())
}
