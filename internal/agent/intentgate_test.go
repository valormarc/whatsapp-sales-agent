package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// lastStagePrompt holds the most recent stage read the
// stub was handed, so a test can see what the thinking was
// given to reason with.
var lastStagePrompt string

// stubStageModel stands in for the thinking the way the
// live model is briefed: it reads the stage charter's
// request and answers with the intent the words MEAN. The
// phrases the tests use are chosen to carry no trigger
// words at all, so a pass can only come from the meaning
// being read, never from a pattern in the code.
func stubStageModel(t *testing.T) {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		system, her := "", ""
		for _, m := range body.Messages {
			if m.Role == "system" {
				system = m.Content
			}
			if i := strings.LastIndex(m.Content, "Her message:"); i >= 0 {
				her = strings.ToLower(m.Content[i:])
			}
		}
		content := `{"intent":"unclear","reply":""}`
		if strings.Contains(system, "You are the thinking of a WhatsApp sales agent") {
			switch {
			case strings.Contains(her, "new payment link"):
				content = `{"intent":"payment_expired","reply":""}`
			case strings.Contains(her, "i want to pay now"):
				content = `{"intent":"payment_want","reply":""}`
			}
		}
		if strings.Contains(system, "intent reader for a seller") {
			content = `{"intent":"answer"}`
			her := ""
			for _, m := range body.Messages {
				if m.Role == "user" {
					her = m.Content
				}
			}
			// Judge the current message only: her earlier
			// words ride along in the prompt as history, and
			// a question she asked two turns ago is not the
			// message being read now.
			if i := strings.LastIndex(her, "Her message: \""); i >= 0 {
				her = her[i:]
			}
			switch {
			case strings.Contains(her, "what do you mean by last price"):
				content = `{"intent":"question","reply":"Your last price is the lowest you will accept for it. I bargain between your list price and your last price, and I never go below it. Customers never see it."}`
			case strings.Contains(her, "why do you need the name"):
				content = `{"intent":"question","reply":"The name must match the MoMo account itself. Your sales money is paid into that account, and it cannot land if the name is different."}`
			case strings.Contains(her, "do it later"):
				content = `{"intent":"cancel"}`
			}
		}
		if strings.Contains(system, "intent reader for a pending sale") {
			for _, m := range body.Messages {
				if m.Role == "user" {
					lastStagePrompt = m.Content
				}
			}
			content = `{"intent":"answer"}`
			switch {
			case strings.Contains(her, "see what it looks like"), strings.Contains(her, "not buying without pic"), strings.Contains(her, "show me pic"):
				content = `{"intent":"picture"}`
			case strings.Contains(her, "forget it"), strings.Contains(her, "drop the whole thing"):
				content = `{"intent":"cancel"}`
			case strings.Contains(her, "in all"):
				content = `{"intent":"question","reply":"Your total is GH₵875: GH₵850 for the bag and GH₵25 for delivery."}`
			case strings.Contains(her, "cover delivery"):
				content = `{"intent":"question","reply":"No. The bag is GH₵850 and delivery is GH₵25, so GH₵875 in all."}`
			}
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		})
	}))
	t.Cleanup(stub.Close)
	t.Setenv("META_MODEL_API_KEY", "stub-key")
	t.Setenv("META_MODEL_API_BASE", stub.URL+"/v1")
}

// The intent gate: a picture ask is served as a picture ask
// wherever it lands, a call-off ends the sale wherever it
// lands, and entry text that quotes the shop's own line
// selects the product instead of closing a deal. These are
// the beats of the screenshot where a buyer asking for a
// pic was marched to a payment link with "Show me pic" as
// the name on her package.

func TestPictureAskAtTheChoiceStepServesThePicNotTheSale(t *testing.T) {
	_, c := setupTwitterShops(t)
	stubStageModel(t)
	buyer := "233550009101"
	agreeBagAtList(t, c, buyer)

	// She is at the fulfilment choice. "I am not buying
	// without pic" is a picture ask, not a place.
	c.say(t, buyer, "I'm not buying without pic")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "I don't have the pic of the Handmade Beaded Bag in this chat. Let me get it from the shop owner for you.") {
		t.Fatalf("the picture ask at the choice step = %q, want the plain no-pic line", got)
	}
	if strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("the sale advanced past the choice on a picture ask: %q", got)
	}
	ownerGot := strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(ownerGot, "A buyer is asking for a picture of Handmade Beaded Bag") {
		t.Fatalf("the owner was not asked for the picture: %q", ownerGot)
	}

	// The choice is still open afterwards: her tap lands.
	tap(t, buyer, "ff_self")
	if got := c.last(t, buyer); !strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("after the pic ask the flow should still complete its choice, got %q", got)
	}
}

