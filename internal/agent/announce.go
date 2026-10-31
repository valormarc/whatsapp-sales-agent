package agent

// Bringing buyers in. A listing that nobody hears about
// does not sell, and the sellers in the market groups do
// their own announcing: a Status post with the photo, and a
// message forwarded to their contacts and groups. So the
// moment a product goes live, the agent hands its seller
// both, ready to use: a Status caption written the way
// group sellers write, riding the product's own photo when
// it has one, and a forwardable broadcast carrying her shop
// link. She copies, pastes, and posts; bringing buyers in
// stops being homework she never gets to. The words are
// drafted by the thinking from the listing's facts and
// verified like any price: the list price and her ordering
// line must be in them word for word, and her private last
// price must never appear.

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// listingDraftCharter briefs the thinking that writes a
// seller's two announcements. The voice is hers, learned
// from how sellers actually post in the groups: warm,
// direct and proud of the goods, never hype.
const listingDraftCharter = `You write the two short announcements a Ghanaian shop seller posts herself the moment a new product goes live in her WhatsApp shop. She will copy them exactly as written, so write in her voice: warm, direct, proud of her goods, the way sellers in the market groups write, in correct English. No hype and no claims the facts do not carry. Her list price and her ordering line must appear exactly as given, character for character. Her last price is private and is never given to you; never invent a discount or a lower figure.

Answer with strict JSON only: {"status": "...", "broadcast": "..."}

- "status": her WhatsApp Status caption, the words that ride under the product's photo. Two to four short lines: the product, one true selling line from its description, the price, then the ordering line.
- "broadcast": the message she forwards to her contacts and groups. Three to five short lines: the product and what is good about it, the price, how the goods reach the buyer when the facts say, then the ordering line.`

// shopEntryLink is the tappable door into one shop: the
// shared number carrying her code. Without a configured
// number there is no link, and the code itself is the door.
func shopEntryLink(shop *catalogue.Merchant) string {
	if shop == nil {
		return ""
	}
	if number := digitsOnly(os.Getenv("AGENT_WHATSAPP_NUMBER")); number != "" {
		return "https://wa.me/" + number + "?text=SHOP-" + shop.ShopCode
	}
	return ""
}

// orderLineFor is the one line every announcement must
// carry: how a buyer reaches this shop and only this shop.
func orderLineFor(shop *catalogue.Merchant) string {
	if link := shopEntryLink(shop); link != "" {
		return "Order here: " + link
	}
	return "To order, message this number and start with SHOP-" + shop.ShopCode
}

// listingDrafts writes a product's Status caption and
// broadcast message: the thinking drafts them from the
// facts, each verified (price and ordering line present,
// last price absent, voice laws held), and any draft that
// fails is replaced by a plain factual one built here.
func listingDrafts(shop *catalogue.Merchant, p catalogue.Product, turn string) (status, broadcast string) {
	orderLine := orderLineFor(shop)
	price := money.GHS(p.ListPrice)
	status = p.Name + " — " + price + "\n" + p.Description + "\n" + orderLine
	broadcast = "Hello. " + shop.Name + " has " + p.Name + " for " + price + ". " + p.Description + "\n" + orderLine
	if len(p.Sizes) > 0 {
		broadcast += "\nSizes: " + sizesLine(&p) + "."
	}

	var b strings.Builder
	b.WriteString("Shop: " + shop.Name + "\n")
	b.WriteString("Product: " + p.Name + "\n")
	b.WriteString("Description: " + p.Description + "\n")
	b.WriteString("Price: " + price + "\n")
	if len(p.Sizes) > 0 {
		b.WriteString("Sizes: " + sizesLine(&p) + "\n")
	}
	if shop.DeliveryFee > 0 {
		b.WriteString("Delivery: " + money.GHS(shop.DeliveryFee) + ". " + shop.DeliveryNote + "\n")
	} else {
		b.WriteString("Delivery is arranged with the seller.\n")
	}
	b.WriteString("Ordering line (use exactly): " + orderLine + "\n")
	out := draftWithBudget(turn, listingDraftCharter, "The settled facts of the listing:\n"+b.String()+"\nWrite the two announcements now.")
	if out == "" {
		return status, broadcast
	}
	open := strings.Index(out, "{")
	end := strings.LastIndex(out, "}")
	if open == -1 || end <= open {
		return status, broadcast
	}
	var raw struct {
		Status    string `json:"status"`
		Broadcast string `json:"broadcast"`
	}
	if json.Unmarshal([]byte(out[open:end+1]), &raw) != nil {
		return status, broadcast
	}
	clean := func(draft string) bool {
		draft = strings.TrimSpace(draft)
		if draft == "" || !voiceClean(draft) {
			return false
		}
		if !strings.Contains(draft, price) || !strings.Contains(draft, orderLine) {
			return false
		}
		// Every figure in the draft must be one of the
		// listing's public figures: the list price, and the
		// shop's delivery fee when the facts carry it. The
		// second audit found this the one drafting path
		// with no figure check at all — an invented anchor
		// ("Was GH₵900") passed, and her private last price
		// leaked in any format but the one exact string the
		// old check excluded. Under figuresClean the last
		// price fails in digits, in words, in any dress,
		// because it is never in the allowed set.
		allowed := []float64{p.ListPrice}
		if shop.DeliveryFee > 0 {
			allowed = append(allowed, shop.DeliveryFee)
		}
		return figuresClean(draft, allowed...)
	}
	if clean(raw.Status) {
		status = strings.TrimSpace(raw.Status)
	}
	if clean(raw.Broadcast) {
		broadcast = strings.TrimSpace(raw.Broadcast)
	}
	return status, broadcast
}

// announceListing hands a seller the two announcements for
// a product that has just gone live. With a kept photo, the
// Status caption rides the photo itself, the way she will
// post it; without one, both go as copyable text.
func announceListing(from, shopID string, product catalogue.Product) {
	merchant := catalogue.GetMerchant(shopID)
	if merchant == nil {
		return
	}
	fresh := product
	for _, p := range catalogue.ListProducts(merchant.ID) {
		if p.ID == product.ID {
			fresh = p
		}
	}
	status, broadcast := listingDrafts(merchant, fresh, turnTokenFor(from))
	photoSent := false
	if len(fresh.Photos) > 0 {
		if data, err := os.ReadFile(datadir.Path(fresh.Photos[0])); err == nil && len(data) > 0 {
			mime := "image/jpeg"
			switch {
			case strings.HasSuffix(fresh.Photos[0], ".png"):
				mime = "image/png"
			case strings.HasSuffix(fresh.Photos[0], ".webp"):
				mime = "image/webp"
			}
			if ref, ok := uploadMediaFn(data, mime); ok {
				whatsapp.SendImage(from, ref, status)
				photoSent = true
			}
		}
	}
	if !photoSent {
		whatsapp.SendText(from, "For your Status, copy this:\n\n"+status)
	}
	whatsapp.SendText(from, "To forward to your contacts and groups:\n\n"+broadcast)
}
