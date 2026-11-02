package agent

// Answer binding, replaying the three live scenes of
// 1-2 November: the merchant shared a catalogue link, in
// its share-text and bare shapes, and a product link; the
// agent answered each with a drafted clarifying question;
// and her short answers to those questions — "Yes", "New",
// "Yes add" — were read fresh, with the question nowhere in
// the reading, so they landed on the allowance clarifier,
// on a greeting, or on the same question again, never on
// the choice she had picked. The law these tests hold: a
// question the agent asks the merchant is registered, and
// her next message is resolved inside that question's
// frame, by a reading that is given the question itself.
//
// The stub model below is context-sensitive the way the
// live model is: handed the merchant's words WITHOUT the
// question it just asked, it produces the journal's
// readings (the allowance clarifier, the fresh-start
// shelf reading); handed the question, it reads her answer
// against the choices the question offered. The tests
// therefore fail on a resolver whose reading never sees
// the question, for the journal's own reasons.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	bindScene1Q = "Do you want me to show your catalogue from the shelf here, or should I use this WhatsApp link another way?"
	bindScene3Q = "Just to confirm, do you want me to add products from this catalogue link to your shelf, or would you like me to show you your current catalogue?"
	bindScene2Q = "Should I add this as a new product or update something already on your shelf?"
	bindAllowQ  = "Just to be clear, do you want me to set the daily giveaway allowance to 300, or did you mean yes to something else?"
	bindAllowQ2 = "Just to be sure, do you want me to add a new product to your shelf, or to set your daily giveaway allowance?"
)