func TestPictureAskAtThePackageQuestionIsNotTheName(t *testing.T) {
	_, c := setupTwitterShops(t)
	stubStageModel(t)
	buyer := "233550009102"
	agreeBagAtList(t, c, buyer)
	tap(t, buyer, "ff_self")

	c.say(t, buyer, "Show me pic")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "I don't have the pic of the Handmade Beaded Bag") {
		t.Fatalf("the picture ask at the name question = %q", got)
	}
	if !strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("the name question should be asked again after the pic: %q", got)
	}
	c.replies(t, ownerEfua)

	// Her real name still lands as the name.
	c.say(t, buyer, "Akosua Mensah")
	if got := c.last(t, buyer); !strings.Contains(got, "Any message for the seller") {
		t.Fatalf("after a real name the flow should move to the note question, got %q", got)
	}
}

func TestForgetItAtTheNoteStepCancelsAndANoteIsNotACancel(t *testing.T) {
	dir, c := setupTwitterShops(t)
	stubStageModel(t)

	buyer := "233550009103"
	agreeBagAtList(t, c, buyer)
	tap(t, buyer, "ff_self")
	c.say(t, buyer, "Akosua Mensah")
	c.say(t, buyer, "What? Just forget it")
	if got := c.last(t, buyer); !strings.Contains(got, "Okay, cancelled") {
		t.Fatalf("a call-off at the note step = %q, want the sale cancelled", got)
	}
	if got := ordersCount(t, dir); got != 0 {
		t.Fatalf("orders after the call-off = %d, want none", got)
	}

	// The same words carrying a place onward are a note.
	buyer2 := "233550009104"
	agreeBagAtList(t, c, buyer2)
	tap(t, buyer2, "ff_self")
	c.say(t, buyer2, "Akosua Mensah")
	c.say(t, buyer2, "leave it at the gate")
	got := strings.Join(c.replies(t, buyer2), "\n")
	if !strings.Contains(got, "Please confirm your order") {
		t.Fatalf("a note carrying 'leave it' should reach the recap, got %q", got)
	}
}

func TestDiscoveryPictureAskBindsServesAndTheOwnerAnswers(t *testing.T) {
	stubPhotoMedia(t)
	dir, c := setupTwitterShops(t)
	stubDiscoveryModel(t)
	buyer := "233550009105"

	c.say(t, buyer, "I'm looking for a bag")
	shown := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(shown, "EFUA has: Handmade Beaded Bag") {
		t.Fatalf("the search should show the bag first: %q", shown)
	}
	c.say(t, buyer, "Show me picture")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "I don't have the pic of the Handmade Beaded Bag in this chat. Let me get it from the shop owner for you.") {
		t.Fatalf("the discovery picture ask = %q", got)
	}
	if strings.Contains(got, "Here is what we have") || strings.Contains(got, "Deal.") {
		t.Fatalf("the picture turn must not sell: %q", got)
	}
	ownerGot := strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(ownerGot, "A buyer is asking for a picture of Handmade Beaded Bag") {
		t.Fatalf("the owner was not asked: %q", ownerGot)
	}

	// The owner's answering photo is passed to the buyer and
	// kept on the product; it opens no listing.
	before := len(doorRecords(t, dir, buyer))
	HandleInbound(Inbound{From: ownerEfua, ImageID: "bag-front"})
	ownerReply := strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(ownerReply, "I have sent it to the buyer") {
		t.Fatalf("the owner's photo should be confirmed as passed on, got %q", ownerReply)
	}
	if strings.Contains(ownerReply, "Got it ✅") {
		t.Fatalf("the answering photo must not open a listing: %q", ownerReply)
	}
	images := 0
	for _, rec := range doorRecords(t, dir, buyer)[before:] {
		if rec.Kind == "image" {
			images++
		}
	}
	if images != 1 {
		t.Fatalf("the buyer should receive the owner's photo, got %d image sends", images)
	}
	shop := catalogue.GetMerchant("efua")
	var bag *catalogue.Product
	for i := range shop.Products {
		if shop.Products[i].ID == "beaded-bag" {
			bag = &shop.Products[i]
		}
	}
	if bag == nil || len(bag.Photos) != 1 {
		t.Fatalf("the photo should be kept on the product, got %+v", bag)
	}
}

