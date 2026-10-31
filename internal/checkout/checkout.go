// Package checkout is the Seev (SeevCash) checkout: hosted MoMo
// payment links.
//
// The API surface mirrors the proven sandbox
// integration from my earlier Seev work, not a guess: POST /api/v1/developer/payments with a
// Bearer key returns data.checkout_url and data.reference, amounts go
// in as pesewas, and GET /api/v1/developer/payments/{reference} reads
// a session back without credentials, its amount in major units.
//
// Money model: the customer pays the
// agreed price by MoMo on Seev's hosted page. Seev takes 1.5 percent.
// The rest settles to the merchant's own MoMo the next day. Seev is
// MoMo only today; there is no card option to offer.
//
// Without SEEV_SECRET_KEY the flow stays in dry-run: a clearly marked
// placeholder link keeps the conversation testable, and nothing about
// the placeholder pretends to be a real payment page.
package checkout

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/valormarc/hackathon-agent/internal/money"
)

const defaultBase = "https://api.seevplus.com"

// seevHTTPClient carries every call to Seev. The default
// client and http.Get have no timeout: a hung Seev
// connection would hold a checkout or a settlement open
// indefinitely. Thirty seconds is far beyond a healthy
// Seev answer.
var seevHTTPClient = &http.Client{Timeout: 30 * time.Second}

// FeeBPS is Seev's fee in basis points: 1.5 percent.
const FeeBPS = 150

// Enabled reports whether a Seev secret key is configured.
func Enabled() bool {
	return os.Getenv("SEEV_SECRET_KEY") != ""
}

func apiBase() string {
	base := os.Getenv("SEEV_API_BASE")
	if base == "" {
		base = defaultBase
	}
	return strings.TrimRight(base, "/")
}

// Settlement is what a sale of a given amount resolves to: the fee
// Seev takes and the net that settles to the merchant.
type Settlement struct {
	Fee float64
	Net float64
}

// SettlementFor computes what the merchant actually receives on a sale
// of amount cedis: Seev's 1.5 percent comes off, the remainder settles
// to her MoMo the next day. Worked in pesewas, fee rounded half up, so
// the figures the merchant is told always sum back to the price the
// customer paid.
func SettlementFor(amount float64) Settlement {
	amountPesewas := money.Round(math.Max(0, amount*100))
	feePesewas := money.Round(amountPesewas * FeeBPS / 10000)
	return Settlement{Fee: feePesewas / 100, Net: (amountPesewas - feePesewas) / 100}
}

// Link is the outcome of opening a hosted checkout. URL is nil when
// Seev was configured but the session could not be created.
type Link struct {
	URL         *string
	DryRun      bool
	ProviderRef *string
}

// FreshKey returns an idempotency key that has never been
// used: the order reference plus a random suffix. Seev's
// dedupe lives entirely on this key, and a reference's own
// keys are a finite, burnable space (a serial reused after
// a store reset collides with sessions minted in a former
// era, at other amounts, plain key and attempt keys
// alike), so every mint after the first rides a fresh key.
// Settlement never reads the key: it follows the provider
// reference of the session that actually opened.
func FreshKey(reference string) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return reference + "-x"
	}
	return reference + "-" + hex.EncodeToString(b[:])
}

// LinkRequest carries the inputs to CreatePaymentLink.
type LinkRequest struct {
	Amount    float64
	Reference string
	// IdempotencyKey is the key this one mint rides. Empty
	// means the reference itself; a mint that must not
	// collide with anything before it passes FreshKey.
	IdempotencyKey string
	RecipientName  string // defaults to "Customer"
	// Subaccount is the selling shop's own Seev subaccount code,
	// when it has one: the payment is split at settlement, the
	// seller's share going to her own MoMo.
	Subaccount string
}

type createBody struct {
	Type      string `json:"type"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Recipient struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"recipient"`
	Meta struct {
		OrderReference string `json:"order_reference"`
	} `json:"meta"`
	RedirectURL string `json:"redirect_url,omitempty"`
	Subaccount  string `json:"subaccount,omitempty"`
}

