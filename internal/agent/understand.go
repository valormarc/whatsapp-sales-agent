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
// money, and a payment a buyer says she has made is still
// only her word: the platform confirms it against the
// payment service itself, and nothing else settles it.
//
// The keyword branches below the thinking step are the net,
// not the mind: they catch what the thinking declined and
// everything on a day the model cannot be reached, and they
// are byte for byte the behaviour the agent had before it
// learned to think. Guided flows keep their own shapes (a
// haggle the model already leads, an onboarding walk, a
// pending close), and the fulfilment steps are read by
// their own stage intent layer (intentgate.go) before a
// step may treat a reply as its answer, so a picture ask or
// a call-off inside a pending sale is judged by meaning
// first, exactly as an open turn is here.

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
	intentPaymentClaim   = "payment_claim"   // she says she has paid: a report the platform verifies itself, never proof by itself
	intentItem           = "item"            // she is asking about a product on the shelf
	intentPicture        = "picture"         // she wants to see the product before she goes on
	intentOrderStatus    = "order_status"    // she is asking after an order she already made
	intentOrderSent      = "order_sent"      // the owner reports the goods for a paid order have gone out
	intentOrderReceived  = "order_received"  // the buyer confirms her paid order reached her
	intentOrderMissing   = "order_missing"   // the buyer reports her paid order has not arrived
	intentOrderDispute   = "order_dispute"   // the buyer opens a dispute on a paid order: cheated, scammed, refund demanded
	intentReorder        = "reorder"         // the buyer wants a past order again, the same items with the same details
	intentRecommend      = "recommend"       // the buyer describes a need and asks the shop to choose: a gift, an occasion, a purpose
	intentTooExpensive   = "too_expensive"   // the buyer balks at a price without naming a figure: a cheaper suitable thing goes in front of her
	intentOrderPayment   = "order_payment"   // the owner asks about the payment on an order
	intentAskBuyer       = "ask_buyer"       // the owner tells the agent to ask her buyer
	intentAttention      = "attention"       // the owner asks what needs her attention: her outstanding work, answered from the books
	intentNewListing     = "new_listing"     // the owner is giving the agent a product to list
	intentShelf          = "shelf"           // she wants to see what the shop has
	intentBasketView     = "basket_view"     // she asks to see her basket: what she has picked and settled
	intentCourtesy       = "courtesy"        // thanks, gratitude, a goodbye: the conversation closing warmly
	intentAsk            = "ask"             // a question the facts can answer
	intentOffTopic       = "offtopic"        // nothing to do with the shop
	intentUnclear        = "unclear"         // a person would still ask what she means
)

// modelFigure is a figure in a model's JSON reading. The
// charters ask for a bare number, but a live run showed the
// model sometimes handing a budget back in words —
// "600 cedis" — and a strict float64 field then rejected
// the WHOLE reading, intent, matches and all, for the sake
// of one auxiliary figure. The figure is read tolerantly
// here instead: a JSON number as itself, or the first
// figure token in a JSON string, valued by the platform's
// own figure grammar (commas and the k included). A string
// with no figure in it reads as no figure, and the caller
// still verifies any budget against the person's own words
// before carrying it, so tolerance can never invent money.
type modelFigure float64

func (f *modelFigure) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = 0
		return nil
	}
	var n float64
	if err := json.Unmarshal(data, &n); err == nil {
		*f = modelFigure(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		*f = 0
		return nil
	}
	token := figureTokenRe.FindString(s)
	if token == "" {
		*f = 0
		return nil
	}
	if v, err := parseFigure(token); err == nil {
		*f = modelFigure(v)
	} else {
		*f = 0
	}
	return nil
}

type understood struct {
	Intent string      `json:"intent"`
	Item   string      `json:"item"`
	Reply  string      `json:"reply"`
	Budget modelFigure `json:"budget"`

	// DirectRoute is read by code from her words: she said the
	// money went somewhere other than the payment link, so it
	// is a payment the platform cannot see or confirm at all.
	// Without it, a paid report on a link order is verified by
	// the agent with Seev itself.
	DirectRoute bool `json:"-"`

	// Words is her message itself, carried for the acts that
	// must quote her exactly (a dispute is passed to the
	// seller in her own words).
	Words string `json:"-"`
}

// figureTokenRe reads one figure token as it is written,
// thousands comma and all: GH₵1,800 is one figure of 1800,
// never a 1 and an 800.
// The k form comes first in the alternation: "900k" is
// nine hundred thousand, and a tokeniser that stops at
// the digits reads it as 900 (the fifth audit found the
// face-value read on eight surfaces at once, while the
// platform's own figure readers — figurePattern and
// parseNumbered — have always honoured the k). The k
// must end the figure: "1kg" is a weight and tokenises
// as its plain 1. Tokens are valued with parseFigure,
// which applies the thousand the k carries.
var figureTokenRe = regexp.MustCompile(`\d[\d,]*(?:\.\d+)?[kK]\b|\d[\d,]*(?:\.\d+)?`)

// figuresClean reports whether every figure in a drafted reply
// is one of the facts it was allowed to use. A draft that
// invents a number is worse than no draft, so it is refused
// whole and the caller falls back to its own words.
// voiceClean is the output voice guard on drafted replies:
// the agent understands every kind of English and answers in
// correct English only, politely and respectfully. A draft
// carrying Pidgin, an assumed title, or a copy of the person's
// own dialect is never sent, however the person wrote. The
// list is deliberately narrow: plain market words a correct
// English answer may use are not in it.
var voiceBadRe = regexp.MustCompile(`(?i)\b(madam|abeg|wahala|wetin|oga|ehn)\b|make i\b|i no go\b|no wahala\b`)

func voiceClean(text string) bool {
	return !voiceBadRe.MatchString(text)
}