func TestEntryEchoSelectsTheProductItDoesNotDeal(t *testing.T) {
	_, c := setupTwitterShops(t)
	buyer := "233550009106"
	c.say(t, buyer, "EFUA has: Handmade Beaded Bag - GH₵850")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "It goes for GH₵850") {
		t.Fatalf("the echoed line should show her the product: %q", got)
	}
	if strings.Contains(got, "Deal.") || strings.Contains(got, "Where are you based?") || strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("the echoed line must not close a deal: %q", got)
	}
}

// The meaning proof: neither phrase below carries a word
// any pattern could catch (no pic, photo or picture; no
// cancel or forget), so only a reader of meaning can route
// them: the first to the picture, the second to the end of
// the sale.
func TestStageIntentReadsMeaningNotWords(t *testing.T) {
	dir, c := setupTwitterShops(t)
	stubStageModel(t)

	buyer := "233550009107"
	agreeBagAtList(t, c, buyer)
	c.say(t, buyer, "Before I pay I need to see what it looks like")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "I don't have the pic of the Handmade Beaded Bag") {
		t.Fatalf("a meaning-only picture ask at the choice step = %q", got)
	}
	if !strings.Contains(got, "how do you want to get it") {
		t.Fatalf("the open question should be held up again after the picture: %q", got)
	}
	c.replies(t, ownerEfua)

	buyer2 := "233550009108"
	agreeBagAtList(t, c, buyer2)
	tap(t, buyer2, "ff_self")
	c.say(t, buyer2, "Akosua Mensah")
	c.say(t, buyer2, "Actually, drop the whole thing")
	if got := c.last(t, buyer2); !strings.Contains(got, "Okay, cancelled") {
		t.Fatalf("a meaning-only call-off at the note step = %q, want the sale cancelled", got)
	}
	if got := ordersCount(t, dir); got != 0 {
		t.Fatalf("orders after the call-off = %d, want none", got)
	}
}

// A question inside a pending sale is answered from the
// sale's facts before the open question is held up again:
// understanding the intention is only half the work.
func TestStageQuestionIsAnsweredNotSwallowed(t *testing.T) {
	_, c := setupTwitterShops(t)
	stubStageModel(t)

	// At the fulfilment choice.
	buyer := "233550009109"
	agreeBagAtList(t, c, buyer)
	c.say(t, buyer, "How much will I pay in all?")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "GH₵875") {
		t.Fatalf("her total question should be answered from the facts, got %q", got)
	}
	if !strings.Contains(got, "how do you want to get it") {
		t.Fatalf("the open question should be held up again after the answer, got %q", got)
	}

	// At the package name question: the question is not
	// taken as her name, and her real name still lands after.
	buyer2 := "233550009110"
	agreeBagAtList(t, c, buyer2)
	tap(t, buyer2, "ff_self")
	c.say(t, buyer2, "Wait, does the price cover delivery?")
	got = strings.Join(c.replies(t, buyer2), "\n")
	if !strings.Contains(got, "GH₵875 in all") {
		t.Fatalf("her delivery question at the name step should be answered, got %q", got)
	}
	if !strings.Contains(got, "What name should go on the package?") {
		t.Fatalf("the name question should be asked again, got %q", got)
	}
	c.say(t, buyer2, "Akosua Mensah")
	if got := c.last(t, buyer2); !strings.Contains(got, "Any message for the seller") {
		t.Fatalf("after the answer her name should land as the name, got %q", got)
	}
}

// The reasoning window: what she said earlier in the chat
// reaches the stage read, so a reference back to it can be
// understood instead of guessed at.
func TestStageReadSeesHerEarlierWords(t *testing.T) {
	_, c := setupTwitterShops(t)
	stubStageModel(t)
	buyer := "233550009111"
	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "I am buying this for my sister")
	c.replies(t, buyer)
	c.say(t, buyer, "Handmade Beaded Bag")
	c.last(t, buyer)
	c.say(t, buyer, "850")
	c.replies(t, buyer)
	c.say(t, buyer, "Before I pay I need to see what it looks like")
	c.replies(t, buyer)
	if !strings.Contains(lastStagePrompt, "buying this for my sister") {
		t.Fatalf("the stage read should carry her earlier words, got %q", lastStagePrompt)
	}
	if !strings.Contains(lastStagePrompt, "What she said earlier in this chat") {
		t.Fatalf("the stage read should label the window as her earlier words, got %q", lastStagePrompt)
	}
}

