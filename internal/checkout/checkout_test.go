package checkout

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/orders"
)

// A local stand-in for the Seev API, shaped like the proven sandbox
// integration: POST /api/v1/developer/payments returns
// data.checkout_url and data.reference, amounts in pesewas, Bearer
// key, idempotency header.

type recorded struct {
	url         string
	auth        string
	idempotency string
	body        map[string]any
}

var (
	handler func(w http.ResponseWriter, r *http.Request)
	last    recorded
)

func stubServer(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		last = recorded{
			url:         r.URL.Path,
			auth:        r.Header.Get("authorization"),
			idempotency: r.Header.Get("idempotency-key"),
			body:        body,
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func useSeev(t *testing.T, base string, status int) {
	t.Helper()
	t.Setenv("SEEV_SECRET_KEY", "sk_test")
	t.Setenv("SEEV_API_BASE", base)
	t.Setenv("SEEV_REDIRECT_URL", "")
	handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(status)
		if status == 200 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"checkout_url": "https://checkout.seevplus.com/pay/abc", "reference": "SEEVREF1"},
			})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "boom"})
		}
	}
}

func TestSettlementSeevTakesOnePointFivePercent(t *testing.T) {
	// The confirmed money model, on the real live-check sale figures.
	if got := SettlementFor(730); got != (Settlement{Fee: 10.95, Net: 719.05}) {
		t.Fatalf("got %+v, want fee 10.95 net 719.05", got)
	}
	if got := SettlementFor(100); got != (Settlement{Fee: 1.5, Net: 98.5}) {
		t.Fatalf("got %+v", got)
	}
	if got := SettlementFor(0); got != (Settlement{Fee: 0, Net: 0}) {
		t.Fatalf("got %+v", got)
	}
}

func TestSettlementFeeAndNetSumBackToPrice(t *testing.T) {
	for _, amount := range []float64{33, 49.99, 120, 450.5, 999.99} {
		s := SettlementFor(amount)
		diff := s.Fee + s.Net - amount
		if diff < 0 {
			diff = -diff
		}
		if diff >= 0.001 {
			t.Fatalf("%v + %v != %v", s.Fee, s.Net, amount)
		}
	}
}

func TestWithoutKeyLinkIsMarkedPlaceholder(t *testing.T) {
	t.Setenv("SEEV_SECRET_KEY", "")
	link := CreatePaymentLink(LinkRequest{Amount: 730, Reference: "HACK-1"})
	if !link.DryRun {
		t.Fatal("placeholder link must be marked dry-run")
	}
	if link.URL == nil || *link.URL != "https://pay.example.test/HACK-1" {
		t.Fatalf("url %v, want the placeholder", link.URL)
	}
}

func TestWithKeyClientPostsPesewasAndReturnsHostedURL(t *testing.T) {
	base := stubServer(t)
	useSeev(t, base, 200)
	link := CreatePaymentLink(LinkRequest{Amount: 730, Reference: "HACK-2"})
	if link.URL == nil || *link.URL != "https://checkout.seevplus.com/pay/abc" {
		t.Fatalf("url %v", link.URL)
	}
	if link.ProviderRef == nil || *link.ProviderRef != "SEEVREF1" {
		t.Fatalf("providerRef %v", link.ProviderRef)
	}
	if link.DryRun {
		t.Fatal("real session must not be dry-run")
	}
	if last.url != "/api/v1/developer/payments" {
		t.Fatalf("url %q", last.url)
	}
	if last.auth != "Bearer sk_test" {
		t.Fatalf("auth %q", last.auth)
	}
	if last.idempotency != "HACK-2" {
		t.Fatalf("idempotency %q", last.idempotency)
	}
	if last.body["amount"] != float64(73000) {
		t.Fatalf("amount %v, want 73000 pesewas", last.body["amount"])
	}
	if last.body["currency"] != "GHS" {
		t.Fatalf("currency %v", last.body["currency"])
	}
	if last.body["type"] != "checkout" {
		t.Fatalf("type %v", last.body["type"])
	}
	meta, _ := last.body["meta"].(map[string]any)
	if meta["order_reference"] != "HACK-2" {
		t.Fatalf("meta %v", last.body["meta"])
	}
}

func TestSeevFailureReturnsNoLinkNeverFake(t *testing.T) {
	base := stubServer(t)
	useSeev(t, base, 500)
	link := CreatePaymentLink(LinkRequest{Amount: 730, Reference: "HACK-3"})
	if link.URL != nil {
		t.Fatalf("url %v, want nil on failure", *link.URL)
	}
	if link.DryRun {
		t.Fatal("failure must not be marked dry-run")
	}
}

