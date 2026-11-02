package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// The cutout offer, proven the way her ruling reads: a
// photo is stored and shown exactly as sent, no edit is
// made at acceptance, and the cutout runs only on her
// instruction — her yes to the one offer, or her own
// words. Her no, her silence and her other business each
// leave the original standing.

// stubCutoutRun makes the edit's background run execute
// inline, so the outcome lands inside the test's turn.
func stubCutoutRun(t *testing.T) {
	t.Helper()
	old := cutoutGoFn
	cutoutGoFn = func(f func()) { f() }
	t.Cleanup(func() { cutoutGoFn = old })
}

// countingSegment stubs the segmentation service with a
// call count and a record of the prompts it was given,
// answering with the fixed cutout (or failing, when ok
// is false).
func countingSegment(t *testing.T, cut []byte, ok bool) (calls *int, prompts *[]string) {
	t.Helper()
	calls = new(int)
	prompts = new([]string)
	stubSegment(t, func(ctx context.Context, photo []byte, prompt, format string) ([]byte, bool) {
		*calls++
		*prompts = append(*prompts, prompt)
		if !ok {
			return nil, false
		}
		return cut, true
	})
	return calls, prompts
}

// allOutboxRecords reads every dry-run outbox record for one
// recipient, from the start of the test.
func allOutboxRecords(t *testing.T, dir, to string) []outboxRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "data", "outbox.jsonl"))
	if err != nil {
		return nil
	}
	var mine []outboxRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec outboxRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("outbox line not JSON: %v", err)
		}
		if rec.To == to {
			mine = append(mine, rec)
		}
	}
	return mine
}

// countOffers counts the edit offers sent to a number in
// the whole test: the offer is the one buttons message
// that proposes the white background.
func countOffers(t *testing.T, dir, to string) int {
	t.Helper()
	n := 0
	for _, rec := range allOutboxRecords(t, dir, to) {
		if rec.Kind == "buttons" && strings.Contains(rec.Body, "clean white background") && strings.Contains(rec.Body, "Shall I?") {
			n++
		}
	}
	return n
}

// armedCutoutOffer returns the edit offer standing in
// front of the merchant, failing the test when none is.
func armedCutoutOffer(t *testing.T, owner string) *YesNoQuestion {
	t.Helper()
	s := sessions.get(sessionKey(MerchantID, owner))
	if s == nil || s.YesNoQ == nil || s.YesNoQ.Kind != cutoutKind {
		t.Fatalf("an edit offer should be armed, got %+v", s)
	}
	return s.YesNoQ
}

// saveSlidesWithChat is saveSlidesListing keeping the
// chat, so the replies around the save stay readable.
func saveSlidesWithChat(t *testing.T, owner string) (string, *chat) {
	t.Helper()
	stubPhotoMedia(t)
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}
	c.say(t, owner, "B Buckle Slides\n\nSizes 37 to 42")
	c.replies(t, owner)
	HandleInbound(Inbound{From: owner, ImageID: "slides-a"})
	c.replies(t, owner)
	c.say(t, owner, "75")
	c.replies(t, owner)
	c.say(t, owner, "70")
	saved := strings.Join(c.replies(t, owner), "\n")
	if strings.Contains(saved, "by measure") {
		c.say(t, owner, "whole")
		saved = strings.Join(c.replies(t, owner), "\n")
	}
	if !strings.Contains(saved, "Saved ✅ B Buckle Slides - GH₵75, last price GH₵70") {
		t.Fatalf("the compact save did not land: %q", saved)
	}
	return dir, c
}

func storedBytes(t *testing.T, dir string, p *catalogue.Product) []byte {
	t.Helper()
	if len(p.Photos) != 1 {
		t.Fatalf("the listing should hold one photo, got %v", p.Photos)
	}
	data, err := os.ReadFile(filepath.Join(dir, "data", p.Photos[0]))
	if err != nil {
		t.Fatalf("read the stored photo: %v", err)
	}
	return data
}