// The seller's boards are read the same way as the buyer's:
// inside her own listing walk, a question about the step is
// answered and the step waits, and a call-off in her own
// words drops the walk.
func TestSellerWalkQuestionAnsweredAndCallOffHonoured(t *testing.T) {
	_, c := setupTwitterShops(t)
	stubStageModel(t)
	owner := ownerEfua
	c.say(t, owner, "add a product")
	c.last(t, owner)
	c.say(t, owner, "Kente Scarf")
	c.last(t, owner)
	c.say(t, owner, "Handwoven kente scarf, bright colours.")
	c.last(t, owner)
	c.say(t, owner, "300")
	c.last(t, owner)
	c.say(t, owner, "Wait, what do you mean by last price?")
	got := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(got, "lowest you will accept") {
		t.Fatalf("her question about the step should be answered, got %q", got)
	}
	if !strings.Contains(got, "what is your last price for Kente Scarf") {
		t.Fatalf("the step should be asked again after the answer, got %q", got)
	}
	// The walk is still standing exactly where she left it.
	c.say(t, owner, "250")
	if got := c.last(t, owner); !strings.Contains(got, "by measure") && !strings.Contains(got, "Shall I add it") {
		t.Fatalf("her last price should land as the walk's answer after the question, got %q", got)
	}

	// A call-off in her own words, at the very first step.
	owner2Shop(t, c)
	c.say(t, "233200000099", "add a product")
	c.last(t, "233200000099")
	c.say(t, "233200000099", "Actually I will do it later")
	if got := c.last(t, "233200000099"); !strings.Contains(got, "Dropped it") {
		t.Fatalf("a call-off inside the walk should drop it, got %q", got)
	}
}

// owner2Shop gives the second test seller a shop of her own
// in the scratch catalogue.
func owner2Shop(t *testing.T, c *chat) {
	t.Helper()
	if _, err := catalogue.CreateMerchant("Second Shop", "233200000099"); err != nil {
		t.Fatalf("second shop: %v", err)
	}
}

// The signup walk is read too: why a step is needed is
// answered before the step takes her words as its answer.
func TestSignupQuestionAnsweredNotConsumed(t *testing.T) {
	dir := setupChat(t, "233550009120", `{"merchants":{}}`)
	c := &chat{dir: dir}
	fake := &fakeSeev{}
	startFakeSeev(t, fake)
	stubStageModel(t)
	stranger := "233550009120"
	c.say(t, stranger, "open a shop")
	c.last(t, stranger)
	c.say(t, stranger, "Adjoa's Kitchen")
	got := c.last(t, stranger)
	if !strings.Contains(got, "AKOSUA MENSAH") {
		t.Fatalf("the walk should be standing on the holder confirmation, got %q", got)
	}
	c.say(t, stranger, "But why do you need the name on the account?")
	got = strings.Join(c.replies(t, stranger), "\n")
	if !strings.Contains(got, "cannot land if the name is different") {
		t.Fatalf("her why question should be answered, got %q", got)
	}
	if !strings.Contains(got, "reads AKOSUA MENSAH") {
		t.Fatalf("the confirmation should be asked again after the answer, got %q", got)
	}
	c.say(t, stranger, "yes")
	if shop := catalogue.MerchantByOwner(stranger); shop == nil {
		t.Fatal("her yes after the explanation should still open the shop")
	}
}

