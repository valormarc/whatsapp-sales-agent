package agent

// Buyer-side photo matching: a buyer sends a photo of a
// thing she likes or is looking for, and the agent reads
// the picture against the actual catalogue and puts the
// closest real product in front of her. The match is a
// judgment the model makes from the image and the facts;
// code verifies the named product exists before anything
// is presented, and the differences are stated honestly:
// the closest match is never dressed up as the exact one.

import (
	"encoding/json"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

const photoMatchSystem = `You are the thinking of a WhatsApp sales agent in Ghana. A buyer has sent a photo of a product she likes or is looking for. The facts list the products actually on sale, with their shops. Look at the photo and name the ONE product in the facts closest to what it shows: the same kind of thing first, then style, then colour. Be honest about the distance in "differences": one short line naming how the closest product differs from the pictured one (a different colour, a plainer style, a different material), or an empty line when the photo plainly shows that very product. Never present a near match as the exact one. If nothing in the facts is even the same kind of thing, leave "match" empty. "seen" is a few plain words for what the photo shows. Answer with exactly one JSON object: {"match": "...", "shop": "...", "differences": "...", "seen": "..."}. "match" is the product's exact name from the facts and "shop" its shop code from the facts.`

type photoMatch struct {
	Match       string `json:"match"`
	Shop        string `json:"shop"`
	Differences string `json:"differences"`
	Seen        string `json:"seen"`
}

// handleBuyerPhotoMatch serves a buyer's reference photo.
// With a shopID it matches inside that shop, the shop she
// is standing in; without one it matches across every
// shop's shelf and she stays unbound, her pick of the
// result being what takes her into its shop. It reports
// whether it took the turn; a photo the platform cannot
// read at all falls back to the caller's own words.
func handleBuyerPhotoMatch(from, shopID, imageID, caption string) bool {
	var candidates []salesCandidate
	if shopID != "" {
		shop := catalogue.GetMerchant(shopID)
		if shop == nil {
			return false
		}
		for _, p := range catalogue.ListProducts(shopID) {
			pp := p
			candidates = append(candidates, salesCandidate{Shop: shop, Product: &pp})
		}
	} else {
		for _, m := range catalogue.ListMerchants() {
			shop := m
			for _, p := range catalogue.ListProducts(shop.ID) {
				pp := p
				candidates = append(candidates, salesCandidate{Shop: &shop, Product: &pp})
			}
		}
	}
	if len(candidates) == 0 {
		return false
	}
	dataURI, ok := whatsapp.DownloadMedia(imageID)
	if !ok {
		return false
	}
	var b strings.Builder
	b.WriteString("On sale:\n")
	for _, c := range candidates {
		b.WriteString("SHOP-" + c.Shop.ShopCode + " (" + c.Shop.Name + "): " + c.label() + "\n")
	}
	if strings.TrimSpace(caption) != "" {
		b.WriteString("Her words with the photo: \"" + strings.TrimSpace(caption) + "\"\n")
	}
	out := model.ChatVision(photoMatchSystem, b.String(), dataURI)
	if out == nil {
		return false
	}
	text := *out
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return false
	}
	var m photoMatch
	if err := json.Unmarshal([]byte(text[start:end+1]), &m); err != nil {
		return false
	}
	var match *salesCandidate
	if strings.TrimSpace(m.Match) != "" {
		// The facts render a code as SHOP-<CODE>, and the
		// thinking may hand it back in that dress: the code
		// itself is what must match.
		shopCode := strings.TrimSpace(m.Shop)
		if len(shopCode) > 5 && strings.EqualFold(shopCode[:5], "SHOP-") {
			shopCode = shopCode[5:]
		}
		for i := range candidates {
			if strings.EqualFold(candidates[i].Product.Name, strings.TrimSpace(m.Match)) &&
				(shopID != "" || shopCode == "" || strings.EqualFold(candidates[i].Shop.ShopCode, shopCode)) {
				match = &candidates[i]
				break
			}
		}
	}
	if match == nil {
		// Nothing close enough to name: that is said
		// plainly, and what IS here is offered instead.
		if shopID != "" {
			shop := catalogue.GetMerchant(shopID)
			whatsapp.SendText(from, "I do not see anything close to that at "+shop.Name+" just now. Here is what they have:\n"+productListText(shopID))
		} else {
			whatsapp.SendText(from, "I do not see anything close to that in any shop here just now. Tell me the kind of thing you are looking for and I will check every shop for it.")
		}
		return true
	}
	lead := "The closest here is the " + match.Product.Name + " at " + money.GHS(match.Product.ListPrice)
	if shopID == "" {
		lead = "At " + match.Shop.Name + ", the closest is the " + match.Product.Name + " at " + money.GHS(match.Product.ListPrice)
	}
	lead += "."
	var pieces []string
	pieces = append(pieces, lead)
	if d := strings.TrimSpace(m.Differences); d != "" && !strings.Contains(d, "₵") && voiceClean(d) {
		pieces = append(pieces, "How it differs: "+d)
	}
	sendPieces(from, pieces...)
	sendProductPhotos(from, match.Product)
	if shopID != "" {
		key := sessionKey(shopID, from)
		s, ok := sessions[key]
		if !ok || s == nil {
			s = &Session{Stage: "browsing", MerchantID: shopID}
		}
		s.LastViewed = match.Product
		sessions[key] = s
	} else {
		pre, ok := sessions[sessionKey("", from)]
		if !ok || pre == nil {
			pre = &Session{Stage: "unbound"}
			sessions[sessionKey("", from)] = pre
		}
		pre.DiscoveryShown = []ShownProduct{{ShopID: match.Shop.ID, ProductID: match.Product.ID}}
	}
	return true
}
