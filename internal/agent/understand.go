package agent

// The understanding step. People talk to this agent the way
// they talk to a person: in their own words, in fragments, in
// Ghanaian English and Pidgin, about whatever life has just
// done to the conversation. A buyer whose payment failed
// because the network dropped does not write in keywords, and
// she must never be answered "I don't understand" or handed
// the same canned line as if her message was never read.
//
// So before the agent gives up on a message its rules did not
// match, the message is understood: the model reads it with
// the facts of the conversation (the shop, the shelf, the
// speaker's open order) and returns what the person means and
// intends, as one of a small set of intents. The agent then
// acts on that intent with its own verified words and facts.
// The model understands and, for open questions, drafts; code
// still owns every figure, every price and the truth about
// money. A drafted reply carrying a figure that is not one of
// the facts is never sent.
//
// Understanding is not believing: a buyer understood to be
// claiming she has paid is still only claiming, and the claim
// still goes to the merchant, exactly as before. The claim
// path runs before this step and is never reached by it.

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/checkout"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/insights"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// The intents the understanding step can return. Payment
// intents are acted on by code with the order's own facts; the
// model's drafted words are used only for ask and unclear.
const (
	intentPaymentBlocked = "payment_blocked" // could not pay: network, service down, bank trouble
	intentPaymentExpired = "payment_expired" // the link is dead, expired, will not open as a link
	intentPaymentLater   = "payment_later"   // she will pay, just not now
	intentItem           = "item"            // she is asking about a product on the shelf
	intentAsk            = "ask"             // a question the facts can answer
	intentOffTopic       = "offtopic"        // nothing to do with the shop
	intentUnclear        = "unclear"         // a person would still ask what she means
)

type understood struct {
	Intent string `json:"intent"`
	Item   string `json:"item"`
	Reply  string `json:"reply"`
}

var figureTokenRe = regexp.MustCompile(`\d+(?:\.\d+)?`)

