package agent

// In-hand confirmation: on the sales where a phantom item
// would hurt most, the seller must say, in her own chat,
// that she has the item in hand before the buyer's payment
// button is sent. The sale pauses at the threshold of
// payment; her yes lets it complete exactly as it would
// have, and her no ends it with nothing charged.
//
// The gate stands on three grounds, any one enough:
//   - the item is a one-off (one in stock: if it is not in
//     hand, there is no second one to send);
//   - the goods come to GH₵1,000 or more;
//   - the seller's number is flagged with strikes.
// A new seller is NOT gated on every sale: her buyers are
// told she is new and her disputes count from the first
// sale (see trust.go), but her ordinary sales flow — a
// gate on every sale would punish exactly the honest new
// seller the platform exists for.
// A seller who has the goods answers in seconds; a seller
// who does not is stopped before money moves, which is the
// whole point. Her silence past the timeout leaves the
// buyer the same ways out as every other wait on Stall:
// wait, or walk away — the cancel law covers her.

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// expensiveGoods is the order size at which a sale always
// waits for the in-hand word.
const expensiveGoods = 1000.0

// inHandTimeout matches the fee quote's patience.
const inHandTimeout = feeQuoteTimeout

// inHandRequest is one open ask for the seller's word.
type inHandRequest struct {
	ShopID    string  `json:"shopId"`
	Buyer     string  `json:"buyer"`
	Item      string  `json:"item"`
	Price     float64 `json:"price"`
	CreatedAt string  `json:"createdAt"`
	Status    string  `json:"status"` // open, confirmed, refused, expired, cancelled
}

var inHandMu sync.Mutex