// The buyer's pending close is a held-open question like
// any other: a picture ask is served there, and a question
// about the total is answered, before yes and no are read.
func TestPendingCloseReadByMeaning(t *testing.T) {
	_, c := setupTwitterShops(t)
	stubStageModel(t)
	buyer := "233550009121"
	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Handmade Beaded Bag")
	c.last(t, buyer)
	c.say(t, buyer, "I will take it")
	c.last(t, buyer)
	c.say(t, buyer, "Before I pay I need to see what it looks like")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "I don't have the pic of the Handmade Beaded Bag") {
		t.Fatalf("a picture ask at the close should be served, got %q", got)
	}
	if !strings.Contains(got, "Shall I send the payment link") {
		t.Fatalf("the close should be held up again, got %q", got)
	}
	c.say(t, buyer, "How much will I pay in all?")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "GH₵875") {
		t.Fatalf("her total question at the close should be answered, got %q", got)
	}
	c.say(t, buyer, "yes")
	if got := c.last(t, buyer); !strings.Contains(got, "how do you want to get it") && !strings.Contains(got, "How do you want to get it") {
		t.Fatalf("her yes after the answers should still close, got %q", got)
	}
}

// A closed sale must not strand on Seev's memory: when the
// plain order reference collides with a session Seev
// already holds under that key (the same order number
// minted again after a store reset, at another amount),
// the mint is retried on a key that has never existed and
// the order points at the session that actually opened.
func TestLinkCreationSurvivesAnIdempotencyCollision(t *testing.T) {
	dir, c := setupTwitterShops(t)
	fake := &fakeSeev{conflictFirstMint: true}
	startFakeSeev(t, fake)
	stubStageModel(t)
	buyer := "233550009130"
	agreeBagAtList(t, c, buyer)
	tap(t, buyer, "ff_self")
	c.finishDetails(t, buyer)
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Pay here to complete your order: https://pay.seevplus.com/PAY-test") {
		t.Fatalf("the sale should still close with a link, got %q", got)
	}
	if strings.Contains(got, "not coming up right now") {
		t.Fatalf("the buyer should never see the failure when the retry opened, got %q", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "data", "orders.json"))
	if err != nil {
		t.Fatalf("orders: %v", err)
	}
	var all map[string]orders.Order
	if err := json.Unmarshal(data, &all); err != nil {
		t.Fatalf("orders parse: %v", err)
	}
	o, ok := all["EFUA-0001"]
	if !ok {
		t.Fatalf("order EFUA-0001 missing: %v", all)
	}
	providerRef := ""
	if o.ProviderRef != nil {
		providerRef = *o.ProviderRef
	}
	if o.Status != "pending" || o.LinkAttempts != 2 || providerRef != "PAY-EFUA-0001" {
		t.Fatalf("the order should point at the session the fresh key opened: %+v", o)
	}
}

// A link_failed order is still an unpaid order, and every
// path that serves the buyer about paying must see it:
// asking for a new link by reference steps the status
// check aside and mints one, and asking to pay now finds
// the order and mints what was never sent.
func TestLinkFailedOrderCanStillBePaid(t *testing.T) {
	_, c := setupTwitterShops(t)
	fake := &fakeSeev{}
	startFakeSeev(t, fake)
	stubStageModel(t)
	seedFailed := func(ref, buyer string) {
		orders.Default.RecordOrder(ref, orders.Order{
			Reference: ref, Status: "link_failed", From: buyer,
			ProductName: "Handmade Beaded Bag", Price: 850, Total: 875,
			MerchantID: "efua", LinkAttempts: 1,
		})
	}

	buyer := "233550009131"
	seedFailed("EFUA-0001", buyer)
	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "New payment link for order EFUA-0001")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Pay here to complete your order: https://pay.seevplus.com/PAY-test") {
		t.Fatalf("a new-link ask on a link_failed order should mint the link, got %q", got)
	}
	if strings.Contains(got, "It is not paid yet") {
		t.Fatalf("the status readback must not answer a link ask, got %q", got)
	}
	if o := orders.Default.GetOrder("EFUA-0001"); o == nil || o.Status != "pending" || o.ProviderRef == nil || *o.ProviderRef != "PAY-EFUA-0001-2" {
		t.Fatalf("the order should stand pending on the minted session: %+v", o)
	}

	buyer2 := "233550009132"
	seedFailed("EFUA-0002", buyer2)
	c.say(t, buyer2, "SHOP-EFUA")
	c.replies(t, buyer2)
	c.say(t, buyer2, "I want to pay now")
	got = strings.Join(c.replies(t, buyer2), "\n")
	if !strings.Contains(got, "Pay here to complete your order: https://pay.seevplus.com/PAY-test") {
		t.Fatalf("asking to pay on a link_failed order should mint the link it never had, got %q", got)
	}
}
