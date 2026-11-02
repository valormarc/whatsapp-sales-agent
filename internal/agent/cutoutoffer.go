package agent

import (
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// The photo edit offer: the cutout is the merchant's to
// instruct, never the platform's to assume (her ruling,
// 2 November, in her words: "It can only cut out if the
// person click on yes. If the person did not click on
// yes, or the person ignored, it should use the original
// photo. If the person click no, it should use the
// original photo.").
//
// So a photo accepted into a listing is stored and shown
// exactly as sent, and no segmentation call is made at
// acceptance, ever. What a save does instead is queue one
// offer for the listing's photos, and the offer is armed
// as the turn's last word, when nothing else is asking
// her anything: not over an open walk question, not over
// the burst batch's price question, not over another
// armed question. While the offer stands as the current
// question, her yes — the button tap, or the typed word a
// tap is translated into — runs the edit on that listing
// alone. Her no, her silence, or any other business at
// all spends that offer: the photo keeps its original,
// permanently for that offer, and it is never asked
// again. A burst that saves several pictured items
// offers them one at a time, each answer settling the
// item it names and the next offer following only while
// she keeps answering.
//
// Her own words are an instruction of the same weight:
// when the reading understands her telling Stall to cut
// out a listed item's photo, the edit runs on that item
// with no offer needed (see serveCutoutInstruction).

// cutoutKind is the armed-question kind a photo edit
// offer rides on.
const cutoutKind = "cutout"

// cutoutGoFn is the seam the edit's background run goes
// through: a cold segmentation call takes its time, and
// the conversation never waits on it — she is told the
// edit is under way and hears the outcome when it lands.
// Tests stand in a runner that executes inline.
var cutoutGoFn = func(f func()) { go f() }

// offerCutoutsAfterSave queues one edit offer for each
// product a save has just stored photos on. It sends
// nothing itself: arming happens at the turn's seams
// (see armNextCutoutOffer), so the offer is always the
// last word of a turn and never a second question beside
// another flow's.
func offerCutoutsAfterSave(from string, s *Session, merchantID string, saved []catalogue.Product) {
	if s == nil || segmentEndpoint() == "" {
		return
	}
	queued := false
	for _, p := range saved {
		fresh := catalogue.FindByID(merchantID, p.ID)
		if fresh == nil || len(fresh.Photos) == 0 {
			continue
		}
		if s.YesNoQ != nil && s.YesNoQ.Kind == cutoutKind && s.YesNoQ.Ref == fresh.ID {
			continue
		}
		already := false
		for _, id := range s.CutoutQueue {
			if id == fresh.ID {
				already = true
				break
			}
		}
		if already {
			continue
		}
		s.CutoutQueue = append(s.CutoutQueue, fresh.ID)
		queued = true
	}
	if queued {
		sessions.put(sessionKey(merchantID, from), s)
	}
}

// armNextCutoutOffer puts the longest-waiting queued
// offer in front of her, when the counter is clear for
// it: no question of any other kind armed or pending,
// no listing walk open. A queued product that has since
// left her shelf, or lost its photos, is simply skipped:
// there is nothing left to offer an edit on.
func armNextCutoutOffer(from string, s *Session) {
	if s == nil || len(s.CutoutQueue) == 0 {
		return
	}
	if s.YesNoQ != nil || s.PendingMerchantQ != nil || s.Onboarding != nil {
		return
	}
	if segmentEndpoint() == "" {
		s.CutoutQueue = nil
		sessions.put(sessionKey(s.MerchantID, from), s)
		return
	}
	for len(s.CutoutQueue) > 0 {
		id := s.CutoutQueue[0]
		s.CutoutQueue = s.CutoutQueue[1:]
		p := catalogue.FindByID(s.MerchantID, id)
		if p == nil || len(p.Photos) == 0 {
			continue
		}
		askYesNo(from, s, cutoutKind, p.ID,
			"I can cut the goods out of the "+p.Name+" photo and set them on a clean white background, if you want. Shall I?",
			"Yes", "No")
		return
	}
	sessions.put(sessionKey(s.MerchantID, from), s)
}

// lapseCutoutOffer spends the offer standing in front
// of her: she has moved on to other business, and under
// her ruling anything but a yes leaves that photo
// exactly as sent, the offer never asked twice. Offers
// still queued behind it were never put to her, so
// nothing has lapsed for them; they wait for the next
// save's seam, each naming its own product when it comes.
func lapseCutoutOffer(s *Session) {
	if s == nil {
		return
	}
	if s.YesNoQ != nil && s.YesNoQ.Kind == cutoutKind {
		s.YesNoQ = nil
	}
}

// serveCutoutAnswer settles an answered offer. A no
// leaves the photo as sent, in so many words, and the
// next waiting offer follows. A yes acknowledges at
// once and runs the edit in the background; when it
// lands she is shown the cut photo, or told plainly it
// could not be done and the original stands.
func serveCutoutAnswer(from string, s *Session, merchant *catalogue.Merchant, productID string, yes bool) {
	if s.YesNoQ != nil && s.YesNoQ.Kind == cutoutKind {
		s.YesNoQ = nil
		sessions.put(sessionKey(merchant.ID, from), s)
	}
	product := catalogue.FindByID(merchant.ID, productID)
	if !yes {
		if product != nil {
			whatsapp.SendText(from, "Alright, the "+product.Name+" photo stays exactly as you sent it.")
		}
		armNextCutoutOffer(from, s)
		return
	}
	if product == nil || len(product.Photos) == 0 {
		whatsapp.SendText(from, "That photo is no longer on a listing, so there is nothing to cut out.")
		armNextCutoutOffer(from, s)
		return
	}
	whatsapp.SendText(from, "Alright, I will cut the "+product.Name+" out and set it on a clean white background.")
	executeCutout(from, merchant, product, func() {
		// The next offer follows only onto a clear
		// counter: if she has since opened a listing or
		// taken up another question, the queue simply
		// waits for the next save's seam.
		if live := sessions.get(sessionKey(merchant.ID, from)); live != nil {
			armNextCutoutOffer(from, live)
		}
	})
}

// executeCutout runs one instructed edit in the
// background and reports its outcome to her: the cut
// photo itself when at least one photo was cut, and a
// plain failure line — the original stands — when none
// was. The after hook, when given, runs once the outcome
// has been sent.
func executeCutout(from string, merchant *catalogue.Merchant, product *catalogue.Product, after func()) {
	cutoutGoFn(func() {
		cut, _ := cutOutProductPhotos(merchant.ID, product)
		if cut > 0 {
			sendProductPhotos(from, merchant.ID, product)
			whatsapp.SendText(from, "Done ✅ "+product.Name+" — its photo is now on a clean white background, and your original photo is kept beside it.")
		} else {
			whatsapp.SendText(from, "I could not cut out the "+product.Name+" photo just now, so it stays exactly as you sent it.")
		}
		if after != nil {
			after()
		}
	})
}

// serveCutoutInstruction carries out the edit she asked
// for in her own words, the reading having named the
// product in "item". An item the reading could not name,
// or a name that fits more than one product on her
// shelf, is met with one question — which one — and
// nothing is cut until she answers it.
func serveCutoutInstruction(from string, merchant *catalogue.Merchant, shopID string, u *understood) bool {
	item := strings.TrimSpace(u.Item)
	if item == "" {
		whatsapp.SendText(from, "Which product's photo shall I cut out? Name it, and I will cut the goods out onto a clean white background.")
		return true
	}
	product, candidates := resolveCutoutTarget(shopID, item)
	if product == nil {
		if len(candidates) > 0 {
			whatsapp.SendText(from, "That name fits more than one product on your shelf: "+joinAnd(candidates)+". Which one's photo shall I cut out?")
			return true
		}
		whatsapp.SendText(from, "I cannot find "+item+" on your shelf. Which product's photo shall I cut out?")
		return true
	}
	if len(product.Photos) == 0 {
		whatsapp.SendText(from, "The "+product.Name+" has no photo on its listing, so there is nothing to cut out.")
		return true
	}
	if segmentEndpoint() == "" {
		whatsapp.SendText(from, "I cannot cut photos out just now, so the "+product.Name+" photo stays as you sent it.")
		return true
	}
	whatsapp.SendText(from, "Alright, I will cut the "+product.Name+" out and set it on a clean white background.")
	executeCutout(from, merchant, product, nil)
	return true
}

// resolveCutoutTarget settles which product a name she
// spoke means, the way her own commands resolve names:
// an exact match first, then a product whose name
// carries her words. It returns the product alone when
// the name settles on one; when it fits several, the
// product is nil and the candidates are named, so she
// can be asked which one she means.
func resolveCutoutTarget(shopID, item string) (*catalogue.Product, []string) {
	q := strings.ToLower(strings.TrimSpace(item))
	if q == "" {
		return nil, nil
	}
	var exact, near []catalogue.Product
	for _, p := range catalogue.ListProducts(shopID) {
		name := strings.ToLower(strings.TrimSpace(p.Name))
		switch {
		case name == q:
			exact = append(exact, p)
		case strings.Contains(name, q):
			near = append(near, p)
		}
	}
	pick := func(set []catalogue.Product) (*catalogue.Product, []string) {
		if len(set) == 1 {
			return &set[0], nil
		}
		names := make([]string, 0, len(set))
		for _, p := range set {
			names = append(names, p.Name)
		}
		return nil, names
	}
	if len(exact) > 0 {
		return pick(exact)
	}
	if len(near) > 0 {
		return pick(near)
	}
	return nil, nil
}
