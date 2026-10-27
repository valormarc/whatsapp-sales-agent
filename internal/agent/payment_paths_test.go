package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/seevwebhook"
)

// Both payment paths. The link
// path confirms itself through Seev's webhook. The direct path
// is how Ghana already buys: MoMo straight to the merchant's own
// number, then the screenshot into the chat. A screenshot is a
// claim, never proof: the agent forwards it to the merchant of
// that shop alone, and only her word settles the order. These
// tests pin the whole discipline: the offer, the choice, the
// claim, her yes, her no, and the shop walls around all of it.

// sayImage delivers one inbound image message, the way a buyer
// sends her transfer screenshot.
func sayImage(t *testing.T, from, mediaID, caption string) {
	t.Helper()
	HandleInbound(Inbound{From: from, ImageID: mediaID, Caption: caption})
}

type rawOutbox struct {
	To       string `json:"to"`
	Body     string `json:"body"`
	Kind     string `json:"kind"`
	MediaRef string `json:"mediaRef"`
}

func rawOutboxRecords(t *testing.T, dir string) []rawOutbox {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "data", "outbox.jsonl"))
	if err != nil {
		return nil
	}
	var out []rawOutbox
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec rawOutbox
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("outbox line not JSON: %v", err)
		}
		out = append(out, rec)
	}
	return out
}

func outboxCountContaining(t *testing.T, dir, to, substr string) int {
	t.Helper()
	n := 0
	for _, rec := range rawOutboxRecords(t, dir) {
		if rec.To == to && strings.Contains(rec.Body, substr) {
			n++
		}
	}
	return n
}

// closePickupSale walks a buyer through a purchase at the list
// price, out the pickup door, in the Twitter shops: the agreement,
// the fulfilment answer, and the completion pieces. It returns
// the completion pieces joined.
func closePickupSale(t *testing.T, c *chat, buyer, entry, view string, listPrice string) string {
	t.Helper()
	c.say(t, buyer, entry)
	c.replies(t, buyer)
	c.say(t, buyer, view)
	c.last(t, buyer)
	c.say(t, buyer, listPrice)
	c.replies(t, buyer)
	c.say(t, buyer, "I will pick it up myself")
	return strings.Join(c.replies(t, buyer), "\n")
}

