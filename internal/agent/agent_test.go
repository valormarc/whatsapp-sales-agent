package agent

import (
	"os"
	"regexp"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/persona"
)

// One door, two modes: the merchant's own number must be recognised
// however WhatsApp formats it, and a customer must never land in
// merchant mode.

func TestMerchantNumberRecognisedAcrossFormats(t *testing.T) {
	t.Setenv("MERCHANT_WHATSAPP", "233550000999")
	if !IsMerchant("233550000999") {
		t.Fatal("exact number must be the merchant")
	}
	if !IsMerchant("+233550000999") {
		t.Fatal("plus-prefixed number must be the merchant")
	}
	if !IsMerchant("+233 55 000 0999") {
		t.Fatal("spaced number must be the merchant")
	}
}

func TestCustomerNumberIsNeverTheMerchant(t *testing.T) {
	t.Setenv("MERCHANT_WHATSAPP", "233550000999")
	if IsMerchant("233550000001") {
		t.Fatal("customer number must not be the merchant")
	}
	if IsMerchant("") {
		t.Fatal("empty sender must not be the merchant")
	}
}

func TestNoMerchantConfiguredNobodyIsMerchant(t *testing.T) {
	t.Setenv("MERCHANT_WHATSAPP", "233550000999")
	os.Unsetenv("MERCHANT_WHATSAPP")
	if IsMerchant("233550000999") {
		t.Fatal("with no merchant number configured, nobody is the merchant")
	}
}

func TestMerchantPromptCarriesDayFiguresAndHonestyLimit(t *testing.T) {
	prompt := persona.BuildMerchantPrompt(
		catalogue.Merchant{Name: "Efua's Store"},
		daybook.State{Sales: 2, Revenue: 1150, GivenAway: 150, Allowance: 300, WalkAways: 1},
	)
	for _, pattern := range []string{
		`Efua's Store`,
		`merchant herself`,
		`2 sale\(s\), GH₵1,150 in, GH₵150 given away`,
		`GH₵150 of the GH₵300 giveaway allowance left, 1 walk-away`,
		// The limit is stated in the prompt itself: side sales are invisible.
		`outside this chat are invisible`,
	} {
		if !regexp.MustCompile(pattern).MatchString(prompt) {
			t.Fatalf("pattern %q not found in prompt:\n%s", pattern, prompt)
		}
	}
}
