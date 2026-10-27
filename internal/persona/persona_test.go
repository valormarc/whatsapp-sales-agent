package persona

import (
	"regexp"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/daybook"
)

var (
	merchant = catalogue.Merchant{Name: "Efua's Store"}
	bag      = catalogue.Product{
		Name:        "Handmade Beaded Bag",
		Description: "Hand-beaded bag, two days of work in every stitch.",
		ListPrice:   850,
		FloorPrice:  680,
	}
)

func match(t *testing.T, pattern, text string) {
	t.Helper()
	if !regexp.MustCompile(pattern).MatchString(text) {
		t.Fatalf("pattern %q not found in prompt:\n%s", pattern, text)
	}
}

func TestPromptNamesShopAndTeachesStoryBeforePrice(t *testing.T) {
	prompt := BuildSystemPrompt(Context{Merchant: merchant, Catalogue: []catalogue.Product{bag}})
	match(t, `Efua's Store`, prompt)
	match(t, `Story before price`, prompt)
	match(t, `Handmade Beaded Bag`, prompt)
}

func TestStopGivenPrivatelyAndSecrecyInstructed(t *testing.T) {
	p := bag
	prompt := BuildSystemPrompt(Context{
		Merchant: merchant,
		Product:  &p,
		Bounds:   &Bounds{Ask: 850, Stop: 721},
	})
	match(t, `GH₵721`, prompt) // the model needs its bound
	if !regexp.MustCompile(`(?i)never reveal`).MatchString(prompt) {
		t.Fatal("prompt must instruct secrecy")
	}
	match(t, `Do not name the stop`, prompt)
}

func TestPromptTeachesScoutQuestionsAndTwoStages(t *testing.T) {
	prompt := BuildSystemPrompt(Context{Merchant: merchant})
	match(t, `for resale`, prompt)
	match(t, `Two stages`, prompt)
	if !regexp.MustCompile(`(?i)keep the door open`).MatchString(prompt) {
		t.Fatal("prompt must teach keeping the door open")
	}
}

func TestDayBookStateSetsFirmnessInPrompt(t *testing.T) {
	day := daybook.State{Sales: 2, GivenAway: 120, Allowance: 300}
	prompt := BuildSystemPrompt(Context{Merchant: merchant, Day: &day})
	match(t, `GH₵120 given away`, prompt)
	match(t, `GH₵180 of today's giveaway allowance left`, prompt)
}

func TestFrameCounterAnchorsAgainstListPrice(t *testing.T) {
	line := FrameCounter(850, 780, bag.Name)
	want := "From GH₵850, I have come down to GH₵780 for the Handmade Beaded Bag."
	if line != want {
		t.Fatalf("got %q, want %q", line, want)
	}
}

func TestFrameCounterPlainWhenNoMovement(t *testing.T) {
	line := FrameCounter(850, 850, bag.Name)
	if !strings.HasPrefix(line, "I can do GH₵850") {
		t.Fatalf("got %q", line)
	}
}
