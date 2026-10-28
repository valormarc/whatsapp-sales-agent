package agent

// The per-shop payment account: opening a shop opens the shop's
// own Seev subaccount in the same sitting, and her buyers' links
// are created against it, so the money settles to her own MoMo.
// A fake Seev stands in for the API; the live sandbox proof is a
// separate layer, run by hand.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

type fakeSeev struct {
	failCreate bool
	payments   []map[string]any
	creates    []map[string]any
}

func startFakeSeev(t *testing.T, fake *fakeSeev) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/api/v1/developer/resolve-account":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"account_name": "AKOSUA MENSAH"}})
		case "/api/v1/developer/subaccounts":
			fake.creates = append(fake.creates, body)
			if fake.failCreate {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "refused"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"subaccount_code": "SUB_test123"}})
		case "/api/v1/developer/payments":
			fake.payments = append(fake.payments, body)
			meta, _ := body["meta"].(map[string]any)
			ref, _ := meta["order_reference"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"checkout_url": "https://pay.seevplus.com/PAY-test",
				"reference":    "PAY-" + ref,
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("SEEV_SECRET_KEY", "sk_test_fake")
	t.Setenv("SEEV_API_BASE", server.URL)
}

// stockTheShop walks the onboarding that signup leaves open and
// saves one product.
func stockTheShop(t *testing.T, c *chat, owner string) {
	t.Helper()
	c.say(t, owner, "Test Plantain")
	c.say(t, owner, "A bunch of plantain.")
	c.say(t, owner, "50")
	c.say(t, owner, "40")
	c.say(t, owner, "whole")
	c.say(t, owner, "yes")
}

func TestSignupOpensTheShopsOwnPaymentAccount(t *testing.T) {
	dir := setupChat(t, "233550009999", `{"merchants":{}}`)
	c := &chat{dir: dir}
	fake := &fakeSeev{}
	startFakeSeev(t, fake)
	owner := "233550031001"

	c.say(t, owner, "open a shop")
	c.say(t, owner, "Suba Shop")
	got := c.replies(t, owner)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "AKOSUA MENSAH") || !strings.Contains(joined, "Say yes") {
		t.Fatalf("signup should hold the resolved MoMo name up for confirmation, got %v", got)
	}
	c.say(t, owner, "yes")
	got = c.replies(t, owner)
	joined = strings.Join(got, "\n")
	if !strings.Contains(joined, "payment account is set up") || !strings.Contains(joined, "99%") {
		t.Fatalf("the payout line should confirm the account and the share, got %v", got)
	}
	shop := catalogue.MerchantByOwner(owner)
	if shop == nil {
		t.Fatal("shop should exist")
	}
	if shop.SubaccountCode != "SUB_test123" || shop.PayoutHolderName != "AKOSUA MENSAH" {
		t.Fatalf("merchant should carry its subaccount: %+v", shop)
	}
	if len(fake.creates) != 1 || fake.creates[0]["business_name"] != "Suba Shop" {
		t.Fatalf("Seev should have been asked once, for the shop: %v", fake.creates)
	}

	// A sale in that shop pays through its subaccount. The new
	// shop has no delivery terms, so the buyer is asked where she
	// is based, then the package details, then the sale completes.
	stockTheShop(t, c, owner)
	buyer := "233550031002"
	c.say(t, buyer, "SHOP-SUBA")
	c.say(t, buyer, "Test Plantain")
	c.say(t, buyer, "50")
	c.say(t, buyer, "Accra")
	c.finishDetails(t, buyer)
	if len(fake.payments) != 1 {
		t.Fatalf("one payment should have been created, got %d", len(fake.payments))
	}
	if fake.payments[0]["subaccount"] != "SUB_test123" {
		t.Fatalf("the buyer's link must be created against the shop's subaccount, body: %v", fake.payments[0])
	}
}

func TestFailedPaymentSetupNeverBlocksTheShopAndRetries(t *testing.T) {
	dir := setupChat(t, "233550009999", `{"merchants":{}}`)
	c := &chat{dir: dir}
	fake := &fakeSeev{failCreate: true}
	startFakeSeev(t, fake)
	owner := "233550031003"

	c.say(t, owner, "open a shop")
	c.say(t, owner, "Retry Shop")
	c.say(t, owner, "yes")
	got := c.replies(t, owner)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "could not set up your payment account") {
		t.Fatalf("a failed creation should be named plainly, got %v", got)
	}
	shop := catalogue.MerchantByOwner(owner)
	if shop == nil {
		t.Fatal("the shop must still open when Seev refuses")
	}
	if shop.SubaccountCode != "" || shop.PayoutHolderName != "AKOSUA MENSAH" {
		t.Fatalf("the holder name is kept for the retry, the code stays empty: %+v", shop)
	}

	// She is mid-onboarding now; step out of it first, then retry.
	c.say(t, owner, "cancel")
	fake.failCreate = false
	c.say(t, owner, "set up my payment")
	got = c.replies(t, owner)
	joined = strings.Join(got, "\n")
	if !strings.Contains(joined, "It is done") {
		t.Fatalf("the retry should complete the setup, got %v", got)
	}
	if shop = catalogue.MerchantByOwner(owner); shop.SubaccountCode != "SUB_test123" {
		t.Fatalf("the retry should land the code: %+v", shop)
	}
}
