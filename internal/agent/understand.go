package agent

// Thinking first. This agent is a mind working for a shop, not
// a board of canned answers. People talk to it the way they
// talk to a person, in their own words, in fragments, in
// Ghanaian English and Pidgin, about whatever life has just
// done to the conversation, and a person behind a counter
// would understand them, because a person thinks. So on every
// open turn the agent thinks first: the model reads the
// message with the whole situation in hand (who is speaking,
// the shop, the shelf, her open order, where the conversation
// stands), does its own analysis of what the person means and
// what the moment calls for, and names the act the platform
// should take. The response then comes out of that thinking,
// but it is spoken inside the platform's world and bound by
// its facts: the code executes the act with the shop's own
// records, every figure is a recorded figure, and a draft
// carrying an invented number is never sent. The model has
// freedom to understand and to phrase; it has no freedom to
// invent stock, prices, times, promises, or the truth about
// money, and a payment it understands a buyer to be claiming
// is still only a claim, settled by the merchant's own word
// or the platform's confirmation, exactly as the rules had it.
//
// The keyword branches below the thinking step are the net,
// not the mind: they catch what the thinking declined and
// everything on a day the model cannot be reached, and they
// are byte for byte the behaviour the agent had before it
// learned to think. Guided flows (a haggle the model already
// leads, the fulfilment steps, an onboarding walk, a pending
// close) own their turns and are never interrupted by this
// step.

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

// The acts a thinking turn can name. Payment acts are executed
// by code from the order itself; the model's drafted words are
// used only for ask and unclear, and only after every figure
// in them has been verified against the facts.
const (
	intentPaymentBlocked = "payment_blocked" // could not pay: network, service down, bank trouble
	intentPaymentExpired = "payment_expired" // the link is dead, expired, will not open as a link
	intentPaymentLater   = "payment_later"   // she will pay, just not now
	intentPaymentWant    = "payment_want"    // she wants to pay now, or asks for the link
	intentPaymentClaim   = "payment_claim"   // she says she has paid directly: a claim, never proof
	intentItem           = "item"            // she is asking about a product on the shelf
	intentOrderStatus    = "order_status"    // she is asking after an order she already made
	intentShelf          = "shelf"           // she wants to see what the shop has
	intentAsk            = "ask"             // a question the facts can answer
	intentOffTopic       = "offtopic"        // nothing to do with the shop
	intentUnclear        = "unclear"         // a person would still ask what she means
)

type understood struct {
	Intent string `json:"intent"`
	Item   string `json:"item"`
	Reply  string `json:"reply"`

	// NamedAmount is not the model's: it is the amount the
	// buyer herself named in her own message, read by code,
	// carried here so a claim records what she said she sent.
	NamedAmount *float64 `json:"-"`
	// DirectRoute is read by code from her words too: she said
	// the money went to the merchant's own number, so the
	// merchant is who verifies it. Without it, a paid report on
	// a link order is verified by the agent with Seev itself.
	DirectRoute bool `json:"-"`
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

const thinkSystem = `You are the thinking of a WhatsApp sales agent for a small shop in Ghana. A person is talking to the shop in their own words, the way people talk: fragments, Ghanaian English, Pidgin, Twi words, half sentences, whatever life has just done to the conversation. Before the shop answers, you think, the way the person behind the counter would: read what she means, look at her situation in the facts you are given, work out what she is asking for and what would actually help her, and name the act the shop should take.

You have freedom in understanding and in phrasing. You have no freedom to invent: never a figure, a product, a stock level, a time or a promise that is not in the facts. People must never be told "I don't understand" for a sentence a person would follow at once.

Answer with strict JSON only, no other text:
{"intent": "...", "item": "...", "reply": "..."}

The acts you can name:
- "payment_blocked": she tried to pay, or is trying, and could not: network trouble, the payment service not working, the bank or MoMo failing, no reception. Not a refusal to pay.
- "payment_expired": the payment link itself is dead: it has expired, it is closed, it will not open as a link, she asks for a NEW link because the old one is finished.
- "payment_later": she will pay, just later: tomorrow, in the evening, when money comes.
- "payment_want": she wants to pay now, or asks for the payment link: "send me the payment link", "I want to pay now", "let me pay", asking for the link again without saying it is dead.
- "payment_claim": she says she has paid, in her own words, whether or not she uses the word paid: "I just sent the money", "check your phone, I have done it", "I have made the payment". Whether she paid by the link or to the shop's number, this is the act: the platform then verifies a link payment with the payment service itself, and sends only a payment to the shop's own number to the merchant to confirm. Her word alone never settles anything.
- "item": she is asking about, or wants, a product on the shelf. Put the product's name, as it appears in the facts, in "item".
- "order_status": she is asking after an order she already made: where it stands, whether it is paid, what happens next with it.
- "shelf": she wants to see what the shop has, or start again from the shelf.
- "ask": a question about the shop that the facts you are given can answer. Draft the answer in "reply": short, plain, warm, in her own register, using ONLY the facts given.
- "offtopic": the message has nothing to do with this shop, its goods, an order or a payment.
- "unclear": even reading kindly, a person would have to ask what is meant. Draft ONE short clarifying question in "reply", in her own terms, offering the likely two readings.

Leave "item" and "reply" empty where the act does not use them.`

// thinkBuyerMessage is the thinking step for a buyer: the
// model analyses her message against her whole situation. It
// returns nil when no model is configured or the analysis
// cannot be read, and the caller keeps the net's behaviour.
func thinkBuyerMessage(from string, s *Session, merchant *catalogue.Merchant, shopID, msg string) *understood {
	shelf, _ := shelfFacts(shopID)
	var b strings.Builder
	b.WriteString("The shop is " + merchant.Name + ".\n")
	b.WriteString("On the shelf: " + shelf + ".\n")
	if merchant.DeliveryFee > 0 {
		b.WriteString("Delivery costs " + money.GHS(merchant.DeliveryFee) + ". " + merchant.DeliveryNote + "\n")
	} else {
		b.WriteString("The shop has no set delivery fee; delivery is arranged with the buyer.\n")
	}
	if s != nil && s.LastViewed != nil {
		b.WriteString("She was last looking at: " + s.LastViewed.Name + ".\n")
	}
	if order := orders.Default.NewestUnpaidFor(shopID, from); order != nil {
		b.WriteString("She has an open order waiting for payment: " + order.ProductName + ", " + money.GHS(order.Charged()) + ", reference " + order.Reference + ".\n")
		if order.ClaimStatus == "open" {
			b.WriteString("She has already told the shop she paid directly, and that claim is waiting for the merchant to confirm.\n")
		}
	} else if latest := orders.Default.NewestFor(shopID, from); latest != nil && latest.Status == "paid" {
		b.WriteString("Her latest order (" + latest.Reference + ", " + latest.ProductName + ") is paid.\n")
	} else {
		b.WriteString("She has no open order waiting for payment.\n")
	}
	b.WriteString("\nHer message: " + msg)
	sys := thinkSystem
	out := model.Chat([]model.Message{{Role: "user", Content: b.String()}}, model.Options{System: &sys})
	u := parseUnderstood(out)
	if u != nil {
		u.NamedAmount = claimAmountOf(msg)
		u.DirectRoute = directWordsRe.MatchString(strings.ToLower(msg))
	}
	return u
}

// merchantDayFigures returns the merchant's day as a fact
// line for the thinking prompt, plus the day's figures a
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
	sys := thinkSystem
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
	case intentPaymentBlocked, intentPaymentExpired, intentPaymentLater, intentPaymentWant, intentPaymentClaim,
		intentItem, intentOrderStatus, intentShelf, intentAsk, intentOffTopic, intentUnclear:
		return &u
	}
	return nil
}