func TestDirectPathHappyPathEndToEnd(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550008001"

	got := closePickupSale(t, c, buyer, "SHOP-EFUA", "Shea butter", "120")
	// Both doors are offered at the payment moment, the direct
	// one worded from the shop's own payout phone.
	if !strings.Contains(got, "Pay here") || !strings.Contains(got, "Or send MoMo directly to Efua's Store's number, "+ownerEfua+", and send me the screenshot here.") {
		t.Fatalf("completion = %q, want the link and the direct offer", got)
	}
	c.replies(t, ownerEfua) // drain the New order notice

	// She chooses the number: the agent names the amount, the
	// number, and whose name is on it, and asks for the screenshot.
	c.say(t, buyer, "I will pay to the number")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Send GH₵120 to "+ownerEfua+". The name on the number is Efua's Store.") || !strings.Contains(got, "When you have sent it, send the screenshot here.") {
		t.Fatalf("direct instructions = %q", got)
	}
	orders := ordersOnDisk(t, dir)
	if len(orders) != 1 || orders[0]["paymentPath"] != "direct" {
		t.Fatalf("orders = %v, want the direct path recorded on the order", orders)
	}
	ref := orders[0]["reference"].(string)

	// The screenshot arrives: a claim, forwarded to Efua alone,
	// with the picture itself sent on as an image message.
	sayImage(t, buyer, "MEDIA-TEST-1", "")
	got = c.last(t, buyer)
	if !strings.Contains(got, "I have sent it to Efua's Store to confirm. I will tell you the moment she does.") {
		t.Fatalf("buyer claim ack = %q", got)
	}
	notes := strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(notes, "Payment claim: "+buyer+" says she has paid GH₵120 for Raw Shea Butter (500g) (order "+ref+") by MoMo to your number.") ||
		!strings.Contains(notes, "Reply yes if you have received it, or no if you have not.") {
		t.Fatalf("merchant claim = %q", notes)
	}
	if strings.Contains(notes, "no screenshot") {
		t.Fatalf("a screenshot claim was labelled word-only: %q", notes)
	}
	images := 0
	for _, rec := range rawOutboxRecords(t, dir) {
		if rec.To == ownerEfua && rec.Kind == "image" {
			images++
			if rec.MediaRef != "MEDIA-TEST-1" || !strings.Contains(rec.Body, "Payment claim:") {
				t.Fatalf("image outbox record = %+v, want the media ref and the claim summary as caption", rec)
			}
		}
	}
	if images != 1 {
		t.Fatalf("image messages to the merchant = %d, want exactly 1", images)
	}
	if got := c.replies(t, ownerAma); len(got) != 0 {
		t.Fatalf("Ama was told about Efua's claim: %q", got)
	}
	// The claim alone paid nothing: the order waits on her word.
	o := ordersOnDisk(t, dir)[0]
	if o["status"] != "pending" || o["claimStatus"] != "open" || o["claimScreenshot"] != true || o["screenshotRef"] != "MEDIA-TEST-1" || o["claimAmount"] != 120.0 {
		t.Fatalf("order after the claim = %v", o)
	}

	// A second screenshot while the claim is open is acknowledged,
	// never duplicated to the merchant.
	sayImage(t, buyer, "MEDIA-TEST-2", "")
	if got := c.last(t, buyer); !strings.Contains(got, "Already with Efua's Store to confirm.") {
		t.Fatalf("duplicate claim ack = %q", got)
	}
	// Exactly two outbox records carry the claim: the text piece
	// and the image caption. The duplicate added neither.
	if n := outboxCountContaining(t, dir, ownerEfua, "Payment claim:"); n != 2 {
		t.Fatalf("claim records to Efua = %d, want 2 (the text piece and the image caption)", n)
	}

	// Her yes settles it: the order is paid by her word, the buyer
	// hears it from her, and Efua's figures carry no Seev fee,
	// because no Seev was anywhere near this money.
	c.say(t, ownerEfua, "yes")
	if got := c.last(t, buyer); !strings.Contains(got, "Efua's Store has confirmed your payment of GH₵120. Thank you.") {
		t.Fatalf("buyer confirmation = %q", got)
	}
	notes = strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(notes, "Confirmed. Raw Shea Butter (500g) paid in full by MoMo to your number: GH₵120 is yours, no Seev fee on this one.") || !strings.Contains(notes, "Day book:") {
		t.Fatalf("merchant closing figures = %q", notes)
	}
	o = ordersOnDisk(t, dir)[0]
	if o["status"] != "paid" || o["paymentPath"] != "direct" || o["settledVia"] != "merchant-confirmed" || o["claimStatus"] != "confirmed" {
		t.Fatalf("settled order = %v", o)
	}
}

