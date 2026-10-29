package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

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
		if strings.Contains(system, "intent reader for a pending sale") {
			content = `{"intent":"answer"}`
			switch {
			case strings.Contains(her, "see what it looks like"), strings.Contains(her, "not buying without pic"), strings.Contains(her, "show me pic"):
				content = `{"intent":"picture"}`
			case strings.Contains(her, "forget it"), strings.Contains(her, "drop the whole thing"):
				content = `{"intent":"cancel"}`
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