func originalBytes(t *testing.T, dir string, p *catalogue.Product) ([]byte, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "data", originalPhotoRel(p.Photos[0])))
	if err != nil {
		return nil, false
	}
	return data, true
}

func TestCutoutAcceptanceMakesNoCallAndOffersOnce(t *testing.T) {
	t.Setenv("SAM_SEGMENT_URL", "http://segment.invalid/cut")
	cut := makeCutoutJPEG(t)
	calls, _ := countingSegment(t, cut, true)
	owner := "233550003201"
	dir, _ := saveSlidesWithChat(t, owner)

	if *calls != 0 {
		t.Fatalf("acceptance made %d segmentation calls, want none", *calls)
	}
	slides := listedSlides(t)
	if got := storedBytes(t, dir, slides); string(got) != "fake-jpeg-bytes-slides-a" {
		t.Fatalf("the stored photo should be the photo as sent, got %d bytes", len(got))
	}
	if _, ok := originalBytes(t, dir, slides); ok {
		t.Fatal("no original sibling should exist before any edit is instructed")
	}
	if n := countOffers(t, dir, owner); n != 1 {
		t.Fatalf("the edit should be offered exactly once, got %d offers", n)
	}
	for _, rec := range allOutboxRecords(t, dir, owner) {
		if rec.Kind == "buttons" && strings.Contains(rec.Body, "Shall I?") {
			if len(rec.Buttons) != 2 || rec.Buttons[0] != "Yes" || rec.Buttons[1] != "No" {
				t.Fatalf("the offer's buttons = %v, want [Yes No]", rec.Buttons)
			}
			if !strings.Contains(rec.Body, "B Buckle Slides") {
				t.Fatalf("the offer should name its product, got %q", rec.Body)
			}
		}
	}
	q := armedCutoutOffer(t, owner)
	if q.Ref != slides.ID {
		t.Fatalf("the offer should be about the slides %q, got ref %q", slides.ID, q.Ref)
	}
}

func TestCutoutSilentWithoutAnEndpoint(t *testing.T) {
	os.Unsetenv("SAM_SEGMENT_URL")
	cut := makeCutoutJPEG(t)
	calls, _ := countingSegment(t, cut, true)
	owner := "233550003202"
	dir, _ := saveSlidesWithChat(t, owner)

	if *calls != 0 {
		t.Fatal("the service was called with no endpoint configured")
	}
	if n := countOffers(t, dir, owner); n != 0 {
		t.Fatalf("with the feature off no offer should be made, got %d", n)
	}
	slides := listedSlides(t)
	if got := storedBytes(t, dir, slides); string(got) != "fake-jpeg-bytes-slides-a" {
		t.Fatal("the photo should stay as sent")
	}
}

func TestCutoutYesTapCutsAndSwaps(t *testing.T) {
	t.Setenv("SAM_SEGMENT_URL", "http://segment.invalid/cut")
	stubCutoutRun(t)
	cut := makeCutoutJPEG(t)
	calls, prompts := countingSegment(t, cut, true)
	owner := "233550003203"
	dir, c := saveSlidesWithChat(t, owner)

	q := armedCutoutOffer(t, owner)
	HandleInbound(Inbound{From: owner, ButtonID: "ynq_" + q.ID + "_y"})
	joined := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(joined, "Done ✅") || !strings.Contains(joined, "clean white background") {
		t.Fatalf("a yes should land the cutout and say so, got %q", joined)
	}
	if *calls != 1 {
		t.Fatalf("a yes should make exactly one segmentation call, got %d", *calls)
	}
	if len(*prompts) != 1 || (*prompts)[0] != "B Buckle Slides" {
		t.Fatalf("the cut should be asked for under the product's own name, got %v", *prompts)
	}
	slides := listedSlides(t)
	if got := storedBytes(t, dir, slides); !bytes.Equal(got, cut) {
		t.Fatal("the stored photo is not the cutout she approved")
	}
	orig, ok := originalBytes(t, dir, slides)
	if !ok || string(orig) != "fake-jpeg-bytes-slides-a" {
		t.Fatal("the original should be kept untouched beside the cutout")
	}
	if s := sessions.get(sessionKey(MerchantID, owner)); s != nil && s.YesNoQ != nil {
		t.Fatal("the answered offer should be disarmed")
	}
}