// Number words are figures too. A drafted reply that writes
// "five thousand cedis" states the same fact as GH₵5,000 and
// must face the same gate; before this parser existed, the
// verifier read only digits, and a figure spelled out in
// words walked straight past it (the 2026-10-30 audit
// demonstrated it on both the reply gate and the haggle
// guard). The parser is bounded English: units, tens,
// hundred, thousand, million, with "and" inside the phrase.
var smallNumberWords = map[string]float64{
	"zero": 0, "one": 1, "two": 2, "three": 3, "four": 4,
	"five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9,
	"ten": 10, "eleven": 11, "twelve": 12, "thirteen": 13,
	"fourteen": 14, "fifteen": 15, "sixteen": 16,
	"seventeen": 17, "eighteen": 18, "nineteen": 19,
}

var tensNumberWords = map[string]float64{
	"twenty": 20, "thirty": 30, "forty": 40, "fifty": 50,
	"sixty": 60, "seventy": 70, "eighty": 80, "ninety": 90,
}

var scaleNumberWords = map[string]float64{
	"hundred": 100, "thousand": 1000, "million": 1000000,
}

func isNumberWord(w string) bool {
	if _, ok := smallNumberWords[w]; ok {
		return true
	}
	if _, ok := tensNumberWords[w]; ok {
		return true
	}
	_, ok := scaleNumberWords[w]
	return ok
}

// parseNumberPhrase reads one English number phrase
// starting at words[i], returning its value and how many
// words it consumed (0 when words[i] does not start one).
func parseNumberPhrase(words []string, i int) (float64, int) {
	total, current := 0.0, 0.0
	j := i
	started := false
	// English number order is part of the phrase's meaning:
	// tens may precede a unit ("fifty two"), never follow
	// one — "two fifty" is the unit-plus-fraction idiom
	// (GH₵2.50) or two figures, never fifty-two, and the
	// second audit caught the sum releasing a draft at a
	// figure no reader would take from it. An out-of-order
	// word ends the phrase, so each figure is verified as
	// itself.
	seenSmall, seenTens := false, false
	for j < len(words) {
		w := words[j]
		if v, ok := smallNumberWords[w]; ok {
			if seenSmall {
				break
			}
			seenSmall = true
			current += v
		} else if v, ok := tensNumberWords[w]; ok {
			if seenSmall || seenTens {
				break
			}
			seenTens = true
			current += v
		} else if w == "hundred" {
			seenSmall, seenTens = false, false
			if current == 0 {
				current = 1
			}
			current *= 100
		} else if w == "thousand" || w == "million" {
			if current == 0 {
				current = 1
			}
			total += current * scaleNumberWords[w]
			current = 0
			seenSmall, seenTens = false, false
		} else if w == "and" && started && j+1 < len(words) && isNumberWord(words[j+1]) {
			// "and" lives inside a phrase ("one hundred and
			// fifty") and nowhere else for our purposes.
		} else {
			break
		}
		started = true
		j++
	}
	if !started {
		return 0, 0
	}
	return total + current, j - i
}

// wordFigure is one spelled-out figure found in a text,
// with whether a currency word stands next to it.
type wordFigure struct {
	value            float64
	currencyAdjacent bool
}

var currencyWords = map[string]bool{
	"cedi": true, "cedis": true, "ghs": true, "gh₵": true, "₵": true,
	// The short forms the rest of the codebase already
	// treats as currency markers (photo, merchant and
	// burst paths): without them a spelled figure priced
	// in "ghc" skipped verification entirely (second
	// audit).
	"ghc": true, "gh": true,
}

// wordFigures finds every spelled-out number phrase in a
// text. Ordinary English uses small numbers constantly
// ("one of the bags"), so each phrase reports whether it
// reads as money; callers decide what follows (see
// figuresClean and the haggle guard). A phrase reads as
// money in two cases:
//
//   - a currency word stands beside it, within two
//     tokens ("five thousand cedis", "cedis fifty"): the
//     second audit showed a currency word two tokens off
//     left a sub-100 price wholly unverified;
//   - its sentence names a currency and carries no digit
//     figure at all: then the spelled figure is the
//     sentence's money, however far the currency word
//     stands — the fourth audit's escape was "fifty bags
//     of rice in cedis", four tokens apart.
//   - its sentence names a currency AND carries a digit
//     figure, and the spelled figure is not a plain
//     count: the fifth audit found the digit switched
//     the sentence rule off wholesale, so a spelled
//     price standing in a money position ("the delivery
//     is 50, and the cloth is fifty cedis in all") went
//     unverified. A plain count — a spelled figure
//     directly followed by a goods noun, with no money
//     word just before it ("two bags for 800 cedis") —
//     stays a count; anything else in a currency-naming
//     sentence is judged money and verified.
func wordFigures(text string) []wordFigure {
	var out []wordFigure
	sentences := strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == '!' || r == '?' || r == ';' || r == '\n'
	})
	for _, s := range sentences {
		out = append(out, wordFiguresIn(s)...)
	}
	return out
}