func TestDirectMerchantNoLeavesItUnpaidAndSheMayClaimAgain(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550008002"

	closePickupSale(t, c, buyer, "SHOP-AMA", "Smoked Tilapia", "180")
	c.replies(t, ownerAma)

	// A word-only claim: labelled as exactly that to the merchant.
	c.say(t, buyer, "I have paid")
	if got := c.last(t, buyer); !strings.Contains(got, "I have sent it to Ama's Stall to confirm.") {
		t.Fatalf("claim ack = %q", got)
	}
	notes := strings.Join(c.replies(t, ownerAma), "\n")
	if !strings.Contains(notes, "Payment claim:") || !strings.Contains(notes, "She sent no screenshot, only her word.") {
		t.Fatalf("word-only claim to Ama = %q", notes)
	}

	// Her no: the buyer is told the payment has not been seen and
	// where to check; the order stays unpaid; the claim closes.
	c.say(t, ownerAma, "no")
	if got := c.last(t, buyer); !strings.Contains(got, "Ama's Stall says she has not seen the payment yet. Check the number you sent to: "+ownerAma+".") || !strings.Contains(got, "send the screenshot again") {
		t.Fatalf("buyer after the no = %q", got)
	}
	c.replies(t, ownerAma)
	o := ordersOnDisk(t, dir)[0]
	if o["status"] != "pending" || o["claimStatus"] != "rejected" {
		t.Fatalf("order after the no = %v", o)
	}

	// When the money actually lands she claims again, and the
	// merchant hears about the new claim; her yes then settles.
	c.say(t, buyer, "I have paid")
	c.replies(t, buyer)
	if n := outboxCountContaining(t, dir, ownerAma, "Payment claim:"); n != 2 {
		t.Fatalf("claims forwarded to Ama = %d, want 2", n)
	}
	c.say(t, ownerAma, "yes")
	if got := c.last(t, buyer); !strings.Contains(got, "Ama's Stall has confirmed your payment of GH₵180.") {
		t.Fatalf("buyer after the second claim's yes = %q", got)
	}
	if o := ordersOnDisk(t, dir)[0]; o["status"] != "paid" || o["settledVia"] != "merchant-confirmed" {
		t.Fatalf("order after the second claim = %v", o)
	}
}

func TestClaimAmountMismatchIsFlaggedToTheMerchant(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550008003"

	closePickupSale(t, c, buyer, "SHOP-EFUA", "Shea butter", "120")
	c.replies(t, ownerEfua)
	ref := ordersOnDisk(t, dir)[0]["reference"].(string)

	// She names less than the total: what she said is recorded,
	// and the merchant is warned in the claim itself.
	c.say(t, buyer, "I have paid 100")
	c.replies(t, buyer)
	notes := strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(notes, "says she has paid GH₵100") || !strings.Contains(notes, "Careful: she names GH₵100, but the order total is GH₵120.") {
		t.Fatalf("mismatch claim = %q", notes)
	}
	o := ordersOnDisk(t, dir)[0]
	if o["claimAmount"] != 100.0 || o["claimMismatch"] != true || o["status"] != "pending" {
		t.Fatalf("order after the mismatched claim = %v", o)
	}

	// A merchant who names the reference settles that claim
	// outright; her word is what counts, warned or not.
	c.say(t, ownerEfua, "yes "+ref)
	if got := c.last(t, buyer); !strings.Contains(got, "Efua's Store has confirmed your payment of GH₵120.") {
		t.Fatalf("buyer after the referenced yes = %q", got)
	}
	if o := ordersOnDisk(t, dir)[0]; o["status"] != "paid" || o["claimStatus"] != "confirmed" {
		t.Fatalf("order after the referenced yes = %v", o)
	}
}

func TestClaimWithNoUnpaidOrderTroublesNoMerchant(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550008004"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "I have paid")
	if got := c.last(t, buyer); !strings.Contains(got, "no unpaid order for you here just now") {
		t.Fatalf("no-order claim reply = %q", got)
	}
	if got := c.replies(t, ownerEfua); len(got) != 0 {
		t.Fatalf("the merchant was troubled with a claim that has no order: %q", got)
	}
	// A screenshot with a claiming caption and no order behind it
	// gets the same plain answer, and still no merchant message.
	sayImage(t, buyer, "MEDIA-TEST-9", "I have paid")
	if got := c.last(t, buyer); !strings.Contains(got, "no unpaid order for you here just now") {
		t.Fatalf("no-order image claim reply = %q", got)
	}
	if got := c.replies(t, ownerEfua); len(got) != 0 {
		t.Fatalf("the merchant was troubled with an orderless screenshot: %q", got)
	}
}

