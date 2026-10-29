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
	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// SignupFlow is one in-progress "open a shop" walk. The shop name
// comes first, because her number, which she is chatting from, is
// her owner identity and needs no asking. When payments are wired,
// one more thing is settled before the shop opens: the name on the
// MoMo account her link money will settle to, so her own Seev
// subaccount can be created in the same sitting. Seev can usually
// read that name off the number itself, and then she only confirms
// it; typing it is the fallback, never the first ask.
type SignupFlow struct {
	Step       string // "name", then "holder" or "confirmholder" when payments are wired
	Name       string
	HolderName string
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

// signupQuestionText is the open question for a signup
// step, as one string, so the seller stage read can reason
// against the very question she was asked.
func signupQuestionText(from string, flow *SignupFlow) string {
	switch flow.Step {
	case "holder":
		return "I need the name on the MoMo account, the name the account itself carries. What name is on it?"
	case "confirmholder":
		return "Money from your payment links will go to the MoMo account on this number, " + checkout.LocalNumber(from) + ". The name on that account reads " + flow.HolderName + ". Say yes if that is the right account, or tell me the name on the right account."
	default: // "name"
		return "What is your shop called?"
	}
}

// serveSignupQuestion reads one signup turn before the
// walk may consume it: a question she asks about the step
// is answered from the facts and the step is re-asked; a
// call-off drops the walk as her own cancel word does.
func serveSignupQuestion(from string, s *Session, text string) bool {
	flow := s.Signup
	if flow == nil {
		return false
	}
	given := ""
	if flow.Name != "" {
		given = "shop name " + flow.Name
	}
	reading := thinkSellerWalkIntent(from, s, "opening her shop", signupQuestionText(from, flow), given, text, nil)
	switch reading.Intent {
	case stageIntentCancel:
		delete(sessions, sessionKey("", from))
		whatsapp.SendText(from, "Dropped it. No shop was created. Say \"open a shop\" whenever you are ready.")
		return true
	case stageIntentAsk:
		if reading.Reply != "" && voiceClean(reading.Reply) && figuresClean(reading.Reply) {
			whatsapp.SendText(from, reading.Reply)
			whatsapp.SendText(from, signupQuestionText(from, flow))
			return true
		}
	}
	return false
}

// advanceSignup feeds one message to the open-a-shop walk.
func advanceSignup(from string, s *Session, text, lower string) {
	if controlWord(lower) == "cancel" {
		delete(sessions, sessionKey("", from))
		whatsapp.SendText(from, "Dropped it. No shop was created. Say \"open a shop\" whenever you are ready.")
		return
	}
	noteSpeakerWords(s, text)
	if serveSignupQuestion(from, s, text) {
		return
	}
	flow := s.Signup
	if flow == nil {
		flow = &SignupFlow{Step: "name"}
		s.Signup = flow
	}

	switch flow.Step {
	case "holder":
		holder := strings.TrimSpace(text)
		if holder == "" || strings.ContainsAny(holder, "0123456789") {
			whatsapp.SendText(from, "I need the name on the MoMo account, the name the account itself carries. What name is on it?")
			return
		}
		flow.HolderName = holder
		finishSignup(from, flow)
	case "confirmholder":
		if isClearYes(lower) {
			finishSignup(from, flow)
			return
		}
		holder := strings.TrimSpace(text)
		if holder == "" || strings.ContainsAny(holder, "0123456789") {
			whatsapp.SendText(from, "Say yes if that is the account your money should go to, or tell me the name on the right account.")
			return
		}
		flow.HolderName = holder
		finishSignup(from, flow)
	default: // "name"
		name := strings.TrimSpace(text)
		if name == "" {
			whatsapp.SendText(from, "What is your shop called?")
			return
		}
		flow.Name = name
		if !checkout.Enabled() {
			finishSignup(from, flow)
			return
		}
		// Payments are wired, so her own payment account is part of
		// opening the shop. Seev can usually read the name on her
		// MoMo account from the number: she confirms it. Only when
		// the read fails does she have to type it.
		if resolved, ok := checkout.ResolveAccount(from); ok {
			flow.HolderName = resolved
			flow.Step = "confirmholder"
			whatsapp.SendText(from, "Your shop is "+name+". One thing before it opens: money from your payment links will go to the MoMo account on this number, "+checkout.LocalNumber(from)+". The name on that account reads "+resolved+". Say yes if that is the right account, or tell me the name on the right account.")
			return
		}
		flow.Step = "holder"
		whatsapp.SendText(from, "Your shop is "+name+". One thing before it opens: what name is on the MoMo account for this number? Money from your payment links will go to that account.")
	}
}

// finishSignup creates the shop and, when payments are wired and a
// holder name was settled, opens her own Seev subaccount in the
// same sitting. The subaccount is never a gate: if its creation
// fails, the shop still opens and she is told plainly, with a retry
// in her own commands ("set up my payment").
func finishSignup(from string, flow *SignupFlow) {
	shop, err := catalogue.CreateMerchant(flow.Name, from)
	if err != nil {
		whatsapp.SendText(from, "I could not open the shop just yet: "+err.Error()+". Tell me the shop name again, or say cancel.")
		return
	}

	payoutLine := ""
	if checkout.Enabled() && flow.HolderName != "" {
		_ = catalogue.SetSubaccount(shop.ID, "", flow.HolderName)
		if code, ok := checkout.CreateSubaccount(shop.Name, shop.PayoutPhone, flow.HolderName); ok {
			_ = catalogue.SetSubaccount(shop.ID, code, flow.HolderName)
			payoutLine = "Your payment account is set up too: money from your payment links settles to your MoMo in the name " + flow.HolderName + ", usually the next day. Of every sale you keep 99%; 1% stays with the platform."
		} else {
			payoutLine = "One thing needs attention: I could not set up your payment account just now. Your shop is open and you can stock it and sell. Say \"set up my payment\" whenever you are ready and we will do it again."
		}
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
	if payoutLine != "" {
		whatsapp.SendText(from, payoutLine)
	}
	whatsapp.SendText(from, "Let us add a new product. What is it called? You can say cancel at any point and nothing will be saved.")
}