func stubAnswerBindingModel(t *testing.T) {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		text := ""
		for _, m := range body.Messages {
			text += m.Content + "\n"
		}
		content := "Here is today so far."
		if strings.Contains(text, "thinking of a WhatsApp sales agent") {
			her := text
			if i := strings.LastIndex(text, "The owner's message:"); i >= 0 {
				her = strings.TrimSpace(text[i+len("The owner's message:"):])
			}
			switch {
			case strings.Contains(her, "Follow this link to view our catalogue"):
				content = `{"intent":"unclear","reply":"` + bindScene1Q + `"}`
			case strings.TrimSpace(her) == "https://wa.me/c/233550009567":
				content = `{"intent":"unclear","reply":"` + bindScene3Q + `"}`
			case strings.Contains(her, "wa.me/p/"):
				content = `{"intent":"unclear","reply":"` + bindScene2Q + `"}`
			case her == "Yes" && strings.Contains(text, bindScene1Q):
				// Given the question, "yes" picks the
				// branch it affirms: show the catalogue.
				content = `{"intent":"shelf"}`
			case her == "Yes" && strings.Contains(text, bindScene3Q):
				// A bare yes picks neither branch of
				// this question: a person asks it again.
				content = `{"intent":"unclear","reply":"` + bindScene3Q + `"}`
			case her == "Yes":
				// No question in the reading: the
				// journal's allowance clarifier.
				content = `{"intent":"unclear","reply":"` + bindAllowQ + `"}`
			case her == "Yes add" && strings.Contains(text, bindScene3Q):
				content = `{"intent":"new_listing"}`
			case her == "Yes add":
				content = `{"intent":"unclear","reply":"` + bindAllowQ2 + `"}`
			case her == "New" && strings.Contains(text, bindScene2Q):
				content = `{"intent":"new_listing"}`
			case her == "New":
				content = `{"intent":"shelf"}`
			default:
				// A reading the platform cannot use:
				// the frame must release, not trap.
				content = "not a reading"
			}
		} else if strings.TrimSpace(text) == "New" || strings.HasSuffix(strings.TrimSpace(text), "\nNew") {
			// The day answer's drafting call, reached
			// only when every frame has fallen through:
			// the journal's fresh greeting.
			content = "Hello. I am Stall. How can I help you today?"
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

func TestCatalogueShareTextYesShowsTheCatalogue(t *testing.T) {
	owner := "233550009201"
	dir := setupChat(t, owner, shopWithBag)
	stubAnswerBindingModel(t)
	c := &chat{dir: dir}

	c.say(t, owner, "Follow this link to view our catalogue on WhatsApp: https://wa.me/c/233550009567")
	got := c.joined(t, owner)
	if !strings.Contains(got, bindScene1Q) {
		t.Fatalf("the link should be met with the clarifying question, got %q", got)
	}
	s := sessions.get(sessionKey(MerchantID, owner))
	if s == nil || s.PendingMerchantQ == nil || s.PendingMerchantQ.Kind != "drafted" {
		t.Fatalf("the drafted question should stand as the pending question, got %+v", s)
	}

	c.say(t, owner, "Yes")
	got = c.joined(t, owner)
	if strings.Contains(got, "did you mean") || strings.Contains(got, "do you want me to set") {
		t.Fatalf("an answer owed to the catalogue question must never land on the allowance clarifier, got %q", got)
	}
	if !strings.Contains(got, "Here is your catalogue:") || !strings.Contains(got, "Handmade Beaded Bag") {
		t.Fatalf("her yes should be read against the question and show her catalogue, got %q", got)
	}
	if s.PendingMerchantQ != nil {
		t.Fatalf("the question should be settled by its answer, got %+v", s.PendingMerchantQ)
	}
}

func TestCatalogueLinkYesAddStartsTheAddPath(t *testing.T) {
	owner := "233550009202"
	dir := setupChat(t, owner, shopWithBag)
	stubAnswerBindingModel(t)
	c := &chat{dir: dir}

	c.say(t, owner, "https://wa.me/c/233550009567")
	got := c.joined(t, owner)
	if !strings.Contains(got, bindScene3Q) {
		t.Fatalf("the bare link should be met with the clarifying question, got %q", got)
	}
	s := sessions.get(sessionKey(MerchantID, owner))
	if s == nil || s.PendingMerchantQ == nil {
		t.Fatalf("the drafted question should stand as the pending question, got %+v", s)
	}

	// A bare yes picks neither branch: the frame may ask
	// its own question again, and nothing else's.
	c.say(t, owner, "Yes")
	got = c.joined(t, owner)
	if strings.Contains(got, "did you mean") || strings.Contains(got, "do you want me to set") || strings.Contains(got, "or to set your daily") {
		t.Fatalf("her yes must not be read as an allowance answer, got %q", got)
	}
	if !strings.Contains(got, bindScene3Q) {
		t.Fatalf("an ambiguous yes should be met with the same question again, got %q", got)
	}

	c.say(t, owner, "Yes add")
	got = c.joined(t, owner)
	if strings.Contains(got, "did you mean") || strings.Contains(got, "do you want me to set") || strings.Contains(got, "or to set your daily") {
		t.Fatalf("her choice must not land on the allowance clarifier, got %q", got)
	}
	if !strings.Contains(got, "What is it called?") {
		t.Fatalf("choosing add should open the add path at its first question, got %q", got)
	}
	if s.Onboarding == nil || s.Onboarding.Step != "name" {
		t.Fatalf("the add path should stand open at its name step, got %+v", s.Onboarding)
	}
	if s.PendingMerchantQ != nil {
		t.Fatalf("the question should be settled, got %+v", s.PendingMerchantQ)
	}
}

func TestProductLinkNewStartsTheAddPath(t *testing.T) {
	owner := "233550009203"
	dir := setupChat(t, owner, shopWithBag)
	stubAnswerBindingModel(t)
	c := &chat{dir: dir}

	c.say(t, owner, "https://wa.me/p/30102436722679799/233550009567")
	got := c.joined(t, owner)
	if !strings.Contains(got, bindScene2Q) {
		t.Fatalf("the product link should be met with its clarifying question, got %q", got)
	}

	c.say(t, owner, "New")
	got = c.joined(t, owner)
	if strings.Contains(got, "Hello. I am Stall") {
		t.Fatalf("her answer must not fall out of the frame into a fresh greeting, got %q", got)
	}
	if !strings.Contains(got, "What is it called?") {
		t.Fatalf("choosing new should start the add path, got %q", got)
	}
	s := sessions.get(sessionKey(MerchantID, owner))
	if s == nil || s.Onboarding == nil {
		t.Fatalf("the add path should stand open, got %+v", s)
	}
}

func TestDraftedFrameReleasesATaughtCommand(t *testing.T) {
	owner := "233550009204"
	dir := setupChat(t, owner, shopWithBag)
	stubAnswerBindingModel(t)
	c := &chat{dir: dir}

	c.say(t, owner, "https://wa.me/c/233550009567")
	c.replies(t, owner)
	s := sessions.get(sessionKey(MerchantID, owner))
	if s == nil || s.PendingMerchantQ == nil {
		t.Fatalf("the drafted question should stand, got %+v", s)
	}

	// A taught command the thinking cannot read as the
	// question's answer is not trapped by the frame: the
	// question is spent and the command is served.
	c.say(t, owner, "set allowance 300")
	got := c.joined(t, owner)
	if !strings.Contains(got, "Done. Your daily giveaway allowance is now") {
		t.Fatalf("the command should be served, got %q", got)
	}
	if s.PendingMerchantQ != nil {
		t.Fatalf("the frame should be released, got %+v", s.PendingMerchantQ)
	}
}
