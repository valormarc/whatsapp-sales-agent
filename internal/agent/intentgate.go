package agent

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
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
