package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A webhook delivery carrying only statuses — Meta's
// reports about messages this number sent — must land
// in the agent's status book: before the payload
// declared the field, exactly this body was answered
// 200 and dropped unseen. The body below is Meta's
// documented shape, with fictional numbers throughout.
func TestWebhookStatusesAreRecorded(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKATHON_AGENT_DATA_DIR", dir)
	// A credential-less sandbox: the unsigned post is
	// accepted by construction (see signatureOK).
	t.Setenv("WHATSAPP_APP_SECRET", "")
	t.Setenv("WHATSAPP_DRY_RUN", "1")
	t.Setenv("WHATSAPP_TOKEN", "")
	t.Setenv("WHATSAPP_PHONE_NUMBER_ID", "")
	t.Setenv("SEEV_SECRET_KEY", "")
	t.Setenv("SEEV_API_BASE", "")
	t.Setenv("META_MODEL_API_KEY", "")
	t.Setenv("META_MODEL_API_BASE", "")
	t.Setenv("STALL_SANDBOX_ALLOW_MODEL", "")

	body := `{"object":"whatsapp_business_account","entry":[{"id":"100000000000001","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"display_phone_number":"233200000000","phone_number_id":"100000000000002"},"statuses":[{"id":"wamid.webhook-delivered","status":"delivered","timestamp":"1700001000","recipient_id":"233240000951"},{"id":"wamid.webhook-delivered","status":"read","timestamp":"1700001060","recipient_id":"233240000951"},{"id":"wamid.webhook-failed","status":"failed","timestamp":"1700001030","recipient_id":"233240000952","errors":[{"code":131047,"title":"Re-engagement message","message":"More than 24 hours have passed since the recipient last replied."}]}]}}]}]}`

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	res := httptest.NewRecorder()
	Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("webhook answered %d, want 200", res.Code)
	}

	book, err := os.ReadFile(filepath.Join(dir, "statuses.json"))
	if err != nil {
		t.Fatalf("no status book was written: %v", err)
	}
	text := string(book)
	for _, want := range []string{`"233240000951"`, `"lastReadAt": 1700001060`, `"wamid.webhook-failed"`, `"errorCode": 131047`} {
		if !strings.Contains(text, want) {
			t.Fatalf("status book does not contain %s:\n%s", want, text)
		}
	}
	journal, err := os.ReadFile(filepath.Join(dir, "statuses.jsonl"))
	if err != nil {
		t.Fatalf("no status journal was written: %v", err)
	}
	if n := strings.Count(string(journal), "\n"); n != 3 {
		t.Fatalf("journal lines = %d, want 3 (delivered, read, failed)", n)
	}
}
