package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// The intent gate. A buyer's turn is read for what she
// actually wants before any stage of the sale is allowed to
// read it as an answer. Two of her intents outrank every open
// question, because no Ghanaian buyer pays for goods she has
// not seen, and a sale she has called off is off:
//
//   - A picture ask is served as a picture ask wherever it
//     lands (browsing, haggling, fulfilment, the package
//     questions): the product's own photo goes out, or, when
//     the chat holds none, she is told plainly and the owner
//     is asked for it. It is never consumed as a name, a
//     place or a note, and the sale does not move while she
//     waits to see the goods.
//   - A cancellation ends the pending sale wherever it
//     lands, in her own words for it ("forget it", "leave
//     it"), never read as a note or a name.
//
// The gate runs inside the ordinary turn lock, ahead of the
// step machines, which is what enforces it: a reply is only
// ever composed after the intent has had its say.

// The stage intents a pending sale can be met with, as the
// thinking names them.
const (
	stageIntentAnswer  = "answer"
	stageIntentPicture = "picture"
	stageIntentCancel  = "cancel"
	stageIntentConfirm = "confirm"
	stageIntentAsk     = "question"
	stageIntentChange  = "change"
	stageIntentOther   = "other"
)

// stageIntentCharter briefs the thinking for one turn of a
// pending sale. The judgement is the model's, made from the
// whole situation (what was asked, what the sale is, what
// she says), never from a word list in code: the same words
// are an answer in one place and a call-off in another, and
// only the meaning tells them apart. Code enforces the stop
// list after the judgement: a picture or a cancellation is
// served before the open question may read her words again.
const stageIntentCharter = `You are the intent reader for a pending sale in a WhatsApp shop in Ghana. A price has been agreed and the agent has asked the buyer ONE open question (about how the goods reach her, the name for the package, a message for the seller, or a final confirm). Before the shop may treat her reply as an answer, you read what she actually means in this situation.

Think from the whole context, never from isolated words. The same phrase can be an answer, an instruction for the order, or the end of the sale, depending on what was asked. Judge what she is trying to accomplish with this message.

Reason in this order: first, what the open question is asking for; second, what she is referring to (including anything she said earlier in this chat); third, whether her message answers that question, replaces it with another need, or ends the sale. Then give the intent.

Answer with strict JSON only: {"intent": "...", "reply": "..."} — "reply" is used only for the question intent and stays empty otherwise.

The intents:
- "picture": she wants to SEE the product before she goes on: asking for a photo in any words, saying she will not buy or pay until she has seen it, asking what it looks like. This overrides the open question completely: she is not answering it.
- "cancel": she is ending this pending purchase: calling it off, dropping the whole thing, changing her mind about buying. Judge by the situation: at the seller-message question, declining the optional message ("I don't want a message", "no note") is an ANSWER, not a cancel; a note telling the seller where to leave the goods ("leave it with the security man") is an ANSWER, not a cancel.
- "confirm": only at the final confirm question: she agrees to the order as read back, or tells the agent to go ahead and send the payment link.
- "answer": she is answering the open question, in whatever shape the answer naturally takes (a place, a name, a choice, a note, a plain no to an optional thing).
- "question": she is asking something about the sale (the total, delivery, the product) and expects an answer before she goes on. Draft the answer in "reply", from the facts only, short and plain; the platform verifies every figure in it before sending.
- "change": only at the final confirm question: she wants something in the order corrected.
- "other": none of these fits.`

// stageReading is what the thinking made of one reply
// inside a pending sale: the intent, and, for a question,
// the answer it drafted from the facts.
type stageReading struct {
	Intent string
	Reply  string
}

// thinkStageIntent asks the thinking what a buyer's reply
// inside a pending sale means, against the open question
// she was asked. It returns an empty reading when no model
// can be read, and the caller then keeps its older
// behaviour.
func thinkStageIntent(from string, s *Session, merchant *catalogue.Merchant, msg string) stageReading {
	if s == nil || s.Product == nil || merchant == nil {
		return stageReading{}
	}
	extra := ""
	if s.FulfilStep == "confirm" {
		f := s.PendingFulfil
		extra = "The order she is confirming: " + f.Mode + ", package name " + f.PackageName + ", note " + f.Note + "."
	}
	return thinkStageIntentQ(from, s, merchant, msg, fulfilPrompt(s), s.AgreedPrice, extra)
}

