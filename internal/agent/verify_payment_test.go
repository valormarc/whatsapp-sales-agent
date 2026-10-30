package agent

// A buyer who reports a link payment is verified by the
// agent, not by the merchant. The merchant has no window into
// Seev, so when a buyer says "I have paid" about a Seev link,
// the agent reads the session back from Seev itself and
// answers both sides from what it says: paid settles the
// order and alerts the merchant exactly as the webhook would,
// not yet paid is said plainly to the buyer alone, and failed
// is said plainly with the link offered again. Only a payment
// she says went to the merchant's own number becomes a claim
// for the merchant to confirm. A fake Seev stands in here;
// the live proof checks the same door against the real one.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// signSeevWebhook signs a webhook body the way Seev does,
// with the secret the test put in SEEV_WEBHOOK_SECRET, so a
// test can post an event the handler will verify. The
// handler fails closed: an unsigned event is a 401 now,
// never a settlement.
func signSeevWebhook(t *testing.T, body string) (string, string) {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(os.Getenv("SEEV_WEBHOOK_SECRET")))
	mac.Write([]byte(fmt.Sprintf("%s.%s", ts, body)))
	return ts, "v1=" + hex.EncodeToString(mac.Sum(nil))
}

// fakeSeevReadback serves one read-back shape for any session
// asked of it, and points the checkout package at it. The
// amount is passed in pesewas, the unit the real read-back
// uses (the seeded order charges GH₵875, so its matching
// read-back amount is 87500).
func fakeSeevReadback(t *testing.T, status string, amount float64) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		fmt.Fprintf(w, `{"data":{"status":%q,"amount":%v}}`, status, amount)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SEEV_API_BASE", srv.URL)
}

func TestPaidReportOnALinkIsVerifiedAndSettledByTheAgent(t *testing.T) {
	merchant := "233550001901"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550001902"
	seedOpenOrder(t, "TEST-4001", buyer, 1)
	fakeSeevReadback(t, "completed", 87500)

	actOnBuyerUnderstanding(buyer, &Session{Stage: "agreed"}, catalogue.GetMerchant(MerchantID), MerchantID, &understood{Intent: intentPaymentClaim})

	o := orders.Default.GetOrder("TEST-4001")
	if o == nil || o.Status != "paid" || o.PaymentPath != "seev" {
		t.Fatalf("a verified link payment must settle as a Seev payment, got %+v", o)
	}
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Payment confirmed") || !strings.Contains(got, "Amount: GH₵875") || !strings.Contains(got, "Thank you for buying from") {
		t.Fatalf("buyer confirmation = %q", got)
	}
	notes := strings.Join(c.replies(t, merchant), "\n")
	if !strings.Contains(notes, "Payment: confirmed") || !strings.Contains(notes, "TEST-4001") {
		t.Fatalf("the merchant must be alerted by the agent, got %q", notes)
	}
	if strings.Contains(notes, "Reply yes if you have received it") {
		t.Fatalf("the merchant must not be asked to verify a Seev payment: %q", notes)
	}
}

func TestPaidReportOnALinkNotYetPaid(t *testing.T) {
	merchant := "233550001911"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550001912"
	seedOpenOrder(t, "TEST-4002", buyer, 1)
	fakeSeevReadback(t, "pending", 87500)

	actOnBuyerUnderstanding(buyer, &Session{Stage: "agreed"}, catalogue.GetMerchant(MerchantID), MerchantID, &understood{Intent: intentPaymentClaim})

	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "has not come through yet") {
		t.Fatalf("buyer answer = %q", got)
	}
	if got := c.replies(t, merchant); len(got) != 0 {
		t.Fatalf("the merchant hears nothing about an unpaid link, got %v", got)
	}
	if o := orders.Default.GetOrder("TEST-4002"); o.Status != "pending" {
		t.Fatalf("the order must stay open, got %+v", o)
	}
}

func TestPaidReportOnALinkThatFailed(t *testing.T) {
	merchant := "233550001921"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550001922"
	seedOpenOrder(t, "TEST-4003", buyer, 1)
	fakeSeevReadback(t, "failed", 87500)

	actOnBuyerUnderstanding(buyer, &Session{Stage: "agreed"}, catalogue.GetMerchant(MerchantID), MerchantID, &understood{Intent: intentPaymentClaim})

	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "did not go through") || !strings.Contains(got, "pay.seevplus.com/PAY-UNDER-1") {
		t.Fatalf("buyer answer = %q", got)
	}
	if got := c.replies(t, merchant); len(got) != 0 {
		t.Fatalf("the merchant hears nothing about a failed link, got %v", got)
	}
}

func TestPaidReportByTheTextDoorVerifiesToo(t *testing.T) {
	merchant := "233550001931"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}
	buyer := "233550001932"
	seedOpenOrder(t, "TEST-4004", buyer, 1)
	fakeSeevReadback(t, "completed", 87500)

	c.say(t, buyer, "SHOP-TEST")
	c.replies(t, buyer)
	c.say(t, buyer, "I have paid")
	if o := orders.Default.GetOrder("TEST-4004"); o == nil || o.Status != "paid" {
		t.Fatalf("the plain words I have paid must be verified and settled, got %+v", o)
	}
}