func wordFiguresIn(text string) []wordFigure {
	raw := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && r != '₵'
	})
	words := make([]string, 0, len(raw))
	for _, w := range raw {
		if w != "" {
			words = append(words, w)
		}
	}
	namesCurrency := false
	for _, w := range words {
		if currencyWords[w] {
			namesCurrency = true
		}
	}
	hasDigit := figureTokenRe.MatchString(text)
	// Money words mark a money position: a spelled figure
	// standing just after one ("is fifty", "charge fifty",
	// "fifty more to pay") is being used as money, not
	// counted as goods.
	moneyWords := map[string]bool{
		"is": true, "more": true, "charge": true, "charges": true,
		"fee": true, "costs": true, "cost": true, "total": true,
		"pay": true, "price": true,
	}
	var out []wordFigure
	for i := 0; i < len(words); {
		v, consumed := parseNumberPhrase(words, i)
		if consumed == 0 {
			i++
			continue
		}
		adjacent := false
		for _, j := range []int{i + consumed, i + consumed + 1, i + consumed + 2, i - 1, i - 2} {
			if j >= 0 && j < len(words) && currencyWords[words[j]] {
				adjacent = true
			}
		}
		if !adjacent && namesCurrency {
			if !hasDigit {
				// The run-4 sentence rule: with no digit
				// figure, the spelled figure is the
				// sentence's money.
				adjacent = true
			} else {
				// A digit figure stands in the sentence,
				// but it no longer switches the rule off
				// wholesale (the fifth audit). The
				// spelled figure is money unless it is a
				// plain count: directly followed by a
				// word that is neither a currency word
				// nor a money word (the goods noun), and
				// with no money word in the two tokens
				// before it.
				moneyBefore := false
				for _, j := range []int{i - 1, i - 2} {
					if j >= 0 && moneyWords[words[j]] {
						moneyBefore = true
					}
				}
				// A money word just after the following
				// token defeats the count reading too
				// ("five in total" — the sixth audit),
				// and "in" right after the figure cannot
				// establish a count: it names no goods.
				// ("Of" is deliberately not a marker:
				// "two of the large bags" is the
				// canonical count shape, held by the
				// fourth audit's control case.)
				moneyAfter := false
				for _, j := range []int{i + consumed + 1, i + consumed + 2} {
					if j < len(words) && moneyWords[words[j]] {
						moneyAfter = true
					}
				}
				plainCount := false
				if next := i + consumed; next < len(words) && !currencyWords[words[next]] && !moneyWords[words[next]] && !moneyBefore && !moneyAfter && words[next] != "in" {
					plainCount = true
				}
				adjacent = !plainCount
			}
		}
		out = append(out, wordFigure{value: v, currencyAdjacent: adjacent})
		i += consumed
	}
	return out
}

// productsStrung reports whether a piece of prose names
// two or more of the platform's products inside it. A
// listing is printed by the platform, one item per line
// at its price; a drafted reply that strings the goods
// along a sentence ("...such as the Shell Crossbody Bag
// at GH₵70, the Black Tote Bag at GH₵80 or...") is a list
// pretending to be talk, and it never sends: the draft
// gates reject it and the caller's factual line stands.
// Naming ONE product in talk is ordinary conversation
// and stays allowed.
func productsStrung(text string) bool {
	lower := strings.ToLower(text)
	found := 0
	for _, m := range catalogue.ListMerchants() {
		for _, p := range catalogue.ListProducts(m.ID) {
			name := strings.ToLower(strings.TrimSpace(p.Name))
			if len(name) >= 4 && strings.Contains(lower, name) {
				found++
				if found >= 2 {
					return true
				}
			}
		}
	}
	return false
}