// thinkStageIntentQ is the stage read against a stated
// open question and price, so every board that holds a
// question open over a buyer (the fulfilment steps, the
// pending close) is read the same way.
func thinkStageIntentQ(from string, s *Session, merchant *catalogue.Merchant, msg, question string, price float64, extra string) stageReading {
	if s == nil || s.Product == nil || merchant == nil {
		return stageReading{}
	}
	var b strings.Builder
	b.WriteString("The shop is " + merchant.Name + ". The pending sale is the " + s.Product.Name + " (" + s.Product.Description + ") at " + money.GHS(price) + "; its list price is " + money.GHS(s.Product.ListPrice) + ".\n")
	if merchant.DeliveryFee > 0 {
		b.WriteString("Delivery costs " + money.GHS(merchant.DeliveryFee) + ". " + merchant.DeliveryNote + "\n")
	} else {
		b.WriteString("The shop has no set delivery fee; delivery is arranged with the seller.\n")
	}
	if w := recentSpeakerWords(s, 4); w != "" {
		b.WriteString("What she said earlier in this chat, oldest first: " + w + ".\n")
	}
	b.WriteString("The open question she was just asked: \"" + question + "\"\n")
	if extra != "" {
		b.WriteString(extra + "\n")
	}
	b.WriteString("Her message: \"" + msg + "\"")
	sys := stageIntentCharter
	timeout := 10000
	out := model.Chat([]model.Message{{Role: "user", Content: b.String()}}, model.Options{System: &sys, TimeoutMs: &timeout})
	if out == nil {
		return stageReading{}
	}
	text := *out
	open := strings.Index(text, "{")
	closeIdx := strings.LastIndex(text, "}")
	if open == -1 || closeIdx <= open {
		return stageReading{}
	}
	var raw struct {
		Intent string `json:"intent"`
		Reply  string `json:"reply"`
	}
	if err := json.Unmarshal([]byte(text[open:closeIdx+1]), &raw); err != nil {
		return stageReading{}
	}
	switch raw.Intent {
	case stageIntentAnswer, stageIntentPicture, stageIntentCancel, stageIntentConfirm, stageIntentAsk, stageIntentChange, stageIntentOther:
		return stageReading{Intent: raw.Intent, Reply: strings.TrimSpace(raw.Reply)}
	}
	return stageReading{}
}

// picRequest is one buyer waiting on one product photo the
// chat does not hold yet, keyed by shop.
type picRequest struct {
	Buyer     string
	ProductID string
	At        time.Time
}

var picRequests = map[string]picRequest{}

// answerPictureAsk serves a picture ask for one product:
// the kept photos go out when the product has them; when it
// has none she hears the plain truth and the owner is asked
// to send one, with her wait remembered here so the owner's
// photo, when it comes, is passed straight to her.
func answerPictureAsk(from string, merchant *catalogue.Merchant, p *catalogue.Product) {
	if p == nil || merchant == nil {
		return
	}
	if sendProductPhotos(from, p) {
		return
	}
	whatsapp.SendText(from, "I don't have the pic of the "+p.Name+" in this chat. Let me get it from the shop owner for you.")
	picRequests[merchant.ID] = picRequest{Buyer: from, ProductID: p.ID, At: time.Now()}
	whatsapp.SendText(merchant.OwnerWhatsApp, "A buyer is asking for a picture of "+p.Name+". Send the photo here and I will pass it to her.")
}

// servePendingPicRequest answers a merchant's photo when a
// buyer is waiting on one from her shop: the photo is kept
// on the product for the next buyer and passed to the
// waiting buyer at once, and the photo opens no listing.
// It reports whether the photo was claimed this way.
func servePendingPicRequest(from string, shop *catalogue.Merchant, imageID string) bool {
	req, ok := picRequests[shop.ID]
	if !ok || time.Since(req.At) > 24*time.Hour {
		return false
	}
	p := catalogue.FindByID(shop.ID, req.ProductID)
	if p == nil {
		delete(picRequests, shop.ID)
		return false
	}
	data, mime, ok := downloadMediaBytesFn(imageID)
	if !ok || len(data) == 0 {
		whatsapp.SendText(from, "I could not fetch that photo. Please send it once more.")
		return true
	}
	_ = os.MkdirAll(datadir.Path("photos"), 0o755)
	ext := ".jpg"
	switch {
	case strings.Contains(mime, "png"):
		ext = ".png"
	case strings.Contains(mime, "webp"):
		ext = ".webp"
	}
	rel := filepath.Join("photos", p.ID+"-"+strconv.Itoa(len(p.Photos)+1)+ext)
	if err := os.WriteFile(datadir.Path(rel), data, 0o644); err != nil {
		whatsapp.SendText(from, "I could not fetch that photo. Please send it once more.")
		return true
	}
	photos := append(append([]string{}, p.Photos...), rel)
	_ = catalogue.SetProductPhotos(shop.ID, p.ID, photos)
	p.Photos = photos
	delete(picRequests, shop.ID)
	if !sendProductPhotos(req.Buyer, p) {
		whatsapp.SendText(req.Buyer, "The shop owner has sent the picture of the "+p.Name+", but I could not pass it on just now. Ask me for the pic once more and I will send it.")
	}
	whatsapp.SendText(from, "Thank you. I have sent it to the buyer, and I have kept it on the "+p.Name+" for the next buyer who asks.")
	return true
}