// CreatePaymentLink opens a hosted checkout for an agreed deal. It
// returns the hosted URL in dry-run placeholder form when no key is
// configured, the real session URL on success, or a nil URL when Seev
// was configured but the session could not be created, so the caller
// can fail honestly instead of sending the customer a link that does
// not work.
func CreatePaymentLink(r LinkRequest) Link {
	if !Enabled() {
		url := "https://pay.example.test/" + r.Reference
		return Link{URL: &url, DryRun: true, ProviderRef: nil}
	}
	name := r.RecipientName
	if name == "" {
		name = "Customer"
	}
	var body createBody
	body.Type = "checkout"
	body.Amount = money.RoundInt(r.Amount * 100) // pesewas
	body.Currency = "GHS"
	body.Recipient.Name = name
	body.Recipient.Email = "customer@example.com"
	body.Meta.OrderReference = r.Reference
	body.Subaccount = r.Subaccount
	if v := os.Getenv("SEEV_REDIRECT_URL"); v != "" {
		body.RedirectURL = v
	}
	data, err := json.Marshal(body)
	if err != nil {
		return Link{URL: nil, DryRun: false, ProviderRef: nil}
	}
	req, err := http.NewRequest(http.MethodPost, apiBase()+"/api/v1/developer/payments", bytes.NewReader(data))
	if err != nil {
		return Link{URL: nil, DryRun: false, ProviderRef: nil}
	}
	req.Header.Set("authorization", "Bearer "+os.Getenv("SEEV_SECRET_KEY"))
	req.Header.Set("content-type", "application/json")
	idem := r.IdempotencyKey
	if idem == "" {
		idem = r.Reference
	}
	req.Header.Set("idempotency-key", idem)
	res, err := seevHTTPClient.Do(req)
	if err != nil {
		// A failed link must never be a silent one: the provider's
		// answer (or the transport error) is the only evidence of
		// why, so it is logged where the operator can read it. The
		// buyer still hears only that the link is not coming up.
		log.Printf("seev checkout for %s: request failed: %v", r.Reference, err)
		return Link{URL: nil, DryRun: false, ProviderRef: nil}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		log.Printf("seev checkout for %s: reading the response failed: %v", r.Reference, err)
		return Link{URL: nil, DryRun: false, ProviderRef: nil}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		log.Printf("seev checkout for %s: status %d: %.300s", r.Reference, res.StatusCode, string(raw))
		return Link{URL: nil, DryRun: false, ProviderRef: nil}
	}
	var parsed struct {
		Data *struct {
			CheckoutURL string `json:"checkout_url"`
			Reference   string `json:"reference"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed.Data == nil || parsed.Data.CheckoutURL == "" {
		return Link{URL: nil, DryRun: false, ProviderRef: nil}
	}
	if parsed.Data.Reference == "" {
		// A checkout URL without its session reference is
		// not a usable link: the whole settlement design
		// reads the session back before believing money
		// moved, and an order recorded with no reference
		// falls into the event's-word-alone road meant for
		// dry runs. Refuse it here, where the partial
		// answer arrives, and let the caller retry or tell
		// the buyer plainly (the 2026-10-30 audit's chain:
		// a partially identified session settling real
		// money on an event alone).
		log.Printf("seev checkout for %s: success response carried no session reference; refusing the link", r.Reference)
		return Link{URL: nil, DryRun: false, ProviderRef: nil}
	}
	url := parsed.Data.CheckoutURL
	rr := parsed.Data.Reference
	return Link{URL: &url, DryRun: false, ProviderRef: &rr}
}

// PaidAmountMatches reports whether a read-back both says
// paid AND states the charged amount exactly. A read-back
// that states no amount proves nothing about the amount:
// a reused reference can point at a session from a former
// era at a different figure, so status alone never settles
// (the rule every settlement road now shares).
func PaidAmountMatches(check *Verification, charged float64) bool {
	return check != nil && IsPaidStatus(check.Status) && check.AmountMajor != nil &&
		money.Round(*check.AmountMajor*100) == money.Round(charged*100)
}

// Verification is a checkout session read back from Seev. Status is
// empty when Seev did not report one; AmountMajor is nil when the
// amount was missing or not a number.
type Verification struct {
	Status      string
	AmountMajor *float64
}

// VerifyPayment reads a checkout session back from Seev. It takes no
// credentials, per Seev's docs. The amount comes back in PESEWAS,
// the same unit creation sends: proven live on 2026-10-28, when a
// GH₵1 session (created at 100) read back amount 100 completed, and
// a GH₵2.50 session (created at 250) read back 250. An earlier
// reading of the docs took the read-back for major units, and
// every real payment then failed the amount check as a hundredfold
// "mismatch"; AmountMajor here is the converted figure, cedis, so
// callers compare like with like. It returns nil on any failure.
func VerifyPayment(providerRef string) *Verification {
	if providerRef == "" {
		return nil
	}
	res, err := seevHTTPClient.Get(apiBase() + "/api/v1/developer/payments/" + providerRef)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil
	}
	var parsed struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	d := parsed.Data
	out := &Verification{}
	if rawStatus, ok := d["status"]; ok {
		var s string
		if err := json.Unmarshal(rawStatus, &s); err == nil {
			out.Status = s
		}
	}
	if rawAmount, ok := d["amount"]; ok {
		if pesewas := majorAmount(rawAmount); pesewas != nil {
			major := *pesewas / 100
			out.AmountMajor = &major
		}
	}
	return out
}

// majorAmount reads an amount that may arrive as a JSON number or a
// numeric string, as Seev sends both shapes.
func majorAmount(raw json.RawMessage) *float64 {
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil
		}
		return &n
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return &f
		}
	}
	return nil
}

var paidWords = []string{"completed", "success", "successful", "paid"}
var failedWords = []string{"failed", "cancelled", "canceled", "expired", "declined"}

func inWords(words []string, status string) bool {
	s := strings.ToLower(status)
	for _, w := range words {
		if s == w {
			return true
		}
	}
	return false
}

// IsPaidStatus reports whether a Seev status means paid.
func IsPaidStatus(status string) bool { return inWords(paidWords, status) }

// IsFailedStatus reports whether a Seev status means the payment
// failed, was cancelled or expired.
func IsFailedStatus(status string) bool { return inWords(failedWords, status) }

// VerifyWebhookSignature verifies a Seev webhook delivery, scheme
// proven in the sandbox integration: the signature is "v1=" plus the
// hex HMAC-SHA256 of "timestamp.rawBody" under the webhook secret, and
// the timestamp must sit within five minutes after to one minute
// before nowSeconds, as replay protection. It fails closed on
// anything missing.
func VerifyWebhookSignature(rawBody, timestamp, signature, secret string, nowSeconds int64) bool {
	if rawBody == "" || timestamp == "" || signature == "" || secret == "" {
		return false
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		// JavaScript Number() also accepts forms like "1800000000.0"
		// only when the value is an integer; keep the strict integer
		// reading and fail closed otherwise.
		f, ferr := strconv.ParseFloat(strings.TrimSpace(timestamp), 64)
		if ferr != nil || f != math.Trunc(f) {
			return false
		}
		ts = int64(f)
	}
	drift := nowSeconds - ts
	if drift < -60 || drift > 300 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%s.%s", timestamp, rawBody)))
	expected := "v1=" + hex.EncodeToString(mac.Sum(nil))
	if len(expected) != len(signature) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}

// VerifyWebhookSignatureNow is VerifyWebhookSignature against the
// current time.
func VerifyWebhookSignatureNow(rawBody, timestamp, signature, secret string) bool {
	return VerifyWebhookSignature(rawBody, timestamp, signature, secret, time.Now().Unix())
}

// Event is the fields the webhook handler needs out of a Seev event
// body.
type Event struct {
	Reference      *string
	OrderReference *string
	Status         *string
	AmountMajor    *float64
}

func strField(m map[string]json.RawMessage, key string) *string {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || s == "" {
		return nil
	}
	return &s
}

// ParseWebhookEvent pulls the fields the webhook handler needs out of
// a Seev event body, whether Seev nests them under data or sends them
// flat. Our own order reference travels in the meta we set at
// creation; Seev's session reference identifies the payment itself.
// The amount, when present, is in major units.
func ParseWebhookEvent(body []byte) Event {
	empty := Event{}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil || root == nil {
		return empty
	}
	d := root
	if rawData, ok := root["data"]; ok {
		var dm map[string]json.RawMessage
		if err := json.Unmarshal(rawData, &dm); err == nil && dm != nil {
			d = dm
		}
	}
	meta := map[string]json.RawMessage{}
	if rawMeta, ok := d["meta"]; ok {
		var mm map[string]json.RawMessage
		if err := json.Unmarshal(rawMeta, &mm); err == nil && mm != nil {
			meta = mm
		}
	} else if rawMeta, ok := root["meta"]; ok {
		var mm map[string]json.RawMessage
		if err := json.Unmarshal(rawMeta, &mm); err == nil && mm != nil {
			meta = mm
		}
	}
	ev := Event{
		Reference:      strField(d, "reference"),
		OrderReference: strField(meta, "order_reference"),
		Status:         strField(d, "status"),
	}
	if rawAmount, ok := d["amount"]; ok {
		ev.AmountMajor = majorAmount(rawAmount)
	}
	return ev
}
