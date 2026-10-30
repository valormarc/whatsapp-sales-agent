package seevwebhook

// The payment webhook is the endpoint that marks orders
// paid, so its rules are the money rules: no secret
// configured means every event is rejected (never "no
// signature needed"), a deployment with checkout disabled
// settles nothing, and when Seev cannot be reached to
// confirm its own event the order is held and the merchant
// told, not settled on the event's word.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

const (
	whSecret = "test-webhook-secret"
	whOwner  = "233550008099"
	whBuyer  = "233550008001"
)

func sign(body, secret string) (string, string) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%s.%s", ts, body)))
	return ts, "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func paidEvent(ref, orderRef string, amount float64) string {
	return fmt.Sprintf(`{"reference":%q,"status":"completed","amount":%v,"meta":{"order_reference":%q}}`, ref, amount, orderRef)
}

func seedOrder(ref string, providerRef *string) {
	orders.Default.RecordOrder(ref, orders.Order{
		From: whBuyer, ProductName: "Test Bag", Price: 100,
		MerchantID: "testshop", ProviderRef: providerRef,
	})
}

func merchantNotes(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("data/outbox.jsonl")
	if err != nil {
		return ""
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"to":"`+whOwner+`"`) {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func TestWebhookFailsClosedWithoutASecret(t *testing.T) {
	t.Setenv("SEEV_WEBHOOK_SECRET", "")
	t.Setenv("WHATSAPP_TOKEN", "")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	seedOrder("TEST-A001", nil)
	body := paidEvent("seev-a1", "TEST-A001", 100)
	ts, sig := sign(body, whSecret)
	if code := Handle(body, ts, sig); code != 401 {
		t.Fatalf("no secret configured must reject every event, got %d", code)
	}
	if o := orders.Default.GetOrder("TEST-A001"); o == nil || o.Status == "paid" {
		t.Fatalf("nothing may settle without a secret, got %+v", o)
	}
}

func TestWebhookRejectsABadSignature(t *testing.T) {
	t.Setenv("SEEV_WEBHOOK_SECRET", whSecret)
	t.Setenv("WHATSAPP_TOKEN", "")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	seedOrder("TEST-A002", nil)
	body := paidEvent("seev-a2", "TEST-A002", 100)
	ts, _ := sign(body, whSecret)
	if code := Handle(body, ts, "v1=deadbeef"); code != 401 {
		t.Fatalf("a bad signature must be rejected, got %d", code)
	}
}

func TestPaidEventSettlesNothingWhileCheckoutIsDisabled(t *testing.T) {
	t.Setenv("SEEV_WEBHOOK_SECRET", whSecret)
	t.Setenv("SEEV_SECRET_KEY", "")
	t.Setenv("WHATSAPP_TOKEN", "")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	seedOrder("TEST-A003", nil)
	body := paidEvent("seev-a3", "TEST-A003", 100)
	ts, sig := sign(body, whSecret)
	if code := Handle(body, ts, sig); code != 200 {
		t.Fatalf("a verified event is acknowledged, got %d", code)
	}
	if o := orders.Default.GetOrder("TEST-A003"); o == nil || o.Status == "paid" {
		t.Fatalf("checkout disabled must settle nothing, got %+v", o)
	}
	if notes := merchantNotes(t); !strings.Contains(notes, "not marked the order paid") {
		t.Fatalf("the merchant must be told the order was held, notes: %q", notes)
	}
}

func TestPaidEventIsHeldWhenTheReadBackCannotBeReached(t *testing.T) {
	t.Setenv("SEEV_WEBHOOK_SECRET", whSecret)
	t.Setenv("SEEV_SECRET_KEY", "test-seev-key")
	t.Setenv("WHATSAPP_TOKEN", "")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	session := "seev-session-held"
	seedOrder("TEST-A004", &session)
	restore := verifyPaymentFn
	verifyPaymentFn = func(string) *checkout.Verification { return nil }
	defer func() { verifyPaymentFn = restore }()

	body := paidEvent("seev-a4", "TEST-A004", 100)
	ts, sig := sign(body, whSecret)
	if code := Handle(body, ts, sig); code != 200 {
		t.Fatalf("a verified event is acknowledged, got %d", code)
	}
	if o := orders.Default.GetOrder("TEST-A004"); o == nil || o.Status == "paid" {
		t.Fatalf("an unreachable read-back must hold the order, got %+v", o)
	}
	if notes := merchantNotes(t); !strings.Contains(notes, "could not confirm it with Seev") {
		t.Fatalf("the merchant must be told why it was held, notes: %q", notes)
	}
}

func TestPaidEventSettlesADryOrderOnItsVerifiedWord(t *testing.T) {
	t.Setenv("SEEV_WEBHOOK_SECRET", whSecret)
	t.Setenv("SEEV_SECRET_KEY", "test-seev-key")
	t.Setenv("WHATSAPP_TOKEN", "")
	t.Setenv("MERCHANT_WHATSAPP", whOwner)
	seedOrder("TEST-A005", nil) // no provider session: the dry-run road
	body := paidEvent("seev-a5", "TEST-A005", 100)
	ts, sig := sign(body, whSecret)
	if code := Handle(body, ts, sig); code != 200 {
		t.Fatalf("a verified event is acknowledged, got %d", code)
	}
	if o := orders.Default.GetOrder("TEST-A005"); o == nil || o.Status != "paid" {
		t.Fatalf("a dry order settles on the verified event, got %+v", o)
	}
}