func loadInHand() []inHandRequest {
	data, err := os.ReadFile(datadir.Path("inhands.json"))
	if err != nil {
		return nil
	}
	var out []inHandRequest
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func saveInHand(all []inHandRequest) {
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(datadir.Path("inhands.json"), data, 0o600)
}

func openInHand(q inHandRequest) {
	inHandMu.Lock()
	defer inHandMu.Unlock()
	all := loadInHand()
	for i := range all {
		if all[i].ShopID == q.ShopID && all[i].Buyer == q.Buyer && all[i].Status == "open" {
			all[i].Status = "cancelled"
		}
	}
	q.Status = "open"
	q.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	all = append(all, q)
	saveInHand(all)
}

func oldestOpenInHand(shopID string) *inHandRequest {
	inHandMu.Lock()
	defer inHandMu.Unlock()
	for _, q := range loadInHand() {
		if q.ShopID == shopID && q.Status == "open" {
			qq := q
			return &qq
		}
	}
	return nil
}

func settleInHand(shopID, buyer, status string) {
	inHandMu.Lock()
	defer inHandMu.Unlock()
	all := loadInHand()
	for i := range all {
		if all[i].ShopID == shopID && all[i].Buyer == buyer && all[i].Status == "open" {
			all[i].Status = status
		}
	}
	saveInHand(all)
}

func cancelInHand(shopID, buyer string) { settleInHand(shopID, buyer, "cancelled") }

// inHandGrounds returns why this sale must wait for the
// seller's in-hand word, or "" when it may complete at
// once. The words are buyer-facing: they name the ground
// plainly, because she is the one being asked to wait.
func inHandGrounds(merchant *catalogue.Merchant, s *Session, price float64) string {
	if merchant == nil {
		return ""
	}
	owner := merchant.OwnerWhatsApp
	switch {
	case flaggedNumber(owner):
		return merchant.Name + " has strikes on its record here, so on every sale the shop confirms the goods are in hand before payment."
	case s.Product != nil && s.Product.Stock == 1:
		return "This is the only one the shop has, so the shop confirms it is in hand before you pay."
	case basketGoods(s)+price >= expensiveGoods:
		return "For an order of this size, the shop confirms the goods are in hand before you pay."
	}
	for _, it := range s.Basket {
		if it.Product.Stock == 1 {
			return "One of these is the only one the shop has, so the shop confirms the goods are in hand before you pay."
		}
	}
	return ""
}

// beginInHandWait parks the sale at the threshold of
// payment: the settled fulfilment goes back on the session,
// the step becomes the wait, and both sides are told.
func beginInHandWait(from string, s *Session, merchant *catalogue.Merchant, f fulfilment, grounds string) {
	shopID := s.MerchantID
	openInHand(inHandRequest{ShopID: shopID, Buyer: from, Item: s.itemLabel(), Price: basketGoods(s) + s.AgreedPrice})
	s.PendingFulfil = f
	s.FulfilStep = "inhandwait"
	sessions[sessionKey(shopID, from)] = s
	sendPieces(from,
		grounds,
		"I have asked "+merchant.Name+" to confirm. The moment she does, your payment button follows, and nothing is paid before it.")
	if phone := merchantNotifyPhone(merchant); phone != "" {
		sendYesNo(phone, "One moment before your buyer pays: she is buying the "+s.itemLabel()+" ("+money.GHS(basketGoods(s)+s.AgreedPrice)+").\nDo you have it in hand now?", "Yes, in hand", "Not in hand")
	}
}

// reopenInHand asks the seller again after a timeout, at
// the buyer's word: a fresh request, a fresh alert, with
// the sale's settled fulfilment still parked on her
// session.
func reopenInHand(from string, s *Session, merchant *catalogue.Merchant) {
	openInHand(inHandRequest{ShopID: s.MerchantID, Buyer: from, Item: s.itemLabel(), Price: basketGoods(s) + s.AgreedPrice})
	s.FulfilStep = "inhandwait"
	sessions[sessionKey(s.MerchantID, from)] = s
	whatsapp.SendText(from, "I have asked "+merchant.Name+" again to confirm the "+s.itemLabel()+" is in hand.")
	if phone := merchantNotifyPhone(merchant); phone != "" {
		sendYesNo(phone, "Your buyer is still waiting: do you have the "+s.itemLabel()+" ("+money.GHS(basketGoods(s)+s.AgreedPrice)+") in hand now?", "Yes, in hand", "Not in hand")
	}
}

// handleMerchantInHand is the seller's word on an open
// in-hand request, consulted in her own chat after her
// in-progress flows. A yes completes the waiting sale
// exactly as the buyer's own yes would have; a no ends it
// with nothing charged and the buyer told why.
func handleMerchantInHand(from string, merchant *catalogue.Merchant, text, lower string) bool {
	merchant = merchantAwaiting(from, merchant, func(id string) bool { return oldestOpenInHand(id) != nil })
	q := oldestOpenInHand(merchant.ID)
	if q == nil {
		return false
	}
	yes, no := isClearYes(lower), isClearNo(lower)
	if !yes && !no {
		return false
	}
	buyer := q.Buyer
	key := sessionKey(merchant.ID, buyer)
	sess := sessions[key]
	live := sess != nil && sess.Stage == "fulfilment" && (sess.FulfilStep == "inhandwait" || sess.FulfilStep == "inhandexpired")
	if no {
		settleInHand(merchant.ID, buyer, "refused")
		if live {
			whatsapp.SendText(buyer, merchant.Name+" says she does not have the "+q.Item+" in hand, so this sale cannot go ahead. Nothing has been charged.")
			dropPendingSale(buyer, sess)
		} else {
			whatsapp.SendText(buyer, merchant.Name+" says she does not have the "+q.Item+" in hand, so that sale cannot go ahead. Nothing was charged.")
		}
		whatsapp.SendText(from, "Understood. I have told the buyer, and the sale is closed.")
		return true
	}
	settleInHand(merchant.ID, buyer, "confirmed")
	if live {
		sess.InHandOK = true
		f := sess.PendingFulfil
		sess.PendingFulfil = fulfilment{}
		sessions[key] = sess
		whatsapp.SendText(from, "Thank you. Her payment button is going out now.")
		completeSale(buyer, sess, sess.AgreedPrice, f)
	} else {
		whatsapp.SendText(from, "Thank you. That buyer is not in a live chat just now; when she returns, the sale can complete.")
	}
	return true
}

// CheckInHand is the timeout pass, run by the watch's
// sweep: an unanswered in-hand ask expires, and the buyer
// is told she may wait (which asks again) or walk away.
func CheckInHand(now time.Time) {
	inHandMu.Lock()
	all := loadInHand()
	var expired []inHandRequest
	for i := range all {
		if all[i].Status != "open" {
			continue
		}
		created, err := time.Parse(time.RFC3339, all[i].CreatedAt)
		if err != nil || now.Sub(created) < inHandTimeout {
			continue
		}
		all[i].Status = "expired"
		expired = append(expired, all[i])
	}
	if len(expired) > 0 {
		saveInHand(all)
	}
	inHandMu.Unlock()
	for _, q := range expired {
		merchant := catalogue.GetMerchant(q.ShopID)
		name := "The shop"
		if merchant != nil {
			name = merchant.Name
		}
		key := sessionKey(q.ShopID, q.Buyer)
		if sess := sessions[key]; sess != nil && sess.Stage == "fulfilment" && sess.FulfilStep == "inhandwait" {
			sess.FulfilStep = "inhandexpired"
			sessions[key] = sess
		}
		whatsapp.SendText(q.Buyer, name+" has not confirmed the "+q.Item+" is in hand yet. You can keep waiting, just say wait, or say cancel and nothing more will happen. Nothing is paid until she confirms and you see your total.")
	}
}
