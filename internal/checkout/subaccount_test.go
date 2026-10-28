package checkout

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestNetworkCodeReadsThePrefix(t *testing.T) {
	cases := map[string]string{
		"233550000111": "mtn",     // 055
		"233240000111": "mtn",     // 024
		"233200000111": "telecel", // 020
		"233500000111": "telecel", // 050
		"233260000111": "at",      // 026
		"233270000111": "at",      // 027
	}
	for phone, want := range cases {
		if got := NetworkCode(phone); got != want {
			t.Fatalf("NetworkCode(%s) = %s, want %s", phone, got, want)
		}
	}
	if got := LocalNumber("233550000111"); got != "0550000111" {
		t.Fatalf("LocalNumber = %s, want 0550000111", got)
	}
	if got := LocalNumber("0550000111"); got != "0550000111" {
		t.Fatalf("LocalNumber kept local form = %s", got)
	}
}

func TestResolveAccountReadsTheNameOnTheNumber(t *testing.T) {
	base := stubServer(t)
	t.Setenv("SEEV_SECRET_KEY", "sk_test")
	t.Setenv("SEEV_API_BASE", base)
	handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"account_name": "AKOSUA MENSAH"},
		})
	}
	name, ok := ResolveAccount("233550000111")
	if !ok || name != "AKOSUA MENSAH" {
		t.Fatalf("ResolveAccount = %q, %v", name, ok)
	}
	if last.url != "/api/v1/developer/resolve-account" {
		t.Fatalf("resolve path = %s", last.url)
	}
	if last.body["account_number"] != "0550000111" || last.body["settlement_bank"] != "mtn" {
		t.Fatalf("resolve body = %v", last.body)
	}
}

func TestCreateSubaccountSendsTheShopsOwnDetails(t *testing.T) {
	base := stubServer(t)
	t.Setenv("SEEV_SECRET_KEY", "sk_test")
	t.Setenv("SEEV_API_BASE", base)
	handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"subaccount_code": "SUB_test123", "verification_status": "verified"},
		})
	}
	code, ok := CreateSubaccount("Suba Shop", "233550000111", "AKOSUA MENSAH")
	if !ok || code != "SUB_test123" {
		t.Fatalf("CreateSubaccount = %q, %v", code, ok)
	}
	if last.url != "/api/v1/developer/subaccounts" {
		t.Fatalf("create path = %s", last.url)
	}
	if last.body["business_name"] != "Suba Shop" || last.body["account_name"] != "AKOSUA MENSAH" ||
		last.body["account_number"] != "0550000111" || last.body["settlement_bank"] != "mtn" {
		t.Fatalf("create body = %v", last.body)
	}
	if last.body["default_share_percent"] != float64(SellerSharePercent) {
		t.Fatalf("share = %v, want %d", last.body["default_share_percent"], SellerSharePercent)
	}
}

func TestCreateSubaccountFailureIsHonest(t *testing.T) {
	base := stubServer(t)
	t.Setenv("SEEV_SECRET_KEY", "sk_test")
	t.Setenv("SEEV_API_BASE", base)
	handler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "bad number"})
	}
	if code, ok := CreateSubaccount("Suba Shop", "233550000111", "AKOSUA MENSAH"); ok || code != "" {
		t.Fatalf("a refused creation must report failure, got %q, %v", code, ok)
	}
}

func TestPaymentLinkCarriesTheShopsSubaccount(t *testing.T) {
	base := stubServer(t)
	useSeev(t, base, 200)
	link := CreatePaymentLink(LinkRequest{Amount: 85, Reference: "SUBA-0001", Subaccount: "SUB_test123"})
	if link.URL == nil {
		t.Fatal("expected a link")
	}
	if last.body["subaccount"] != "SUB_test123" {
		t.Fatalf("payment body subaccount = %v", last.body["subaccount"])
	}
}