func sign(body, ts, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + body))
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookSignatureProvenSchemeVerifiesAndFailsClosed(t *testing.T) {
	secret := "whsec_test"
	bodyBytes, _ := json.Marshal(map[string]any{
		"data": map[string]any{"reference": "SEEV1", "status": "completed", "amount": 730},
	})
	body := string(bodyBytes)
	now := int64(1800000000)
	ts := "1800000000"
	sig := sign(body, ts, secret)
	cases := []struct {
		name                     string
		rawBody, ts, sig, secret string
		now                      int64
		want                     bool
	}{
		{"valid", body, ts, sig, secret, now, true},
		{"tampered body", body + "x", ts, sig, secret, now, false},
		{"wrong secret", body, ts, sig, "wrong-secret", now, false},
		{"stale", body, ts, sig, secret, now + 301, false},
		{"too far ahead", body, ts, sig, secret, now - 61, false},
		{"non-numeric timestamp", body, "abc", sig, secret, now, false},
		{"empty signature", body, ts, "", secret, now, false},
		{"empty body", "", ts, sig, secret, now, false},
	}
	for _, c := range cases {
		if got := VerifyWebhookSignature(c.rawBody, c.ts, c.sig, c.secret, c.now); got != c.want {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestWebhookEventsParseFlatOrNested(t *testing.T) {
	nested := ParseWebhookEvent([]byte(`{"data":{"reference":"SEEV1","status":"completed","amount":"730","meta":{"order_reference":"HACK-9"}}}`))
	if nested.Reference == nil || *nested.Reference != "SEEV1" ||
		nested.OrderReference == nil || *nested.OrderReference != "HACK-9" ||
		nested.Status == nil || *nested.Status != "completed" ||
		nested.AmountMajor == nil || *nested.AmountMajor != 730 {
		t.Fatalf("nested: %+v", nested)
	}
	flat := ParseWebhookEvent([]byte(`{"reference":"SEEV2","status":"failed"}`))
	if flat.Reference == nil || *flat.Reference != "SEEV2" {
		t.Fatalf("flat reference: %+v", flat)
	}
	if flat.Status == nil || *flat.Status != "failed" {
		t.Fatalf("flat status: %+v", flat)
	}
	if flat.AmountMajor != nil {
		t.Fatalf("flat amount: %+v, want nil", flat)
	}
	if got := ParseWebhookEvent(nil); got.Reference != nil {
		t.Fatalf("empty body: %+v", got)
	}
}

func TestPaidAndFailedWords(t *testing.T) {
	if !IsPaidStatus("completed") || !IsPaidStatus("SUCCESS") || IsPaidStatus("pending") {
		t.Fatal("paid words mismatch")
	}
	if !IsFailedStatus("failed") || !IsFailedStatus("expired") || IsFailedStatus("completed") {
		t.Fatal("failed words mismatch")
	}
}

func TestOrderBookRecordsFindsAndSettles(t *testing.T) {
	book := orders.New(filepath.Join(t.TempDir(), "orders.json"))
	ref := "SEEVREF1"
	book.RecordOrder("HACK-7", orders.Order{
		From:        "233550000001",
		ProductName: "Handmade Beaded Bag",
		Price:       730,
		ProviderRef: &ref,
	})
	if got := book.GetOrder("HACK-7"); got == nil || got.Status != "pending" {
		t.Fatalf("got %+v, want pending", got)
	}
	if got := book.FindByProviderRef("SEEVREF1"); got == nil || got.Reference != "HACK-7" {
		t.Fatalf("find by provider ref: %+v", got)
	}
	if got := book.FindByProviderRef("nope"); got != nil {
		t.Fatalf("unknown provider ref: %+v, want nil", got)
	}
	book.SetStatus("HACK-7", "paid")
	if got := book.GetOrder("HACK-7"); got == nil || got.Status != "paid" {
		t.Fatalf("got %+v, want paid", got)
	}
	if got := book.GetOrder("missing"); got != nil {
		t.Fatalf("missing order: %+v, want nil", got)
	}
}

func TestVerifyPaymentReadsPesewas(t *testing.T) {
	// The real read-back shape, captured live: a session
	// created for GH₵1 (100 pesewas) reads back amount 100,
	// and one created for GH₵2.50 reads back 250. AmountMajor
	// must come out in cedis: 1 and 2.5.
	for _, tc := range []struct {
		amount float64
		want   float64
	}{{100, 1}, {250, 2.5}, {87500, 875}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("content-type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"status": "completed", "amount": tc.amount},
			})
		}))
		t.Setenv("SEEV_API_BASE", srv.URL)
		v := VerifyPayment("PAY-TEST")
		srv.Close()
		if v == nil || v.AmountMajor == nil || *v.AmountMajor != tc.want {
			t.Fatalf("read-back %v pesewas must read as GH₵%v, got %+v", tc.amount, tc.want, v)
		}
	}
}