func TestCutoutTypedYesCutsAndSwaps(t *testing.T) {
	t.Setenv("SAM_SEGMENT_URL", "http://segment.invalid/cut")
	stubCutoutRun(t)
	cut := makeCutoutJPEG(t)
	calls, _ := countingSegment(t, cut, true)
	owner := "233550003204"
	dir, c := saveSlidesWithChat(t, owner)

	c.say(t, owner, "yes")
	joined := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(joined, "Done ✅") {
		t.Fatalf("a typed yes should run the cut, got %q", joined)
	}
	if *calls != 1 {
		t.Fatalf("a typed yes should make exactly one call, got %d", *calls)
	}
	slides := listedSlides(t)
	if got := storedBytes(t, dir, slides); !bytes.Equal(got, cut) {
		t.Fatal("the stored photo is not the cutout she approved")
	}
}

func TestCutoutNoTapKeepsTheOriginal(t *testing.T) {
	t.Setenv("SAM_SEGMENT_URL", "http://segment.invalid/cut")
	stubCutoutRun(t)
	cut := makeCutoutJPEG(t)
	calls, _ := countingSegment(t, cut, true)
	owner := "233550003205"
	dir, c := saveSlidesWithChat(t, owner)

	q := armedCutoutOffer(t, owner)
	HandleInbound(Inbound{From: owner, ButtonID: "ynq_" + q.ID + "_n"})
	joined := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(joined, "stays exactly as you sent it") {
		t.Fatalf("a no should be answered plainly, got %q", joined)
	}
	if *calls != 0 {
		t.Fatalf("a no must make no segmentation call, got %d", *calls)
	}
	slides := listedSlides(t)
	if got := storedBytes(t, dir, slides); string(got) != "fake-jpeg-bytes-slides-a" {
		t.Fatal("after a no the photo must stand as sent")
	}
	if _, ok := originalBytes(t, dir, slides); ok {
		t.Fatal("no original sibling should exist when no edit was made")
	}
}

func TestCutoutOfferLapsesOnOtherBusiness(t *testing.T) {
	t.Setenv("SAM_SEGMENT_URL", "http://segment.invalid/cut")
	stubCutoutRun(t)
	cut := makeCutoutJPEG(t)
	calls, _ := countingSegment(t, cut, true)
	owner := "233550003206"
	dir, c := saveSlidesWithChat(t, owner)

	// Other business: the offer lapses, unasked again.
	c.say(t, owner, "my catalogue")
	c.replies(t, owner)
	if s := sessions.get(sessionKey(MerchantID, owner)); s != nil && s.YesNoQ != nil {
		t.Fatal("the offer should have lapsed on other business")
	}
	if n := countOffers(t, dir, owner); n != 1 {
		t.Fatalf("the offer must not be repeated, got %d offers", n)
	}
	// A yes after the lapse belongs to nothing: no cut.
	c.say(t, owner, "yes")
	c.replies(t, owner)
	if *calls != 0 {
		t.Fatalf("a yes after the lapse must not cut, got %d calls", *calls)
	}
	slides := listedSlides(t)
	if got := storedBytes(t, dir, slides); string(got) != "fake-jpeg-bytes-slides-a" {
		t.Fatal("the photo must stand as sent after a lapsed offer")
	}
}