// sellerWalkCharter briefs the thinking for one turn of a
// seller's own guided walk (listing a product, opening her
// shop). She is the domain expert on her goods and a
// beginner on this platform, in that order: when she asks
// what a step means, she is answered plainly and the step
// waits; her answer is never consumed by a question she was
// asking. The judgement is the model's, from the situation,
// exactly as on the buyer's side.
const sellerWalkCharter = `You are the intent reader for a seller setting up her shop in a WhatsApp chat in Ghana. The agent has asked her ONE open question in a guided walk, and before the walk may treat her reply as its answer, you read what she actually means.

Reason in this order: first, what the open question is asking for; second, what she is referring to (including anything she said earlier in this chat); third, whether her message answers that question, asks something about it, or leaves the walk. Then give the intent.

Answer with strict JSON only: {"intent": "...", "reply": "..."} — "reply" is used only for the question intent and stays empty otherwise.

The intents:
- "answer": she is answering the open question, in whatever shape the answer naturally takes.
- "question": she is asking what something means, why it is needed, or how this works, and expects an explanation before she goes on. Draft the explanation in "reply": plain, respectful, correct English, from the facts and meanings given, never a title (never Ma, Madam, Sir or Boss). Short: two or three sentences at most.
- "cancel": she is leaving the walk: stopping, postponing it, saying she will do it later or not at all.
- "other": none of these fits.

Meanings she may ask about: her list price is the price customers see first. Her last price is the lowest she will accept; the agent bargains between the two and never goes below her last price, and customers never see it. A daily giveaway allowance is the total discount she lets the agent give away across a day. Her shop code is the address customers use to reach her shop on this number. The holder name is the name on the MoMo account her sales money is paid into; it must match the account itself, or the money cannot land. A product's measure is how it is sold: as a whole piece, or by the yard, litre or kilo.`

// thinkSellerWalkIntent asks the thinking what a seller's
// reply inside her own guided walk means, against the open
// question she was asked.
func thinkSellerWalkIntent(from string, s *Session, walk, question, given, msg string, figures []float64) stageReading {
	var b strings.Builder
	b.WriteString("The walk: " + walk + ".\n")
	if given != "" {
		b.WriteString("What she has given so far: " + given + ".\n")
	}
	if w := recentSpeakerWords(s, 4); w != "" {
		b.WriteString("What she said earlier in this chat, oldest first: " + w + ".\n")
	}
	b.WriteString("The open question she was just asked: \"" + question + "\"\n")
	b.WriteString("Her message: \"" + msg + "\"")
	sys := sellerWalkCharter
	timeout := 8000
	out := model.Chat([]model.Message{{Role: "user", Content: b.String()}}, model.Options{System: &sys, TimeoutMs: &timeout})
	if out == nil {
		return stageReading{}
	}
	text := *out
	open := strings.Index(text, "{")
	closeIdx := strings.LastIndex(text, "}")
	if open == -1 || closeIdx <= open {
		return stageReading{}
	}
	var raw struct {
		Intent string `json:"intent"`
		Reply  string `json:"reply"`
	}
	if err := json.Unmarshal([]byte(text[open:closeIdx+1]), &raw); err != nil {
		return stageReading{}
	}
	switch raw.Intent {
	case stageIntentAnswer, stageIntentAsk, stageIntentCancel, stageIntentOther:
		return stageReading{Intent: raw.Intent, Reply: strings.TrimSpace(raw.Reply)}
	}
	return stageReading{}
}

// serveSellerWalkTurn reads one onboarding turn before the
// walk may consume it. It serves a question (answered, the
// step re-asked) and a call-off (the walk dropped, as her
// own cancel word drops it), and reports whether it served
// the turn. Steps whose questions the walk asks in its own
// specialised shapes (the intake gate, the duplicate
// choice, the piece size) keep their own handling and are
// not read here.
func serveSellerWalkTurn(from string, s *Session, merchant *catalogue.Merchant, text, lower string) bool {
	flow := s.Onboarding
	if flow == nil || controlWord(lower) == "cancel" {
		return false
	}
	question := onboardingQuestionText(flow)
	if question == "" {
		return false
	}
	var given []string
	if flow.Name != "" {
		given = append(given, "name "+flow.Name)
	}
	if flow.ListPrice > 0 {
		given = append(given, "list price "+money.GHS(flow.ListPrice))
	}
	if flow.FloorPrice > 0 {
		given = append(given, "last price "+money.GHS(flow.FloorPrice))
	}
	reading := thinkSellerWalkIntent(from, s, "listing a product for her shop", question, strings.Join(given, "; "), text,
		[]float64{flow.ListPrice, flow.FloorPrice, merchant.DailyGiveawayAllowance})
	switch reading.Intent {
	case stageIntentCancel:
		s.Onboarding = nil
		whatsapp.SendText(from, "Dropped it. Nothing was saved. Say \"add a product\" whenever you are ready.")
		return true
	case stageIntentAsk:
		if reading.Reply != "" && voiceClean(reading.Reply) && figuresClean(reading.Reply, []float64{flow.ListPrice, flow.FloorPrice, merchant.DailyGiveawayAllowance}...) {
			whatsapp.SendText(from, reading.Reply)
			askOnboardingStep(from, flow)
			return true
		}
	}
	return false
}