// figuresClean reports whether every figure in a drafted reply
// is one of the facts it was allowed to use. A draft that
// invents a number is worse than no draft, so it is refused
// whole and the caller falls back to its own words.
func figuresClean(text string, allowed ...float64) bool {
	for _, tok := range figureTokenRe.FindAllString(text, -1) {
		v, err := strconv.ParseFloat(tok, 64)
		if err != nil {
			return false
		}
		ok := false
		for _, a := range allowed {
			if money.Round(v*100) == money.Round(a*100) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func shelfFacts(shopID string) (string, []float64) {
	var parts []string
	var figures []float64
	for _, p := range catalogue.ListProducts(shopID) {
		parts = append(parts, p.Name+" at "+money.GHS(p.ListPrice))
		figures = append(figures, p.ListPrice)
	}
	if len(parts) == 0 {
		return "the shelf is empty", figures
	}
	return strings.Join(parts, "; "), figures
}

const understandSystem = `You are the understanding step of a WhatsApp sales agent for a small shop in Ghana. A person is talking to the shop in their own words, the way people talk: fragments, Ghanaian English, Pidgin, Twi words, half sentences. Your one job is to work out what the person means and what they intend, from their words and the facts you are given. People must never be told "I don't understand" for a sentence a person would follow at once.

Answer with strict JSON only, no other text:
{"intent": "...", "item": "...", "reply": "..."}

The intents:
- "payment_blocked": the person tried to pay, or is trying, and could not: network trouble, the payment service not working, the bank or MoMo failing, no reception. Not a refusal to pay.
- "payment_expired": the payment link itself is dead: it has expired, it is closed, it will not open as a link.
- "payment_later": the person will pay, just later: tomorrow, in the evening, when money comes.
- "item": the person is asking about, or wants, a product on the shelf. Put the product's name, as it appears in the facts, in "item".
- "ask": a question about the shop that the facts you are given can answer. Draft the answer in "reply": short, plain, warm, in the person's own register, using ONLY the facts given. Never invent a figure, a product, a time or a promise.
- "offtopic": the message has nothing to do with this shop, its goods, an order or a payment.
- "unclear": even reading kindly, a person would have to ask what is meant. Draft ONE short clarifying question in "reply", in the person's own terms, offering the likely two readings.

Leave "item" and "reply" empty where the intent does not use them.`

// understandBuyerMessage asks the model what a buyer means.
// It returns nil when no model is configured or the answer
// cannot be read, and the caller keeps its old behaviour.
func understandBuyerMessage(from string, s *Session, merchant *catalogue.Merchant, shopID, msg string) *understood {
	shelf, _ := shelfFacts(shopID)
	var b strings.Builder
	b.WriteString("The shop is " + merchant.Name + ".\n")
	b.WriteString("On the shelf: " + shelf + ".\n")
	if merchant.DeliveryFee > 0 {
		b.WriteString("Delivery costs " + money.GHS(merchant.DeliveryFee) + ". " + merchant.DeliveryNote + "\n")
	} else {
		b.WriteString("The shop has no set delivery fee; delivery is arranged with the buyer.\n")
	}
	if order := orders.Default.NewestUnpaidFor(shopID, from); order != nil {
		b.WriteString("This customer has an open order waiting for payment: " + order.ProductName + ", " + money.GHS(order.Charged()) + ", reference " + order.Reference + ".\n")
	} else {
		b.WriteString("This customer has no open order waiting for payment.\n")
	}
	b.WriteString("\nThe customer's message: " + msg)
	sys := understandSystem
	out := model.Chat([]model.Message{{Role: "user", Content: b.String()}}, model.Options{System: &sys})
	return parseUnderstood(out)
}

// merchantDayFigures returns the merchant's day as a fact
// line for the understanding prompt, plus the day's figures a
// drafted reply is allowed to use.
func merchantDayFigures(merchant *catalogue.Merchant, shopID string) (string, []float64) {
	day := daybook.Default.GetDayState(shopID, daybook.Options{Allowance: merchant.DailyGiveawayAllowance})
	return insights.DayReportText(day), []float64{day.Revenue, day.GivenAway, day.Allowance, float64(day.Sales), float64(day.WalkAways)}
}

// understandMerchantMessage asks the model what the merchant
// herself means, with her own facts (her shelf and her day).
func understandMerchantMessage(from string, merchant *catalogue.Merchant, shopID, msg string) *understood {
	shelf, _ := shelfFacts(shopID)
	dayText, _ := merchantDayFigures(merchant, shopID)
	var b strings.Builder
	b.WriteString("The person writing is the OWNER of the shop " + merchant.Name + ", talking to her own sales agent.\n")
	b.WriteString("On her shelf: " + shelf + ".\n")
	b.WriteString("Her day so far: " + dayText + "\n")
	b.WriteString("Things she can ask the agent to do: show her catalogue, change a price or a last price, set her daily giveaway allowance, remove a product, add a product by describing it or sending a photo, and tell her how today is going.\n")
	b.WriteString("For her, the payment and item intents still apply when she is asking about a customer's order or a product; use \"ask\" for any question her shelf, her day or those abilities can answer, drafting the reply from the facts only.\n")
	b.WriteString("\nThe owner's message: " + msg)
	sys := understandSystem
	out := model.Chat([]model.Message{{Role: "user", Content: b.String()}}, model.Options{System: &sys})
	return parseUnderstood(out)
}

func parseUnderstood(out *string) *understood {
	if out == nil {
		return nil
	}
	text := *out
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil
	}
	var u understood
	if err := json.Unmarshal([]byte(text[start:end+1]), &u); err != nil {
		return nil
	}
	switch u.Intent {
	case intentPaymentBlocked, intentPaymentExpired, intentPaymentLater, intentItem, intentAsk, intentOffTopic, intentUnclear:
		return &u
	}
	return nil
}

// actOnBuyerUnderstanding carries out what a buyer was
// understood to mean. It returns true when it answered, so the
// caller does not fall back to its canned line.
func actOnBuyerUnderstanding(from string, s *Session, merchant *catalogue.Merchant, shopID string, u *understood) bool {
	switch u.Intent {
	case intentOffTopic:
		laneStrike(from, s, merchant)
		return true
	case intentItem:
		p := catalogue.FindProduct(shopID, strings.ToLower(u.Item))
		if p == nil {
			return false
		}
		s.LastViewed = p
		if s.Stage == "agreed" {
			s.Stage = "browsing"
		}
		sendPieces(from, "Still available.", p.Name+": "+p.Description+" It goes for "+money.GHS(p.ListPrice)+"."+offerInvite(p, " Want to make an offer?"))
		return true
	case intentPaymentBlocked, intentPaymentExpired, intentPaymentLater:
		order := orders.Default.NewestUnpaidFor(shopID, from)
		if order == nil {
			return false
		}
		switch u.Intent {
		case intentPaymentBlocked:
			return answerPaymentBlocked(from, s, merchant, shopID, order)
		case intentPaymentExpired:
			return reissueLink(from, s, merchant, order)
		default:
			sendPieces(from,
				"No problem at all.",
				"Your order "+order.Reference+" for the "+order.ProductName+" at "+money.GHS(order.Charged())+" is held for you.",
				"Pay whenever you are ready, with the link I sent you. If anything changes, just tell me here.")
			return true
		}
	case intentAsk, intentUnclear:
		if strings.TrimSpace(u.Reply) == "" {
			return false
		}
		_, figures := shelfFacts(shopID)
		if order := orders.Default.NewestUnpaidFor(shopID, from); order != nil {
			figures = append(figures, order.Charged(), order.Price)
		}
		if merchant.DeliveryFee > 0 {
			figures = append(figures, merchant.DeliveryFee)
		}
		if !figuresClean(u.Reply, figures...) {
			return false
		}
		sendPieces(from, u.Reply)
		return true
	}
	return false
}

// answerPaymentBlocked answers a buyer who could not pay: the
// money has not moved, the reason is at her end, and she is
// not refusing. Her order stays open, the ways to pay are
// restated once, and the merchant hears that a buyer is trying
// to pay her, so an unmade payment is never silent at either
// end.
func answerPaymentBlocked(from string, s *Session, merchant *catalogue.Merchant, shopID string, order *orders.Order) bool {
	pieces := []string{
		"I am sorry the network is giving you trouble. It happens.",
		"Nothing has been charged, and your order " + order.Reference + " for the " + order.ProductName + " at " + money.GHS(order.Charged()) + " is still open and held for you.",
		"When the network settles, the same link I sent you will work.",
	}
	if merchant.PayoutPhone != "" {
		pieces = append(pieces, "Or send MoMo directly to "+merchant.Name+"'s number, "+merchant.PayoutPhone+", and send me the screenshot here.")
	}
	sendPieces(from, pieces...)
	if phone := merchantNotifyPhone(merchant); phone != "" {
		whatsapp.SendText(phone, "Payment trouble\n\n*"+order.ProductName+"*\nBuyer "+from+" says she could not complete the payment for order "+order.Reference+" ("+money.GHS(order.Charged())+"): the network or the payment service at her end.\n\nThe order is still open. She has the link and your MoMo number.")
	}
	return true
}

// reissueLink answers a buyer whose payment link is dead: a
// fresh checkout is minted for the same order, under a new
// attempt key (Seev will not replay a used idempotency key),
// and the order now points at the new session, so the webhook
// still settles this order when she pays. The order's own
// reference, the one the buyer and the merchant know, never
// changes.
func reissueLink(from string, s *Session, merchant *catalogue.Merchant, order *orders.Order) bool {
	attempts := order.LinkAttempts
	if attempts < 1 {
		attempts = 1
	}
	if attempts >= 3 {
		sendPieces(from,
			"I have made you fresh links a few times now, and I do not want to keep sending dead ends.",
			"Your order "+order.Reference+" for the "+order.ProductName+" at "+money.GHS(order.Charged())+" is still open.",
		)
		if merchant.PayoutPhone != "" {
			sendPieces(from, "Send MoMo directly to "+merchant.Name+"'s number, "+merchant.PayoutPhone+", and send me the screenshot here, and we will complete it that way.")
		}
		return true
	}
	link := checkout.CreatePaymentLink(checkout.LinkRequest{
		Amount:        order.Charged(),
		Reference:     order.Reference + "-" + strconv.Itoa(attempts+1),
		RecipientName: order.PackageName,
		Subaccount:    merchant.SubaccountCode,
	})
	if link.URL == nil {
		whatsapp.SendText(from, "The link is not coming up right now, even a fresh one. The shop has been told, and your order "+order.Reference+" is still open.")
		if phone := merchantNotifyPhone(merchant); phone != "" {
			whatsapp.SendText(phone, "A fresh payment link could not be made for order "+order.Reference+" ("+order.ProductName+", "+money.GHS(order.Charged())+"). Buyer "+from+" is waiting on it. Please reach out to her here.")
		}
		return true
	}
	newRef := link.ProviderRef
	orders.Default.Update(order.Reference, func(o *orders.Order) {
		o.ProviderRef = newRef
		o.LinkAttempts = attempts + 1
	})
	if s != nil {
		s.PaymentURL = *link.URL
	}
	pieces := []string{
		"That link has expired, so I have made you a fresh one. The old one will not work now.",
		"Pay here to complete your order: " + *link.URL,
	}
	if merchant.PayoutPhone != "" {
		pieces = append(pieces, "Or send MoMo directly to "+merchant.Name+"'s number, "+merchant.PayoutPhone+", and send me the screenshot here.")
	}
	sendPieces(from, pieces...)
	return true
}

// actOnMerchantUnderstanding carries out what the merchant was
// understood to mean: a question the facts answer, or one
// clarifying question. It never performs a change to her shop:
// changes still go through her plain commands, so nothing
// about her catalogue moves on a model's reading alone.
func actOnMerchantUnderstanding(from string, merchant *catalogue.Merchant, shopID string, u *understood) bool {
	switch u.Intent {
	case intentAsk, intentUnclear:
		if strings.TrimSpace(u.Reply) == "" {
			return false
		}
		_, figures := shelfFacts(shopID)
		_, dayFigures := merchantDayFigures(merchant, shopID)
		figures = append(figures, dayFigures...)
		if !figuresClean(u.Reply, figures...) {
			return false
		}
		sendPieces(from, u.Reply)
		return true
	}
	return false
}

// merchantHelpText is the last resort for a merchant message
// nothing understood: plain, honest, and it names what she can
// ask for, in her own chat.
const merchantHelpText = "I did not catch that one. I can show your catalogue, add a product when you describe it or send its photo, change a price or a last price, set your daily allowance, and tell you how today is going. Say \"my catalogue\", or ask me how today is going."