// orderPaymentURL rebuilds an order's payment link address
// from the book alone: a live order's hosted page lives under
// its provider reference, and a dry-run order under its own
// reference, the two shapes the sale itself minted. This is
// what lets a payment conversation survive the agent's own
// restarts: the link is in the book, not only in a session.
func orderPaymentURL(o *orders.Order) string {
	if o.ProviderRef != nil && *o.ProviderRef != "" {
		return "https://pay.seevplus.com/" + *o.ProviderRef
	}
	return "https://pay.example.test/" + o.Reference
}

// actOnBuyerUnderstanding carries out what a buyer was
// understood to mean. It returns true when it answered, so the
// caller does not fall back to the net.
func actOnBuyerUnderstanding(from string, s *Session, merchant *catalogue.Merchant, shopID string, u *understood) bool {
	switch u.Intent {
	case intentOffTopic:
		laneStrike(from, s, merchant)
		return true
	case intentShelf:
		sendPieces(from, "Welcome to "+merchant.Name+".", productListText(shopID))
		return true
	case intentOrderStatus:
		o := orders.Default.NewestFor(shopID, from)
		if o == nil {
			return false
		}
		sendPieces(from, orderReadback(merchant, o)...)
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
	case intentPaymentClaim:
		order := orders.Default.NewestUnpaidFor(shopID, from)
		if order == nil {
			return false
		}
		handlePaidReport(from, merchant, order, u.NamedAmount, u.DirectRoute)
		return true
	case intentPaymentBlocked, intentPaymentExpired, intentPaymentLater, intentPaymentWant:
		order := orders.Default.NewestUnpaidFor(shopID, from)
		if order == nil {
			return false
		}
		switch u.Intent {
		case intentPaymentBlocked:
			return answerPaymentBlocked(from, s, merchant, shopID, order)
		case intentPaymentExpired:
			return reissueLink(from, s, merchant, order)
		case intentPaymentWant:
			return sendPaymentLink(from, s, merchant, order)
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

// sendPaymentLink answers a buyer who wants to pay now, or
// asks for the link: the link comes from the order itself, so
// it is served the same whether her session is warm or the
// agent has restarted since the sale. When the order carries
// nothing to send (its first link never landed), a fresh one
// is minted instead of a dead end being repeated.
func sendPaymentLink(from string, s *Session, merchant *catalogue.Merchant, order *orders.Order) bool {
	url := ""
	if order.ProviderRef != nil && *order.ProviderRef != "" {
		url = orderPaymentURL(order)
	} else if s != nil && s.PaymentURL != "" {
		url = s.PaymentURL
	}
	if url == "" {
		return reissueLink(from, s, merchant, order)
	}
	if s != nil {
		s.PaymentURL = url
	}
	pieces := []string{
		"Of course. Here is the payment link for your order " + order.Reference + " (" + order.ProductName + ", " + money.GHS(order.Charged()) + ").",
		"Pay here to complete your order: " + url,
	}
	if merchant.PayoutPhone != "" {
		pieces = append(pieces, "Or send MoMo directly to "+merchant.Name+"'s number, "+merchant.PayoutPhone+", and send me the screenshot here.")
	}
	sendPieces(from, pieces...)
	return true
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
