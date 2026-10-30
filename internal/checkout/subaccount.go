package checkout

// One payment account per shop: a Seev subaccount in the shop's
// own name, settling to the seller's own MoMo. The platform holds
// one Seev relationship; each shop's link money is split at
// payment time by subaccount, so no one has to divide a shared pot
// by hand afterwards. The seller's share is fixed at creation:
// 99 percent to her, the figure the platform's own test store ran
// on, because a subaccount may not keep the whole amount.

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

// SellerSharePercent is what a shop keeps of every link payment.
const SellerSharePercent = 99

// NetworkCode reads the MoMo network out of a Ghana number's
// prefix. It is a routing hint for Seev's settlement bank field;
// ported numbers can mislead it, and a wrong guess surfaces as a
// failed creation the seller can retry, never as lost money.
func NetworkCode(phone string) string {
	local := LocalNumber(phone)
	if len(local) < 3 {
		return "mtn"
	}
	switch local[:3] {
	case "024", "025", "053", "054", "055", "059":
		return "mtn"
	case "020", "050":
		return "telecel"
	case "026", "027", "056", "057":
		return "at"
	}
	return "mtn"
}

// LocalNumber puts a Ghana number in the 0-prefixed local form
// Seev's account fields use: the WhatsApp form 23355... becomes
// 055....
func LocalNumber(phone string) string {
	d := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	if strings.HasPrefix(d, "233") && len(d) == 12 {
		return "0" + d[3:]
	}
	return d
}

type seevCall struct {
	path string
	body any
}

// post issues one authenticated call to the Seev developer API and
// hands back the parsed envelope. Failures are logged with the
// provider's own answer, exactly as checkout creation is.
func post(call seevCall) (map[string]any, int, bool) {
	data, err := json.Marshal(call.body)
	if err != nil {
		return nil, 0, false
	}
	req, err := http.NewRequest(http.MethodPost, apiBase()+call.path, bytes.NewReader(data))
	if err != nil {
		return nil, 0, false
	}
	req.Header.Set("authorization", "Bearer "+os.Getenv("SEEV_SECRET_KEY"))
	req.Header.Set("content-type", "application/json")
	res, err := seevHTTPClient.Do(req)
	if err != nil {
		log.Printf("seev %s: request failed: %v", call.path, err)
		return nil, 0, false
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, res.StatusCode, false
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		log.Printf("seev %s: status %d: %.300s", call.path, res.StatusCode, string(raw))
		return nil, res.StatusCode, false
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, res.StatusCode, false
	}
	if ok, _ := parsed["success"].(bool); !ok {
		log.Printf("seev %s: unsuccessful envelope: %.300s", call.path, string(raw))
		return nil, res.StatusCode, false
	}
	return parsed, res.StatusCode, true
}

func dataField(envelope map[string]any, key string) string {
	data, _ := envelope["data"].(map[string]any)
	if data == nil {
		return ""
	}
	s, _ := data[key].(string)
	return s
}

// ResolveAccount asks Seev for the name on a MoMo account, so the
// seller confirms the name instead of typing it from memory.
func ResolveAccount(phone string) (string, bool) {
	envelope, _, ok := post(seevCall{
		path: "/api/v1/developer/resolve-account",
		body: map[string]any{
			"settlement_bank": NetworkCode(phone),
			"account_number":  LocalNumber(phone),
		},
	})
	if !ok {
		return "", false
	}
	name := dataField(envelope, "account_name")
	return name, name != ""
}

// CreateSubaccount opens the shop's own Seev subaccount and
// returns its code. The second value is false on any failure; the
// shop itself must still open either way, so callers treat this as
// a best-effort step with a retry, never a gate.
func CreateSubaccount(businessName, phone, holderName string) (string, bool) {
	envelope, _, ok := post(seevCall{
		path: "/api/v1/developer/subaccounts",
		body: map[string]any{
			"business_name":         businessName,
			"settlement_bank":       NetworkCode(phone),
			"account_number":        LocalNumber(phone),
			"account_name":          holderName,
			"default_share_percent": SellerSharePercent,
		},
	})
	if !ok {
		return "", false
	}
	code := dataField(envelope, "subaccount_code")
	if code == "" {
		code = dataField(envelope, "code")
	}
	return code, code != ""
}
