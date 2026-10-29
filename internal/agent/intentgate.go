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

Answer with strict JSON only: {"intent": "..."}

The intents:
- "picture": she wants to SEE the product before she goes on: asking for a photo in any words, saying she will not buy or pay until she has seen it, asking what it looks like. This overrides the open question completely: she is not answering it.
- "cancel": she is ending this pending purchase: calling it off, dropping the whole thing, changing her mind about buying. Judge by the situation: at the seller-message question, declining the optional message ("I don't want a message", "no note") is an ANSWER, not a cancel; a note telling the seller where to leave the goods ("leave it with the security man") is an ANSWER, not a cancel.
- "confirm": only at the final confirm question: she agrees to the order as read back, or tells the agent to go ahead and send the payment link.
- "answer": she is answering the open question, in whatever shape the answer naturally takes (a place, a name, a choice, a note, a plain no to an optional thing).
- "question": she is asking something about the sale (the total, delivery, the product) and expects an answer before she goes on.
- "change": only at the final confirm question: she wants something in the order corrected.
- "other": none of these fits.`

// thinkStageIntent asks the thinking what a buyer's reply
// inside a pending sale means, against the open question
// she was asked. It returns "" when no model can be read,
// and the caller then keeps its older behaviour.
func thinkStageIntent(from string, s *Session, merchant *catalogue.Merchant, msg string) string {
	if s == nil || s.Product == nil || merchant == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("The shop is " + merchant.Name + ". The pending sale is the " + s.Product.Name + " at " + money.GHS(s.AgreedPrice) + ".\n")
	b.WriteString("The open question she was just asked: \"" + fulfilPrompt(s) + "\"\n")
	if s.FulfilStep == "confirm" {
		f := s.PendingFulfil
		b.WriteString("The order she is confirming: " + f.Mode + ", package name " + f.PackageName + ", note " + f.Note + ".\n")
	}
	b.WriteString("Her message: \"" + msg + "\"")
	sys := stageIntentCharter
	timeout := 10000
	out := model.Chat([]model.Message{{Role: "user", Content: b.String()}}, model.Options{System: &sys, TimeoutMs: &timeout})
	if out == nil {
		return ""
	}
	text := *out
	open := strings.Index(text, "{")
	closeIdx := strings.LastIndex(text, "}")
	if open == -1 || closeIdx <= open {
		return ""
	}
	var raw struct {
		Intent string `json:"intent"`
	}
	if err := json.Unmarshal([]byte(text[open:closeIdx+1]), &raw); err != nil {
		return ""
	}
	switch raw.Intent {
	case stageIntentAnswer, stageIntentPicture, stageIntentCancel, stageIntentConfirm, stageIntentAsk, stageIntentChange, stageIntentOther:
		return raw.Intent
	}
	return ""
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