func figuresClean(text string, allowed ...float64) bool {
	for _, wf := range wordFigures(text) {
		// A spelled figure is money when a currency word
		// stands beside it, or when it is large enough that
		// in a sales reply it can only be a price or a fee.
		// Small bare numbers are ordinary English ("one of
		// the bags") and are not held against the draft.
		if !wf.currencyAdjacent && wf.value < 100 {
			continue
		}
		ok := false
		for _, a := range allowed {
			if money.Round(wf.value*100) == money.Round(a*100) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, tok := range figureTokenRe.FindAllString(text, -1) {
		v, err := parseFigure(tok)
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

// figureNearCurrency reports whether a digit token at
// [start, end) in text carries a currency marker of its
// own: the marker attached directly before it ("GH₵42",
// "cedis 42") or directly after it ("42 cedis", "42 ghs"),
// with only spaces between. A marker belonging to the
// NEXT figure does not count — "size 42, comes to GH₵850"
// leaves the 42 bare.
func figureNearCurrency(text string, start, end int) bool {
	wordAt := func(s string) string {
		s = strings.TrimSpace(strings.ToLower(s))
		end := 0
		for _, r := range s {
			if (r >= 'a' && r <= 'z') || r == '₵' {
				end += len(string(r))
				continue
			}
			break
		}
		return s[:end]
	}
	if w := wordAt(text[end:]); currencyWords[w] {
		return true
	}
	before := strings.TrimSpace(text[:start])
	if strings.HasSuffix(before, "₵") {
		return true
	}
	if i := strings.LastIndexFunc(before, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '₵')
	}); i >= 0 {
		if currencyWords[strings.ToLower(before[i+1:])] {
			return true
		}
	} else if currencyWords[strings.ToLower(before)] {
		return true
	}
	return false
}

// recapFiguresClean verifies an order recap's figures
// with the units kept apart — the pooled figuresClean
// cannot: the third audit showed a numeric size in the
// shared pool licensing an invented fee of the same value
// ("size 42" admitting "GH₵42 delivery"). Money figures
// (currency-marked, or spelled at 100 and above) must be
// money facts of the order. A bare, unmarked digit token
// may instead be one of the order's non-money values —
// its quantities and sizes — because an honest recap
// names those too, in bare positions.
func recapFiguresClean(text string, moneyAllowed, bareAllowed []float64) bool {
	inSet := func(set []float64, v float64) bool {
		for _, a := range set {
			if money.Round(v*100) == money.Round(a*100) {
				return true
			}
		}
		return false
	}
	for _, wf := range wordFigures(text) {
		if !wf.currencyAdjacent && wf.value < 100 {
			continue
		}
		if !inSet(moneyAllowed, wf.value) {
			return false
		}
	}
	for _, m := range figureTokenRe.FindAllStringIndex(text, -1) {
		v, err := parseFigure(text[m[0]:m[1]])
		if err != nil {
			return false
		}
		if inSet(moneyAllowed, v) {
			continue
		}
		if inSet(bareAllowed, v) && !figureNearCurrency(text, m[0], m[1]) {
			continue
		}
		return false
	}
	return true
}

func shelfFacts(shopID string) (string, []float64) {
	var parts []string
	var figures []float64
	for _, p := range catalogue.ListProducts(shopID) {
		// The facts carry the shelf's stock as well as its
		// prices (the failure-cases pass): a merchant
		// asking "how much is left" — or a buyer asking
		// after a nearly-gone good — is owed the book's
		// own figure, and a model reasoning over
		// price-only facts can only decline. The stock
		// figure joins the verifiable figures, so a
		// drafted reply may state it and code can check
		// it like any price.
		stock := ""
		switch units := p.AvailableUnits(); {
		case units <= 0:
			stock = ", sold out"
		case p.ByMeasure():
			stock = ", " + formatQty(units) + " " + unitWord(p.Unit, units) + " in stock"
			figures = append(figures, units)
		default:
			stock = ", " + formatQty(units) + " in stock"
			figures = append(figures, units)
		}
		parts = append(parts, p.Name+" at "+money.GHS(p.ListPrice)+stock)
		figures = append(figures, p.ListPrice)
	}
	if len(parts) == 0 {
		return "the shelf is empty", figures
	}
	return strings.Join(parts, "; "), figures
}

const thinkSystem = `You are the thinking of a WhatsApp sales agent for a small shop in Ghana. A person is talking to the shop in their own words, the way people talk: fragments, Ghanaian English, Pidgin, Twi words, half sentences, whatever life has just done to the conversation. Before the shop answers, you think, the way the person behind the counter would: read what she means, look at her situation in the facts you are given, work out what she is asking for and what would actually help her, and name the act the shop should take.

You have freedom in understanding and in phrasing. You have no freedom to invent: never a figure, a product, a stock level, a time or a promise that is not in the facts. People must never be told "I don't understand" for a sentence a person would follow at once.

Three laws govern every reply the shop sends. First, think and answer: when the facts you are given hold the answer, give it. Never reply to a factual question with a question of your own, never ask the person to choose between readings the facts already settle, and when someone asks you to explain something, explain it. Second, every reply is polite and respectful: no impatience, no talking down, and no assumed titles, never Ma, Madam, Sir or Boss; address no one by a title at all. Third, language: people will write in Pidgin, broken English and mixed languages, and you understand all of it, but the shop always replies in correct English. Never copy a person's dialect back at them, and never write Pidgin yourself.

Reason in this order before you answer: first, what she is referring to (a product, an order, a price already named, something she was just shown or told); second, what she wants to happen; third, what the facts allow. Most wrong answers come from skipping the first step. When the facts say she is replying to a quoted message, that message — and who sent it, her or the shop — is what she is referring to: judge her words against the quoted message first, and against the current conversation second.

Answer with strict JSON only, no other text:
{"intent": "...", "item": "...", "reply": "...", "budget": 0}
"budget" is a number, not words: when she states what she can spend (her budget, an amount she has for this, a ceiling she names), give that figure there exactly as she stated it; otherwise 0.

The acts you can name:
- "payment_blocked": she tried to pay, or is trying, and could not: network trouble, the payment service not working, the bank or MoMo failing, no reception. Not a refusal to pay.
- "payment_expired": the payment link itself is dead: it has expired, it is closed, it will not open as a link, she asks for a NEW link because the old one is finished.
- "payment_later": she will pay, just later: tomorrow, in the evening, when money comes.
- "payment_want": she wants to pay now, or asks for the payment link: "send me the payment link", "I want to pay now", "let me pay", asking for the link again without saying it is dead.
- "payment_claim": she says she has paid, in her own words, whether or not she uses the word paid: "I just sent the money", "check your phone, I have done it", "I have made the payment". Payment here is only ever through the payment button the platform sent for the order: the platform verifies a payment on it with the payment service itself, and a payment she says went anywhere else is one the platform cannot see or confirm. Her word alone never settles anything, and no merchant is ever asked to confirm a payment.
- "item": she is asking about, or wants, a product on the shelf. Put the product's name, as it appears in the facts, in "item".
- "picture": she wants to SEE a product before she goes on: asking for its photo in any words, or saying she will not buy until she has seen it. Put the product's name in "item" when she names one; when she plainly means the one she was last looking at, leave "item" empty and the platform will use that one. A picture ask is never a generic question and never a buy: the picture is served first.
- "order_status": she is asking after an order she already made: where it stands, whether it is paid, what happens next with it.
- "order_sent": the shop owner is reporting that the goods for a paid order have gone out to the buyer: she has sent it, dispatched it, given it to the rider or the station, or made it ready for collection. Only the owner names this act; a buyer never does.
- "order_received": the buyer is confirming that the goods from her paid order have arrived and she has them: "it has come", "I have received it", "the parcel reached me". Not thanks on its own; she is reporting the arrival.
- "order_missing": the buyer is reporting that the goods from her paid order have NOT arrived: they never came, she is still waiting and it is overdue, the parcel is lost. Asking where an order is, without saying it has failed to arrive, is "order_status", not this.
- "order_dispute": the buyer is opening a dispute on a paid order: she says she was cheated or scammed, that the seller took her money and sent nothing, or she demands her money back through the platform for goods that never came. Stronger than "order_missing": not a delay being reported, a sale being called dishonest.
- "reorder": the buyer wants a past order AGAIN: the same items, the same details, bought once more. "Can I buy this again", "reorder the same", "I want the same thing I bought last time", quoting a completed order and asking to repeat it. She is not asking where that order stands (that is "order_status") and not starting a fresh browse; she is repeating a purchase.
- "recommend": she is asking the shop to CHOOSE for her: she describes a need, an occasion, a gift for someone, a purpose, a kind of thing she cannot name, and wants the shop's judgment on what fits. "I need something nice for my girlfriend", "what would you suggest for a wedding", "I am looking for a gift". She has not named the product she wants; when she names a product she wants, that is "item". Do not draft the recommendation yourself; the platform composes it from the shelf.
- "too_expensive": she is balking at the price of the thing she is looking at: it is too much for her, still expensive, more than she can do, WITHOUT naming a figure she would pay (a figure she names is an offer, handled by the haggle, not this act). The platform will put a cheaper suitable thing in front of her; do not draft it yourself.
- "order_payment": the shop owner is asking about the payment on an order: whether it has been paid, what is happening with it. When an open order is in the facts and she asks about "the payment" or "this order" without naming another, that is the order she means.
- "ask_buyer": the shop owner is telling you to ask her buyer something, or to find something out from the buyer: "ask the buyer", "ask her", "find out from her". You carry the question to the buyer yourself; never ask the owner what to ask when the facts show the open matter.
- "attention": the shop owner is asking what needs her, what is outstanding, what she should deal with today: "what needs my attention", "what is pending", "what should I do today", "anything for me to handle". The platform answers from her books — disputes, paid orders waiting to be sent, her unfinished work, orders waiting on buyers — so do not draft the list yourself.
- "new_listing": the shop owner is giving you a product to sell: describing goods she stocks, pasting an advert for a product, naming a product with its price or description so it can be listed. Goods copy meant for the shelf is this act, never a question; a statement about her shop, her orders or her day is not.
- "shelf": she wants to see what the shop has, or start again from the shelf.
- "basket_view": she is asking to see her basket: what she has picked, what she has already settled, what she is carrying in this order so far, in any words ("check basket", "what do I have so far?", "did we clear the basket?"). Do not draft it yourself; the platform lists it from her session.
- "courtesy": she is thanking the shop, expressing gratitude, or saying goodbye, most often right after a completed purchase. This is part of the shop's conversation, never off-topic. Draft the reply in "reply": short, warm, correct English. When the facts show her completed order, acknowledge it briefly by its reference or product. Never pitch the shelf, never list goods, never name a price in a courtesy reply.
- "ask": a question about the shop that the facts you are given can answer, or a request to explain something already said. Draft the answer in "reply": short, plain, warm, in correct English, using ONLY the facts given. An explanation is an answer, never a question back.
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
	b.WriteString("The shop is " + merchant.Name + ". Your name is Stall: when she asks your name or what to call you, the answer is Stall.\n")
	b.WriteString("On the shelf: " + shelf + ".\n")
	if merchant.DeliveryFee > 0 {
		b.WriteString("Delivery costs " + money.GHS(merchant.DeliveryFee) + ". " + merchant.DeliveryNote + "\n")
	} else {
		b.WriteString("The shop has no set delivery fee; delivery is arranged with the buyer.\n")
	}
	if s != nil && s.LastViewed != nil {
		b.WriteString("She was last looking at: " + s.LastViewed.Name + ".\n")
	}
	if s != nil {
		if fact := basketFactLine(s); fact != "" {
			b.WriteString(fact + "\n")
		}
	}
	if budget := budgetOf(from, s); budget > 0 {
		b.WriteString("Her budget for this conversation: " + money.GHS(budget) + ". Recommendations and alternatives respect it.\n")
	}
	if w := recentSpeakerWords(s, 4); w != "" {
		b.WriteString("What she said earlier in this chat, oldest first: " + w + ".\n")
	}
	b.WriteString(quoteFactLine(from))
	if order := orders.Default.NewestUnpaidFor(shopID, from); order != nil {
		b.WriteString("She has an open order waiting for payment: " + order.ProductName + ", " + money.GHS(order.Charged()) + ", reference " + order.Reference + ". Payment is only through the payment button Stall sent for it, and Stall confirms it itself.\n")
	} else if latest := orders.Default.NewestFor(shopID, from); latest != nil && latest.Status == "paid" {
		b.WriteString("Her latest order (" + latest.Reference + ", " + latest.ProductName + ") is paid.")
		switch latest.DeliveryState {
		case "sent":
			b.WriteString(" The seller has marked it sent.\n")
		case "received":
			b.WriteString(" She has already confirmed it reached her.\n")
		case "missing":
			b.WriteString(" She has already told the shop it has not arrived.\n")
		default:
			b.WriteString("\n")
		}
	} else {
		b.WriteString("She has no open order waiting for payment.\n")
	}
	b.WriteString("\nHer message: " + msg)
	sys := thinkSystem
	out := model.Chat([]model.Message{{Role: "user", Content: b.String()}}, model.Options{System: &sys, Turn: turnTokenFor(from)})
	u := parseUnderstood(out)
	if u != nil {
		u.DirectRoute = directWordsRe.MatchString(strings.ToLower(msg))
		u.Words = msg
	}
	return u
}

// merchantDayFigures returns the merchant's day as a fact
// line for the thinking prompt, plus the day's figures a
// drafted reply is allowed to use.
// merchantDayFigures returns the day report with its
// figures SPLIT by kind — money values and bare counts —
// because the gates that verify a reply against them
// must not pool the two (the fifth audit: pooled, a sale
// count of 850 licensed a drafted "GH₵850" the day never
// saw; a count is not money and must never verify as
// one). Callers verify currency-marked figures against
// the money set and bare figures against either, the
// recap gate's shape.
func merchantDayFigures(merchant *catalogue.Merchant, shopID string) (string, []float64, []float64) {
	day := daybook.Default.GetDayState(shopID, daybook.Options{Allowance: merchant.DailyGiveawayAllowance})
	left := day.Allowance - day.GivenAway
	if left < 0 {
		left = 0
	}
	return insights.DayReportText(day),
		[]float64{day.Revenue, day.GivenAway, day.Allowance, left},
		[]float64{float64(day.Sales), float64(day.WalkAways)}
}

// understandMerchantMessage asks the model what the merchant
// herself means, with her own facts: her shelf, her day, her
// allowance, and the live state of her orders. The orders are
// what her payment questions are about; a thinking step that
// cannot see them can only deflect, and deflection is what
// this step replaced.
func understandMerchantMessage(from string, merchant *catalogue.Merchant, shopID, msg string) *understood {
	shelf, _ := shelfFacts(shopID)
	dayText, _, _ := merchantDayFigures(merchant, shopID)
	var b strings.Builder
	b.WriteString("The person writing is the OWNER of the shop " + merchant.Name + ", talking to her own sales agent.\n")
	b.WriteString("Your name is Stall. When she asks your name or what to call you, the answer is Stall.\n")
	b.WriteString("On her shelf: " + shelf + ".\n")
	if ms, ok := sessions.lookup(sessionKey(shopID, from)); ok {
		if w := recentSpeakerWords(ms, 4); w != "" {
			b.WriteString("What she said earlier in this chat, oldest first: " + w + ".\n")
		}
	}
	b.WriteString(quoteFactLine(from))
	b.WriteString("Her day so far: " + dayText + "\n")
	b.WriteString("Her daily giveaway allowance is the amount she allows the agent to give away in discounts across a day; every discount agreed comes out of it, and the agent haggles firmer as it runs down. It stands at " + money.GHS(merchant.DailyGiveawayAllowance) + ".\n")
	if !merchant.AllowanceSet {
		b.WriteString("She has not set an allowance yet, so the agent has nothing to give away when buyers bargain. Setting one is done by saying: set allowance 300.\n")
	} else if merchant.DailyGiveawayAllowance <= 0 {
		b.WriteString("Her allowance is GH₵0 by her own choice: she gives nothing away when buyers bargain, and it is settled — never raise it with her as if it were missing.\n")
	}
	for _, o := range paidOrdersOf(shopID) {
		switch o.DeliveryState {
		case "missing":
			b.WriteString("A buyer says her order has not arrived and it needs her attention: " + o.Reference + ", the " + o.ProductName + ", buyer " + o.From + ".\n")
		case "":
			b.WriteString("A paid order is waiting to be sent: " + o.Reference + ", the " + o.ProductName + ", buyer " + o.From + ". When she says she has sent it, that is the order she means.\n")
		}
	}
	open := 0
	for _, o := range orders.Default.All() {
		if o.MerchantID != shopID || o.Status != "pending" {
			continue
		}
		open++
		if open <= 3 {
			b.WriteString("An order is still open and unpaid: " + o.Reference + ", the " + o.ProductName + " at " + money.GHS(o.Charged()) + ", buyer " + o.From + ".\n")
		}
	}
	if focus := focusOrder(shopID); focus != nil {
		b.WriteString("The order in focus, the one she means when she says \"this order\" or asks about \"the payment\" without naming another, is " + focus.Reference + " (the " + focus.ProductName + ", " + money.GHS(focus.Charged()) + ", buyer " + focus.From + ", status " + focus.Status + ").\n")
	}
	if key := sessionKey(shopID, from); sessions.get(key) != nil && sessions.get(key).LastMerchantNote != "" {
		b.WriteString("The last thing the agent told her, which an \"explain this\" from her most likely points at: \"" + sessions.get(key).LastMerchantNote + "\" If she asks to explain, explain THAT, in plain words, from the facts.\n")
	}
	b.WriteString("Things she can ask the agent to do: show her catalogue, change a price or a last price, set her daily giveaway allowance, remove a product, add a product by describing it or sending a photo, tell her how today is going, report on the payment of an order, ask a buyer a question on her behalf, and tell her what needs her attention.\n")
	b.WriteString("Use \"order_payment\" when she is asking about the payment on an order, \"ask_buyer\" when she is telling the agent to ask her buyer something, and \"attention\" when she is asking what needs her or what is outstanding. Use \"ask\" for any other question her shelf, her day, her allowance or those abilities can answer, drafting the reply from the facts only.\n")
	b.WriteString("\nThe owner's message: " + msg)
	sys := thinkSystem
	out := model.Chat([]model.Message{{Role: "user", Content: b.String()}}, model.Options{System: &sys, Turn: turnTokenFor(from)})
	return parseUnderstood(out)
}

// focusOrder is the order a conversation is about when none
// is named: the newest open order first, then the newest
// order of any kind.
func focusOrder(shopID string) *orders.Order {
	var newestOpen, newestAny *orders.Order
	for _, o := range orders.Default.All() {
		if o.MerchantID != shopID {
			continue
		}
		oo := o
		if newestAny == nil || oo.Reference > newestAny.Reference {
			newestAny = &oo
		}
		if oo.Unpaid() && (newestOpen == nil || oo.Reference > newestOpen.Reference) {
			newestOpen = &oo
		}
	}
	if newestOpen != nil {
		return newestOpen
	}
	return newestAny
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
		intentItem, intentPicture, intentOrderStatus, intentOrderSent, intentOrderReceived, intentOrderMissing, intentOrderDispute, intentReorder, intentRecommend, intentTooExpensive, intentOrderPayment, intentAskBuyer, intentAttention, intentNewListing, intentShelf, intentBasketView, intentCourtesy, intentAsk, intentOffTopic, intentUnclear:
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
		laneStrike(from, s, merchant, u.Words)
		return true
	case intentCourtesy:
		// Thanks is a closing, not a new sale: the reply is
		// warm and brief, acknowledges her completed order
		// when she has one, and never pitches the shelf at a
		// person who has just bought. A draft that carries a
		// figure or a pitch is not a courtesy; the plain line
		// stands in for it.
		reply := strings.TrimSpace(u.Reply)
		if reply == "" || !voiceClean(reply) || !figuresClean(reply) || productsStrung(reply) ||
			strings.Contains(strings.ToLower(reply), "here is what we have") {
			reply = courtesyLine(shopID, from, merchant)
		}
		whatsapp.SendText(from, reply)
		return true
	case intentShelf:
		whatsapp.SendText(from, welcomeFor(merchant, from))
		presentShelf(from, merchant)
		return true
	case intentBasketView:
		serveBasketView(from, s, shopID)
		return true
	case intentOrderStatus:
		o := orders.Default.NewestFor(shopID, from)
		if qo := quotedOrderFor(shopID, from); qo != nil {
			o = qo
		}
		if o == nil {
			return false
		}
		whatsapp.SendText(from, orderReadback(merchant, o))
		return true
	case intentOrderReceived:
		return buyerConfirmReceived(from, merchant, shopID, u.Words)
	case intentOrderMissing:
		return buyerReportMissing(from, merchant, shopID, u.Words)
	case intentOrderDispute:
		order := disputeTarget(shopID, from)
		if order == nil {
			return false
		}
		openDispute(from, merchant, order, u.Words)
		return true
	case intentReorder:
		return startReorder(from, s, merchant, shopID, u.Words)
	case intentRecommend:
		return recommendInShop(from, s, merchant, shopID, u.Words)
	case intentTooExpensive:
		current := s.LastViewed
		if current == nil {
			current = s.Product
		}
		return offerCheaperAlternative(from, s, merchant, shopID, current, u.Words)
	case intentItem:
		p := catalogue.FindProduct(shopID, strings.ToLower(u.Item))
		if p == nil {
			p = quotedProductIn(shopID, from)
		}
		if p == nil {
			return false
		}
		if p.AvailableUnits() <= 0 {
			// "Still available." is printed by code, so
			// code owns its truth (the seventh audit): a
			// sold-out product is answered honestly, with
			// no priced view and no place in her view.
			whatsapp.SendText(from, "That one has sold out just now.")
			return true
		}
		if p.ByMeasure() {
			// A quantity stated in her own words is a
			// structural fact, not a reading (the
			// failure-cases pass): "I need 3 yards of the
			// ankara" read as a plain item answers "Still
			// available." and the measure she stated is
			// lost. When her words carry a quantity in
			// the product's own unit, the item act
			// declines and the measure road below owns
			// the turn: it quotes the cut, haggles the
			// cut, refuses what is not there.
			if q, ok := parseQuantity(strings.ToLower(u.Words)); ok && q.Unit == p.Unit {
				return false
			}
		}
		s.LastViewed = p
		if s.Stage == "agreed" {
			// The agreed shell sheds its sale identity
			// with its stage (the sixth audit), as on the
			// browsing road.
			s.Stage = "browsing"
			s.SaleID = ""
			s.PaymentURL = ""
		}
		whatsapp.SendText(from, "Still available.")
		sendProductView(from, p, " Want to make an offer?")
		return true
	case intentPicture:
		// The picture is served before anything is sold:
		// the product she named, or the one she was just
		// looking at.
		var p *catalogue.Product
		if strings.TrimSpace(u.Item) != "" {
			p = catalogue.FindProduct(shopID, strings.ToLower(u.Item))
		}
		if p == nil {
			p = quotedProductIn(shopID, from)
		}
		if p == nil {
			p = s.LastViewed
		}
		if p == nil {
			return false
		}
		if p.AvailableUnits() <= 0 {
			// No photos, view, merchant photo solicitation
			// or LastViewed for goods that cannot be
			// bought (the seventh audit).
			whatsapp.SendText(from, "The "+p.Name+" has sold out just now, so it is not available to buy.")
			return true
		}
		s.LastViewed = p
		if sendProductPhotos(from, shopID, p) {
			sendProductView(from, p, " Make me an offer and let us see.")
			return true
		}
		answerPictureAsk(from, merchant, p)
		sendProductViewLead(from, "It is this: "+p.Description, p, " Make me an offer and let us see.")
		return true
	case intentPaymentClaim:
		order := orders.Default.NewestUnpaidFor(shopID, from)
		if qo := quotedUnpaidOrder(shopID, from); qo != nil {
			order = qo
		}
		if order == nil {
			return false
		}
		handlePaidReport(from, merchant, order, u.DirectRoute)
		return true
	case intentPaymentBlocked, intentPaymentExpired, intentPaymentLater, intentPaymentWant:
		order := orders.Default.NewestUnpaidFor(shopID, from)
		if qo := quotedUnpaidOrder(shopID, from); qo != nil {
			order = qo
		}
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
		if !figuresClean(u.Reply, figures...) || !voiceClean(u.Reply) || productsStrung(u.Reply) {
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
	sendPieces(from, "Of course. Here is the payment link for your order "+order.Reference+" ("+order.ProductName+", "+money.GHS(order.Charged())+").")
	sendPayButton(from, order.Charged(), url)
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
	sendPieces(from, pieces...)
	if phone := merchantNotifyPhone(merchant); phone != "" {
		whatsapp.SendText(phone, "Payment trouble\n\n*"+order.ProductName+"*\nBuyer "+from+" says she could not complete the payment for order "+order.Reference+" ("+money.GHS(order.Charged())+"): the network or the payment service at her end.\n\nThe order is still open. She has the link.")
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
			"Your order "+order.Reference+" for the "+order.ProductName+" at "+money.GHS(order.Charged())+" is still open, and the payment button is the way to complete it. If it keeps failing, tell me what it says and I will look into it with the payment service.",
		)
		return true
	}
	link := checkout.CreatePaymentLink(checkout.LinkRequest{
		Amount:         order.Charged(),
		Reference:      order.Reference + "-" + strconv.Itoa(attempts+1),
		IdempotencyKey: checkout.FreshKey(order.Reference),
		RecipientName:  order.PackageName,
		Subaccount:     subaccountFor(merchant),
	})
	if link.URL == nil {
		whatsapp.SendText(from, "The link is not coming up right now, even a fresh one. The shop has been told, and your order "+order.Reference+" is still open.")
		if phone := merchantNotifyPhone(merchant); phone != "" {
			whatsapp.SendText(phone, "A fresh payment link could not be made for order "+order.Reference+" ("+order.ProductName+", "+money.GHS(order.Charged())+"). Buyer "+from+" is waiting on it. Please reach out to her here.")
		}
		return true
	}
	newRef := link.ProviderRef
	// The mint above can take seconds, and the order can
	// move inside that window — the webhook settles it,
	// the merchant cancels it. The third audit found this
	// write assigning Status='pending' unconditionally
	// from the pre-mint copy, reopening an order that had
	// already ended. Transition only a stored order that
	// is still open, rebind the session's nature with its
	// reference, and if the order moved on, say so from
	// the stored record instead of sending a live button
	// for an order that no longer wants one.
	applied := false
	orders.Default.Update(order.Reference, func(o *orders.Order) {
		if o.Status != "pending" && o.Status != "link_failed" {
			return
		}
		applied = true
		// The session attributes follow the successful
		// mint unconditionally (the fifth audit): a
		// dry-run mint carries ProviderRef nil and
		// DryRun true by construction, and the old guard
		// skipped the write exactly then — leaving the
		// order bound to its old REAL provider session
		// with the flag unset, a session the sweep and
		// the webhook would keep verifying against an
		// order the re-issue believed was dry.
		o.ProviderRef = newRef
		o.DryRun = link.DryRun
		o.LinkAttempts = attempts + 1
		// A live session exists again: an order whose link
		// never came up goes back to plain pending, the
		// state every reader expects of an order with a
		// working link.
		o.Status = "pending"
	})
	if !applied {
		stored := orders.Default.GetOrder(order.Reference)
		if stored != nil && stored.Status == "paid" {
			whatsapp.SendText(from, "Good news: your order "+stored.Reference+" has already been paid and confirmed, so there is nothing more to pay on it.")
		} else {
			whatsapp.SendText(from, "That order, "+order.Reference+", has ended, so there is nothing to pay on it. If you still want the item, tell me and we will start it fresh.")
		}
		return true
	}
	if s != nil {
		s.PaymentURL = *link.URL
	}
	sendPieces(from, "That link has expired, so I have made you a fresh one. The old one will not work now.")
	sendPayButton(from, order.Charged(), *link.URL)
	return true
}

// actOnMerchantUnderstanding carries out what the merchant
// was understood to mean: the payment state of her order
// answered from the book and a live check, her instruction
// to ask the buyer carried out, or a question the facts
// answer. It never performs a change to her shop: changes
// still go through her plain commands, so nothing about her
// catalogue moves on a model's reading alone.
func actOnMerchantUnderstanding(from string, merchant *catalogue.Merchant, shopID, msg string, u *understood) bool {
	switch u.Intent {
	case intentOrderSent:
		return merchantMarkSent(from, merchant, shopID, msg)
	case intentOrderPayment:
		return merchantOrderPayment(from, merchant, shopID, msg)
	case intentAskBuyer:
		return merchantAskBuyer(from, merchant, shopID, u)
	case intentCourtesy:
		// A courtesy from the owner — thanks, a laugh, a
		// goodbye — is answered the way the buyer's is: her
		// drafted reply, figure- and voice-guarded. Without
		// this case her pleasantry fell through every door
		// into the day answer, and a laugh got a financial
		// report for an answer.
		if strings.TrimSpace(u.Reply) == "" || !figuresClean(u.Reply) || !voiceClean(u.Reply) || productsStrung(u.Reply) {
			return false
		}
		sendPieces(from, u.Reply)
		return true
	case intentAttention:
		whatsapp.SendText(from, merchantAttentionText(merchant, sessions.get(sessionKey(shopID, from))))
		return true
	case intentAsk, intentUnclear:
		if strings.TrimSpace(u.Reply) == "" {
			return false
		}
		_, figures := shelfFacts(shopID)
		_, dayMoney, dayCounts := merchantDayFigures(merchant, shopID)
		figures = append(figures, dayMoney...)
		if focus := focusOrder(shopID); focus != nil {
			figures = append(figures, focus.Charged(), focus.Price)
		}
		figures = append(figures, merchant.DailyGiveawayAllowance)
		// The figures in the agent's own last note to her are
		// facts too: an explanation of the allowance prompt
		// naturally repeats its example ("set allowance 300"),
		// and a guard that rejects the agent's own words makes
		// the explanation unsendable.
		if s, ok := sessions.lookup(sessionKey(shopID, from)); ok && s.LastMerchantNote != "" {
			for _, tok := range figureTokenRe.FindAllString(s.LastMerchantNote, -1) {
				if v, err := parseFigure(tok); err == nil {
					figures = append(figures, v)
				}
			}
		}
		// The day counts ride the BARE pool only (the
		// fifth audit): pooled with the money, a count
		// licensed a currency figure the books never saw.
		// recapFiguresClean checks currency-marked
		// figures against the money alone and bare
		// figures against money or counts.
		if !recapFiguresClean(u.Reply, figures, dayCounts) || !voiceClean(u.Reply) || productsStrung(u.Reply) {
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
const merchantHelpText = "I did not catch that one. I can show your catalogue, add a product when you describe it or send its photo, change a price or a last price, set your daily allowance, set or change your payout details, tell you how today is going, and tell you what needs your attention. Say \"my catalogue\", \"payout details\", or ask me what needs your attention."

// courtesyRe reads a closing pleasantry in the no-model net:
// thanks and goodbyes are part of the shop's conversation and
// are never steered as off-topic.
var courtesyRe = regexp.MustCompile(`^(thank|thanks|thank you|bye|goodbye|great|perfect|lovely|nice one|well done|ok thanks|thank u)\b`)

// courtesyLine is the plain closing the net answers a
// pleasantry with: warm, brief, and grounded in her completed
// order when she has one.
func courtesyLine(shopID, from string, merchant *catalogue.Merchant) string {
	if latest := orders.Default.NewestFor(shopID, from); latest != nil && latest.Status == "paid" {
		return "You are welcome. Your order " + latest.Reference + " is with " + merchant.Name + "."
	}
	return "You are welcome."
}