func TestCutoutFailureAfterYesLeavesTheOriginal(t *testing.T) {
	t.Setenv("SAM_SEGMENT_URL", "http://segment.invalid/cut")
	stubCutoutRun(t)
	calls, _ := countingSegment(t, nil, false)
	owner := "233550003207"
	dir, c := saveSlidesWithChat(t, owner)

	q := armedCutoutOffer(t, owner)
	HandleInbound(Inbound{From: owner, ButtonID: "ynq_" + q.ID + "_y"})
	joined := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(joined, "could not cut out") || !strings.Contains(joined, "stays exactly as you sent it") {
		t.Fatalf("a failed edit should be told plainly, got %q", joined)
	}
	if *calls != 1 {
		t.Fatalf("the instructed edit should have been attempted once, got %d calls", *calls)
	}
	slides := listedSlides(t)
	if got := storedBytes(t, dir, slides); string(got) != "fake-jpeg-bytes-slides-a" {
		t.Fatal("a failed cutout must leave the photo exactly as sent")
	}
	if _, ok := originalBytes(t, dir, slides); ok {
		t.Fatal("no original sibling should exist when the edit failed")
	}
}

func TestCutoutInstructionRunsTheCut(t *testing.T) {
	t.Setenv("SAM_SEGMENT_URL", "http://segment.invalid/cut")
	stubCutoutRun(t)
	cut := makeCutoutJPEG(t)
	calls, _ := countingSegment(t, cut, true)
	owner := "233550003208"
	dir, c := saveSlidesWithChat(t, owner)

	// The offer is declined first, so the instruction is
	// the only road the edit can take.
	q := armedCutoutOffer(t, owner)
	HandleInbound(Inbound{From: owner, ButtonID: "ynq_" + q.ID + "_n"})
	c.replies(t, owner)

	merchant := catalogue.GetMerchant(MerchantID)
	if !inTurn(owner, func() bool {
		return actOnMerchantUnderstanding(owner, merchant, MerchantID, "cut out the slides photo", &understood{Intent: intentCutout, Item: "B Buckle Slides"})
	}) {
		t.Fatal("her instruction should be served")
	}
	joined := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(joined, "Done ✅") {
		t.Fatalf("her instruction should land the cutout, got %q", joined)
	}
	if *calls != 1 {
		t.Fatalf("her instruction should make exactly one call, got %d", *calls)
	}
	slides := listedSlides(t)
	if got := storedBytes(t, dir, slides); !bytes.Equal(got, cut) {
		t.Fatal("the stored photo is not the cutout she instructed")
	}
	orig, ok := originalBytes(t, dir, slides)
	if !ok || string(orig) != "fake-jpeg-bytes-slides-a" {
		t.Fatal("the original should be kept untouched beside the cutout")
	}
}

const shopWithTwoBeaded = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Beaded Bag","description":"Hand-beaded evening bag.","listPrice":850,"floorPrice":680,"stock":3},{"id":"beaded-clutch","name":"Beaded Clutch","description":"Hand-beaded clutch.","listPrice":450,"floorPrice":380,"stock":2}]}}}`

func TestCutoutInstructionAsksWhichOne(t *testing.T) {
	t.Setenv("SAM_SEGMENT_URL", "http://segment.invalid/cut")
	stubCutoutRun(t)
	cut := makeCutoutJPEG(t)
	calls, _ := countingSegment(t, cut, true)
	owner := "233550003209"
	dir := setupChat(t, owner, shopWithTwoBeaded)
	c := &chat{dir: dir}
	merchant := catalogue.GetMerchant(MerchantID)

	serve := func(msg string, u *understood) string {
		t.Helper()
		if !inTurn(owner, func() bool { return actOnMerchantUnderstanding(owner, merchant, MerchantID, msg, u) }) {
			t.Fatalf("the instruction %q should be taken up", msg)
		}
		return strings.Join(c.replies(t, owner), "\n")
	}

	if got := serve("cut out a photo", &understood{Intent: intentCutout}); !strings.Contains(got, "Which product's photo shall I cut out?") {
		t.Fatalf("an unnamed instruction should be asked which one, got %q", got)
	}
	if got := serve("cut out the unicycle", &understood{Intent: intentCutout, Item: "Unicycle"}); !strings.Contains(got, "cannot find Unicycle") {
		t.Fatalf("an unknown product should be asked which one, got %q", got)
	}
	if got := serve("cut out the beaded one", &understood{Intent: intentCutout, Item: "Beaded"}); !strings.Contains(got, "Beaded Bag") || !strings.Contains(got, "Beaded Clutch") || !strings.Contains(got, "Which one's photo shall I cut out?") {
		t.Fatalf("an ambiguous name should name its candidates and ask once, got %q", got)
	}
	if *calls != 0 {
		t.Fatalf("no edit may run before she names the product, got %d calls", *calls)
	}
}

