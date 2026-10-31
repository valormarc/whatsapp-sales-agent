package checkout

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Regression for the audit lead that a success response
// carrying a checkout URL but no session reference produced
// a payable order with nothing to read back: such a link is
// refused at the client, so no order can ever be recorded
// pointing at a session that does not exist.
func TestCreatePaymentLinkRequiresASessionReference(t *testing.T) {
	base := stubServer(t)
	t.Setenv("SEEV_SECRET_KEY", "sk_test")
	t.Setenv("SEEV_API_BASE", base)
	t.Setenv("SEEV_REDIRECT_URL", "")
	handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"checkout_url": "https://checkout.seevplus.com/pay/abc"},
		})
	}
	link := CreatePaymentLink(LinkRequest{Reference: "TEST-REF-1", Amount: 100, RecipientName: "Ama"})
	if link.URL != nil {
		t.Fatalf("a success without a session reference must not yield a link, got %v", *link.URL)
	}
	if link.ProviderRef != nil {
		t.Fatal("no provider reference may be recorded either")
	}
}

// A read-back that says paid but states no amount proves
// nothing about the amount, and never matches.
func TestPaidAmountMatchesRequiresAStatedAmount(t *testing.T) {
	if PaidAmountMatches(&Verification{Status: "completed"}, 100) {
		t.Fatal("a paid read-back with no amount must not match")
	}
	amt := 100.0
	if !PaidAmountMatches(&Verification{Status: "completed", AmountMajor: &amt}, 100) {
		t.Fatal("a paid read-back at the charged amount must match")
	}
	other := 90.0
	if PaidAmountMatches(&Verification{Status: "completed", AmountMajor: &other}, 100) {
		t.Fatal("a paid read-back at a different amount must not match")
	}
	if PaidAmountMatches(&Verification{Status: "pending", AmountMajor: &amt}, 100) {
		t.Fatal("an unpaid read-back must not match")
	}
}
