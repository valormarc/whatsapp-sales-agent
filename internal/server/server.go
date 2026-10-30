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
	"time"

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
		// Fail closed. An unset secret must never read as
		// "no signature needed": the day an env is lost on a
		// restart is the day anyone who finds this URL can
		// post as any phone number, owners included. The one
		// exception is a process the operator has explicitly
		// declared a sandbox (WHATSAPP_DRY_RUN=1, the proof
		// and simulator fleet): it holds no credentials to
		// send with and settles nothing real, and its
		// harnesses post unsigned by construction. A live
		// process never carries that flag, so for it an
		// unset secret rejects everything. Run says which
		// case it is at startup.
		return os.Getenv("WHATSAPP_DRY_RUN") == "1"
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
					// Context is present when the message quotes
					// an earlier one: its id names the quoted
					// message, and its from names who sent that
					// message, which is how the agent knows what
					// a reply points at, and whose words it is
					// pointing at.
					Context *struct {
						ID   string `json:"id"`
						From string `json:"from"`
					} `json:"context"`
					Text *struct {
						Body string `json:"body"`
					} `json:"text"`
					Image *struct {
						ID      string `json:"id"`
						Caption string `json:"caption"`
					} `json:"image"`
					Interactive *struct {
						Type        string `json:"type"`
						ButtonReply *struct {
							ID    string `json:"id"`
							Title string `json:"title"`
						} `json:"button_reply"`
						ListReply *struct {
							ID          string `json:"id"`
							Title       string `json:"title"`
							Description string `json:"description"`
						} `json:"list_reply"`
						NFMReply *struct {
							ResponseJSON string `json:"response_json"`
							Name         string `json:"name"`
							Body         string `json:"body"`
						} `json:"nfm_reply"`
					} `json:"interactive"`
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

	mux.HandleFunc("GET /paid", pageHandler(paidPage))

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
		if mode == "subscribe" && verifyToken() != "" && len(token) == len(verifyToken()) && subtle.ConstantTimeCompare([]byte(token), []byte(verifyToken())) == 1 {
			w.Header().Set("content-type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(challenge))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	})

	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
		// The body is capped before anything reads it: the
		// signature check needs the bytes, so an uncapped
		// read would let an unauthenticated request eat
		// memory before it is ever verified. Two megabytes
		// is far beyond any real Meta batch.
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_, _ = w.Write([]byte("too large"))
			return
		}
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
						contextID, contextFrom := "", ""
						if msg.Context != nil {
							contextID = msg.Context.ID
							contextFrom = msg.Context.From
						}
						switch msg.Type {
						case "text":
							body := ""
							if msg.Text != nil {
								body = msg.Text.Body
							}
							agent.HandleInbound(agent.Inbound{From: msg.From, Text: body, MessageID: msg.ID, ContextID: contextID, ContextFrom: contextFrom})
						case "image":
							var imageID, caption string
							if msg.Image != nil {
								imageID = msg.Image.ID
								caption = msg.Image.Caption
							}
							agent.HandleInbound(agent.Inbound{From: msg.From, ImageID: imageID, Caption: caption, MessageID: msg.ID, ContextID: contextID, ContextFrom: contextFrom})
						case "interactive":
							// A tap on a reply button the agent sent,
							// or a row of a list sheet. Only the id
							// travels inward; the agent decides what
							// a tap means, and a tap on anything it
							// does not know is ignored.
							buttonID, listID, flowReply := "", "", ""
							if msg.Interactive != nil {
								if msg.Interactive.ButtonReply != nil {
									buttonID = msg.Interactive.ButtonReply.ID
								}
								if msg.Interactive.ListReply != nil {
									listID = msg.Interactive.ListReply.ID
								}
								if msg.Interactive.NFMReply != nil {
									flowReply = msg.Interactive.NFMReply.ResponseJSON
								}
							}
							if buttonID != "" {
								agent.HandleInbound(agent.Inbound{From: msg.From, ButtonID: buttonID, MessageID: msg.ID, ContextID: contextID, ContextFrom: contextFrom})
							} else if listID != "" {
								agent.HandleInbound(agent.Inbound{From: msg.From, ListID: listID, MessageID: msg.ID, ContextID: contextID, ContextFrom: contextFrom})
							} else if flowReply != "" {
								agent.HandleInbound(agent.Inbound{From: msg.From, FlowReply: flowReply, MessageID: msg.ID, ContextID: contextID, ContextFrom: contextFrom})
							}
						}
					}()
				}
			}
		}
	})

	mux.HandleFunc("POST /webhooks/seev", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
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
	agent.StartUnpaidWatch()
	agent.StartSessionSnapshots()
	if os.Getenv("WHATSAPP_APP_SECRET") == "" {
		if os.Getenv("WHATSAPP_DRY_RUN") == "1" {
			fmt.Fprintln(os.Stderr, "NOTE: dry run with no WHATSAPP_APP_SECRET; webhook signatures are not being checked (sandbox only)")
		} else {
			fmt.Fprintln(os.Stderr, "WARNING: WHATSAPP_APP_SECRET is not set; the WhatsApp webhook will reject every request until it is")
		}
	}
	if os.Getenv("SEEV_WEBHOOK_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "WARNING: SEEV_WEBHOOK_SECRET is not set; the payment webhook will reject every event until it is")
	}
	fmt.Printf("hackathon-agent listening on :%d\n", port)
	// Header and read timeouts close the slowloris door: a
	// connection that dribbles its request is cut in
	// seconds. There is deliberately no WriteTimeout: the
	// webhook acks first and then handles the turn inline,
	// and a long turn must not be severed mid-handling
	// (Meta already holds its flushed 200 by then).
	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(port),
		Handler:           Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	return srv.ListenAndServe()
}
