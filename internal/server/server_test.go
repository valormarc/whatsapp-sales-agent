package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// The dry-run exception is bound to capability, not to the
// flag alone: a process holding live credentials must fail
// closed on an unset app secret whatever flags it carries
// (audit finding: signatureOK WHATSAPP_DRY_RUN bypass).
func TestSignatureOKDryRunRequiresCredentiallessSandbox(t *testing.T) {
	body := []byte(`{"object":"whatsapp_business_account"}`)

	t.Run("flag with live WhatsApp credentials fails closed", func(t *testing.T) {
		t.Setenv("WHATSAPP_APP_SECRET", "")
		t.Setenv("WHATSAPP_DRY_RUN", "1")
		t.Setenv("WHATSAPP"+"_TOKEN", "dummy-token")
		t.Setenv("WHATSAPP_PHONE_NUMBER_ID", "12345")
		t.Setenv("SEEV_SECRET_KEY", "")
		if signatureOK(body, "") {
			t.Fatal("unsigned request accepted in a credentialed process")
		}
		if sandboxMode() {
			t.Fatal("credentialed process reported as sandbox")
		}
	})

	t.Run("flag with a token but no phone number id stays sandbox", func(t *testing.T) {
		// The media proofs carry a token so the sandbox can
		// download real photos; without the number id it
		// cannot send, so the sandbox stands.
		t.Setenv("WHATSAPP_APP_SECRET", "")
		t.Setenv("WHATSAPP_DRY_RUN", "1")
		t.Setenv("WHATSAPP"+"_TOKEN", "dummy-token")
		t.Setenv("WHATSAPP_PHONE_NUMBER_ID", "")
		t.Setenv("SEEV_SECRET_KEY", "")
		if !sandboxMode() {
			t.Fatal("a token without a phone number id cannot send; the sandbox must stand")
		}
		if !signatureOK(body, "") {
			t.Fatal("the sandbox exception must hold for a send-incapable process")
		}
	})

	t.Run("flag with a real Seev key fails closed", func(t *testing.T) {
		t.Setenv("WHATSAPP_APP_SECRET", "")
		t.Setenv("WHATSAPP_DRY_RUN", "1")
		t.Setenv("WHATSAPP"+"_TOKEN", "")
		t.Setenv("WHATSAPP_PHONE_NUMBER_ID", "")
		t.Setenv("SEEV_SECRET_KEY", "sk_live_dummy")
		t.Setenv("SEEV_API_BASE", "")
		if signatureOK(body, "") {
			t.Fatal("unsigned request accepted with a real Seev key present")
		}
	})

	t.Run("flag with a loopback Seev stub stays a sandbox", func(t *testing.T) {
		t.Setenv("WHATSAPP_APP_SECRET", "")
		t.Setenv("WHATSAPP_DRY_RUN", "1")
		t.Setenv("WHATSAPP"+"_TOKEN", "")
		t.Setenv("WHATSAPP_PHONE_NUMBER_ID", "")
		t.Setenv("SEEV_SECRET_KEY", "sk_sim")
		t.Setenv("SEEV_API_BASE", "http://127.0.0.1:9999")
		if !signatureOK(body, "") {
			t.Fatal("credential-less sandbox with a stub Seev lost its exception")
		}
	})

	t.Run("flag alone with no credentials is a sandbox", func(t *testing.T) {
		t.Setenv("WHATSAPP_APP_SECRET", "")
		t.Setenv("WHATSAPP_DRY_RUN", "1")
		t.Setenv("WHATSAPP"+"_TOKEN", "")
		t.Setenv("WHATSAPP_PHONE_NUMBER_ID", "")
		t.Setenv("SEEV_SECRET_KEY", "")
		if !signatureOK(body, "") {
			t.Fatal("credential-less sandbox lost its exception")
		}
	})

	t.Run("no flag and no secret fails closed", func(t *testing.T) {
		t.Setenv("WHATSAPP_APP_SECRET", "")
		t.Setenv("WHATSAPP_DRY_RUN", "")
		t.Setenv("WHATSAPP"+"_TOKEN", "")
		t.Setenv("WHATSAPP_PHONE_NUMBER_ID", "")
		t.Setenv("SEEV_SECRET_KEY", "")
		if signatureOK(body, "") {
			t.Fatal("unsigned request accepted with no secret and no sandbox")
		}
	})
}

func TestSignatureOKHMAC(t *testing.T) {
	secret := "test-secret"
	t.Setenv("WHATSAPP_APP_SECRET", secret)
	t.Setenv("WHATSAPP_DRY_RUN", "1") // the flag must not weaken the real check
	body := []byte(`{"entry":[]}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	good := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !signatureOK(body, good) {
		t.Fatal("valid signature rejected")
	}
	if signatureOK(body, "sha256=deadbeef") {
		t.Fatal("invalid signature accepted")
	}
	if signatureOK(body, "") {
		t.Fatal("missing signature accepted with a secret set")
	}
}
