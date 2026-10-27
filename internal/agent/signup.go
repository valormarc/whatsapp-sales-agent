package agent

// Merchant signup in chat: how a new shop is born on the shared
// number. The real world model asks nothing of a merchant beyond the
// chat she already knows: no Facebook account, no Business Suite, no
// forms. She says she wants to sell, names her shop, and the system
// opens it under her own number, with a unique shop code her
// customers will enter by. From there she is an owner, and the
// ordinary merchant mode, starting with the add-a-product walk,
// takes over.

import (
	"os"
	"regexp"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// SignupFlow is one in-progress "open a shop" walk. The only step
// today is the shop name: creation itself needs nothing else from
// her, because her number, which she is chatting from, is her owner
// identity.
type SignupFlow struct {
	Step string // "name"
}

var sellIntentRe = regexp.MustCompile(`\b(open|start|create|set up)\b[^.!?]*\bshop\b|\bnew shop\b|\bi want to sell\b|\bi would like to sell\b|\bi want to start selling\b|\bbecome a (seller|merchant)\b`)

// startSignup begins the open-a-shop walk on the pre-shop session.
func startSignup(from string) {
	sessions[sessionKey("", from)] = &Session{Stage: "signup", Signup: &SignupFlow{Step: "name"}}
	whatsapp.SendText(from, "Welcome. Let us open your shop here, in this chat. Nothing to install and nothing to sign up for anywhere else. First, what is your shop called? You can say cancel and nothing will be created.")
}

// shareTextFor is what she gives her customers: her wa.me link when
// the agent's own number is configured, carrying the shop entry
// text; otherwise the code and the entry text themselves, which do
// the same work typed into the shared number's chat.
func shareTextFor(shop catalogue.Merchant) string {
	entry := "SHOP-" + shop.ShopCode
	if number := digitsOnly(os.Getenv("AGENT_WHATSAPP_NUMBER")); number != "" {
		return "Share this link with your customers: https://wa.me/" + number + "?text=" + entry + " . Anyone who taps it lands in your shop here, and only your shop."
	}
	return "Your shop code is " + shop.ShopCode + ". Ask your customers to message this number and start with " + entry + ", and they will land in your shop, and only your shop."
}

// advanceSignup feeds one message to the open-a-shop walk.
func advanceSignup(from string, s *Session, text, lower string) {
	if controlWord(lower) == "cancel" {
		delete(sessions, sessionKey("", from))
		whatsapp.SendText(from, "Dropped it. No shop was created. Say \"open a shop\" whenever you are ready.")
		return
	}

	name := strings.TrimSpace(text)
	if name == "" {
		whatsapp.SendText(from, "What is your shop called?")
		return
	}

	shop, err := catalogue.CreateMerchant(name, from)
	if err != nil {
		whatsapp.SendText(from, "I could not open the shop just yet: "+err.Error()+". Tell me the shop name again, or say cancel.")
		return
	}

	// Her shop exists now, so she stops being a pre-shop sender: the
	// session moves under her own shop's key, with the add-a-product
	// walk already open, the same handover an existing merchant gets.
	delete(sessions, sessionKey("", from))
	sessions[sessionKey(shop.ID, from)] = &Session{
		Stage:      "merchant",
		MerchantID: shop.ID,
		Onboarding: &OnboardingFlow{Step: "name"},
	}
	whatsapp.SendText(from, "Your shop "+shop.Name+" is open, and this number is its owner, so I will know you whenever you chat. "+shareTextFor(shop))
	whatsapp.SendText(from, "Let us add a new product. What is it called? You can say cancel at any point and nothing will be saved.")
}