// A burst's pictured items are offered one at a time:
// each answer settles the item it names, a no leaves its
// photo standing, and the next offer follows the answer.
func TestBurstCutoutOffersChainPerItem(t *testing.T) {
	t.Setenv("SAM_SEGMENT_URL", "http://segment.invalid/cut")
	stubCutoutRun(t)
	stubPhotoMedia(t)
	cut := makeCutoutJPEG(t)
	calls, _ := countingSegment(t, cut, true)
	owner := "233550003210"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, owner, "Corner Rack\nSteel frame with 4 tiers\nGH₵270")
	c.photo(t, owner, "Dinner Set\n16 pieces for the family\nGH₵180")
	c.replies(t, owner)
	c.say(t, owner, "250")
	c.replies(t, owner)
	c.say(t, owner, "whole")
	joined := c.joined(t, owner)
	if !strings.Contains(joined, "last price for each") || !strings.Contains(joined, "Dinner Set") {
		t.Fatalf("saving the open listing should present the batch, got %q", joined)
	}
	c.say(t, owner, "170")
	joined = c.joined(t, owner)
	if !strings.Contains(joined, "Added ✅") {
		t.Fatalf("the batch should save, got %q", joined)
	}

	rack := catalogue.FindByName(MerchantID, "Corner Rack")
	dinner := catalogue.FindByName(MerchantID, "Dinner Set")
	if rack == nil || dinner == nil {
		t.Fatalf("both items should be listed, rack=%+v dinner=%+v", rack, dinner)
	}
	// The first offer is the first item saved, and the
	// batch's price question never shared its turn.
	q := armedCutoutOffer(t, owner)
	if q.Ref != rack.ID {
		t.Fatalf("the first offer should be the Corner Rack's, got ref %q", q.Ref)
	}
	if !strings.Contains(q.Body, "Corner Rack") {
		t.Fatalf("the offer should name its product, got %q", q.Body)
	}
	if *calls != 0 {
		t.Fatalf("no edit may run before she answers, got %d calls", *calls)
	}

	c.say(t, owner, "no")
	joined = c.joined(t, owner)
	if !strings.Contains(joined, "Corner Rack photo stays exactly as you sent it") {
		t.Fatalf("a no should settle the rack plainly, got %q", joined)
	}
	q = armedCutoutOffer(t, owner)
	if q.Ref != dinner.ID {
		t.Fatalf("the next offer should be the Dinner Set's, got ref %q", q.Ref)
	}
	if *calls != 0 {
		t.Fatalf("a no must make no call, got %d", *calls)
	}
	if got := storedBytes(t, dir, rack); string(got) != "fake-jpeg-bytes-img-throw-1" {
		t.Fatal("the rack's photo must stand as sent after its no")
	}

	c.say(t, owner, "yes")
	joined = c.joined(t, owner)
	if !strings.Contains(joined, "Done ✅") || !strings.Contains(joined, "Dinner Set") {
		t.Fatalf("a yes should cut the Dinner Set and say so, got %q", joined)
	}
	if *calls != 1 {
		t.Fatalf("the yes should make exactly one call, got %d", *calls)
	}
	if got := storedBytes(t, dir, dinner); !bytes.Equal(got, cut) {
		t.Fatal("the Dinner Set's stored photo is not its approved cutout")
	}
	orig, ok := originalBytes(t, dir, dinner)
	if !ok || string(orig) != "fake-jpeg-bytes-img-throw-2" {
		t.Fatal("the Dinner Set's original should be kept beside its cutout")
	}
	if got := storedBytes(t, dir, rack); string(got) != "fake-jpeg-bytes-img-throw-1" {
		t.Fatal("the rack's photo must still stand as sent")
	}
}