func TestClaimsAndConfirmationsNeverCrossShops(t *testing.T) {
	dir, c := setupTwitterShops(t)
	buyer := "233550008005"

	// Her order is Ama's. Standing in Efua's shop, her claim
	// attaches to nothing: it is answered as a claim with no
	// order here, and neither merchant hears a word of it.
	closePickupSale(t, c, buyer, "SHOP-AMA", "Smoked Tilapia", "180")
	c.replies(t, ownerAma)
	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "I have paid")
	if got := c.last(t, buyer); !strings.Contains(got, "no unpaid order for you here just now") {
		t.Fatalf("claim in the wrong shop = %q", got)
	}
	if got := c.replies(t, ownerAma); len(got) != 0 {
		t.Fatalf("Ama heard a claim made in Efua's shop: %q", got)
	}
	if o := ordersOnDisk(t, dir)[0]; o["claimStatus"] != nil {
		t.Fatalf("Ama's order grew a claim from another shop: %v", o)
	}

	// Back in Ama's shop the claim lands where it belongs.
	c.say(t, buyer, "SHOP-AMA")
	c.replies(t, buyer)
	c.say(t, buyer, "I have paid")
	c.replies(t, buyer)
	if n := outboxCountContaining(t, dir, ownerAma, "Payment claim:"); n != 1 {
		t.Fatalf("claims to Ama = %d, want 1", n)
	}

	// Efua's yes is about her own shop, which has no open claim:
	// it falls through to her ordinary handling and settles
	// nothing of Ama's.
	c.say(t, ownerEfua, "yes")
	c.replies(t, ownerEfua)
	if o := ordersOnDisk(t, dir)[0]; o["status"] != "pending" || o["claimStatus"] != "open" {
		t.Fatalf("Efua's yes touched Ama's claim: %v", o)
	}
	if got := c.replies(t, buyer); len(got) != 0 {
		t.Fatalf("the buyer was told something by another shop's yes: %q", got)
	}

	// Ama's own yes settles her own claim.
	c.say(t, ownerAma, "yes")
	if got := c.last(t, buyer); !strings.Contains(got, "Ama's Stall has confirmed your payment of GH₵180.") {
		t.Fatalf("buyer after Ama's yes = %q", got)
	}
	if o := ordersOnDisk(t, dir)[0]; o["status"] != "paid" || o["settledVia"] != "merchant-confirmed" {
		t.Fatalf("order after Ama's yes = %v", o)
	}
}

func TestLinkSettlementStillSettlesAndMarksTheSeevPath(t *testing.T) {
	t.Setenv("SEEV_SECRET_KEY", "")
	t.Setenv("SEEV_WEBHOOK_SECRET", "")
	dir, c := setupTwoShops(t)
	buyer := "233550008006"

	c.say(t, buyer, "SHOP-AMA")
	c.last(t, buyer)
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.last(t, buyer)
	c.say(t, buyer, "160")
	c.replies(t, buyer)
	c.say(t, buyer, "162")
	c.replies(t, buyer)
	c.say(t, buyer, "Koforidua")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Pay here") {
		t.Fatalf("completion = %q", got)
	}
	placed := ordersOnDisk(t, dir)[0]
	ref := placed["reference"].(string)
	c.replies(t, ownerAma)

	// The webhook must name what the buyer actually agreed: the
	// haggle carries a deliberate random trader variation, so the
	// closing price is read from the order, never assumed.
	agreed := int(placed["total"].(float64))
	body := `{"data":{"reference":"SEEVSIM9","status":"completed","amount":` + strconv.Itoa(agreed) + `},"meta":{"order_reference":"` + ref + `"}}`
	if status := seevwebhook.Handle(body, "", ""); status != 200 {
		t.Fatalf("webhook status %d, want 200", status)
	}
	o := ordersOnDisk(t, dir)[0]
	if o["status"] != "paid" || o["paymentPath"] != "seev" {
		t.Fatalf("order after the webhook = %v, want paid on the seev path", o)
	}
	if got := c.last(t, buyer); !strings.Contains(got, "Payment received, thank you.") {
		t.Fatalf("buyer settlement message = %q", got)
	}
}
