package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The codeless buyer: she is understood, searched for, and
// bound on her pick, in place of the old wall that repeated
// the ask for a shop code at everything she said.

const discoveryCatalogue = `{"merchants":{
"luxe":{"id":"luxe","name":"LUXE","shopCode":"LUXE","ownerWhatsApp":"233265993567","payoutPhone":"233265993567","dailyGiveawayAllowance":0,"products":[
{"id":"elitebook","name":"HP EliteBook 1030 G8 x360","description":"11th Gen Core i5, 16GB RAM, 512GB SSD, like new","listPrice":6800,"floorPrice":6600,"stock":1},
{"id":"sandals","name":"Beaded Sandals","description":"Sizes 37 to 42, very quality","listPrice":80,"floorPrice":70,"stock":4}]},
"efua":{"id":"efua","name":"Efua's Store","shopCode":"EFUA","ownerWhatsApp":"233550004099","payoutPhone":"233550004099","dailyGiveawayAllowance":300,"products":[
{"id":"bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag","listPrice":850,"floorPrice":680,"stock":3}]}
}}`

// stubDiscoveryModel thinks like the charter: it reads the
// buyer's words out of the request and answers in the
// discovery JSON, the way the live model is briefed to.
func stubDiscoveryModel(t *testing.T) {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		// Only her words decide: the charter's own examples
		// mention laptops too, so match on the message alone.
		text := ""
		for _, m := range body.Messages {
			text += m.Content + "\n"
		}
		her := text
		if i := strings.LastIndex(text, "Her message:"); i >= 0 {
			her = text[i:]
		}
		lower := strings.ToLower(her)
		content := `{"intent":"unclear","reply":"Tell me what you are looking for and I will search every shop."}`
		switch {
		case strings.Contains(lower, "which shop has laptop"), strings.Contains(lower, "sells laptop"), strings.Contains(lower, "looking for laptop"):
			content = `{"intent":"search","item":"laptop","matches":[{"shop":"LUXE","product":"HP EliteBook 1030 G8 x360"}]}`
		case strings.Contains(lower, "no shop in particular"), strings.Contains(lower, "no shop on particular"):
			content = `{"intent":"no_shop","reply":"No problem at all. What are you looking to buy? I will check every shop for it."}`
		case strings.Contains(lower, "looking for a bag"):
			content = `{"intent":"search","item":"bag","matches":[{"shop":"EFUA","product":"Handmade Beaded Bag"}]}`
		case strings.Contains(lower, "show me picture"):
			content = `{"intent":"picture","matches":[{"shop":"EFUA","product":"Handmade Beaded Bag"}]}`
		case strings.Contains(lower, "the elitebook"):
			content = `{"intent":"shop","shop":"LUXE","matches":[{"shop":"LUXE","product":"HP EliteBook 1030 G8 x360"}]}`
		case strings.Contains(lower, "president of ghana"):
			content = `{"intent":"offtopic","reply":"That is outside my work here. I sell for the shops on this number, so tell me what you are looking to buy and I will search them all."}`
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

func TestDiscoveryFindsTheLaptopAcrossShops(t *testing.T) {
	dir := setupChat(t, "233265993567", discoveryCatalogue)
	stubDiscoveryModel(t)
	c := &chat{dir: dir}
	buyer := "233550007001"

	c.say(t, buyer, "Do you have any shop that sells laptop?")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "HP EliteBook 1030 G8 x360") || !strings.Contains(got, "HP EliteBook 1030 G8 x360 - GH₵6,800") {
		t.Fatalf("the one laptop anywhere should be presented directly, got %q", got)
	}
	if strings.Contains(got, "Which one do you want to look at?") {
		t.Fatalf("a single result is not a choice, got %q", got)
	}
	if strings.Contains(got, "I need to know whose shop") {
		t.Fatalf("the welcome wall came back: %q", got)
	}

	// The same question again gets the same help, not a loop
	// of welcomes: discovery answers every time.
	c.say(t, buyer, "Which shop has laptop?")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "HP EliteBook 1030 G8 x360") {
		t.Fatalf("the repeat question should be answered again, got %q", got)
	}
}

func TestDiscoveryNoParticularShopAsksWhatSheWants(t *testing.T) {
	dir := setupChat(t, "233265993567", discoveryCatalogue)
	stubDiscoveryModel(t)
	c := &chat{dir: dir}
	buyer := "233550007002"

	c.say(t, buyer, "No shop in particular")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "What are you looking to buy?") {
		t.Fatalf("no-particular-shop should open the search, got %q", got)
	}
	// And her answer is searched, not walled.
	c.say(t, buyer, "I am looking for laptop to buy")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "HP EliteBook 1030 G8 x360") {
		t.Fatalf("her want should be searched across shops, got %q", got)
	}
}

func TestDiscoveryPickBindsHerToThatShop(t *testing.T) {
	dir := setupChat(t, "233265993567", discoveryCatalogue)
	stubDiscoveryModel(t)
	c := &chat{dir: dir}
	buyer := "233550007003"

	c.say(t, buyer, "Do you have any shop that sells laptop?")
	got := strings.Join(c.replies(t, buyer), "\n")
	c.say(t, buyer, "The EliteBook")
	got += "\n" + strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Welcome to LUXE.") || !strings.Contains(got, "HP EliteBook 1030 G8 x360 - GH₵6,800") {
		t.Fatalf("she should land in LUXE with the laptop in focus, got %q", got)
	}
	if bindings.get(buyer) != "luxe" {
		t.Fatalf("the buyer should be bound to luxe, got %q", bindings.get(buyer))
	}
}

func TestDiscoveryDeclinesOffTopicAndSteersBack(t *testing.T) {
	dir := setupChat(t, "233265993567", discoveryCatalogue)
	stubDiscoveryModel(t)
	c := &chat{dir: dir}
	buyer := "233550007004"

	c.say(t, buyer, "Who is the president of Ghana?")
	got := strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "Mahama") || !strings.Contains(got, "outside my work") {
		t.Fatalf("off-topic should be declined with steering, got %q", got)
	}
}

// Without the model, the net still helps: a literal search
// finds the bag, and a bare offer binds nothing.
func TestDiscoveryNetSearchesWithoutTheModel(t *testing.T) {
	dir := setupChat(t, "233265993567", discoveryCatalogue)
	c := &chat{dir: dir}
	buyer := "233550007005"

	c.say(t, buyer, "I am looking for sandals")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Beaded Sandals") || !strings.Contains(got, "Beaded Sandals - GH₵80") {
		t.Fatalf("the net's one match should be presented directly, got %q", got)
	}
	if strings.Contains(got, "Which one do you want to look at?") {
		t.Fatalf("a single result is not a choice, got %q", got)
	}
}

// A search that finds exactly one product anywhere takes
// her straight to it: no "which one" over a choice of one,
// and she lands inside the shop, able to haggle at once.
func TestDiscoverySingleMatchPresentsDirectly(t *testing.T) {
	dir := setupChat(t, "233265993567", discoveryCatalogue)
	c := &chat{dir: dir}
	buyer := "233550007009"

	c.say(t, buyer, "I am looking for sandals")
	got := strings.Join(c.replies(t, buyer), "\n")
	if strings.Contains(got, "Which one do you want to look at?") {
		t.Fatalf("one match anywhere is not a choice, got %q", got)
	}
	if !strings.Contains(got, "LUXE has it:") || !strings.Contains(got, "Beaded Sandals - GH₵80") {
		t.Fatalf("the one match should be put in front of her, got %q", got)
	}
	if bindings.get(buyer) == "luxe" {
		t.Fatalf("presenting the match must not bind her before her pick, got %q", got)
	}
	c.say(t, buyer, "The sandals")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Welcome to LUXE") {
		t.Fatalf("her pick should take her into the shop, got %q", got)
	}
	c.say(t, buyer, "I can pay 70")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Beaded Sandals") || !strings.Contains(got, "GH₵") {
		t.Fatalf("her offer should be haggled inside the shop, got %q", got)
	}
}

// Her pick from shown results binds her, with no model
// needed: a selection from a presented list is mechanics.
func TestDiscoveryPickFromShownResultsBindsWithoutTheModel(t *testing.T) {
	dir := setupChat(t, "233265993567", discoveryCatalogue)
	c := &chat{dir: dir}
	buyer := "233550007006"

	c.say(t, buyer, "I am looking for sandals")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Beaded Sandals") {
		t.Fatalf("the search should show the sandals, got %q", got)
	}
	c.say(t, buyer, "The sandals")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Welcome to LUXE.") || bindings.get(buyer) != "luxe" {
		t.Fatalf("the pick should bind her to LUXE, got %q (bound %q)", got, bindings.get(buyer))
	}
	// An ordinal pick works the same way for a fresh buyer.
	buyer2 := "233550007007"
	c.say(t, buyer2, "I am looking for beaded sandals")
	c.replies(t, buyer2)
	c.say(t, buyer2, "The first one")
	if bindings.get(buyer2) != "luxe" {
		t.Fatalf("the ordinal pick should bind her to LUXE, got %q", bindings.get(buyer2))
	}
}

// The single match is presented with its photo when the
// product has one: the card she would have picked, shown
// instead of listed.
func TestDiscoverySingleMatchShowsThePhoto(t *testing.T) {
	stubPhotoMedia(t)
	cat := `{"merchants":{"luxe":{"id":"luxe","name":"LUXE","shopCode":"LUXE","ownerWhatsApp":"233265993567","payoutPhone":"233265993567","dailyGiveawayAllowance":0,"products":[{"id":"sandals","name":"Beaded Sandals","description":"Sizes 37 to 42","listPrice":80,"floorPrice":70,"stock":4,"photos":["photos/sandals-0.jpg"]}]}}}`
	dir := setupChat(t, "233265993567", cat)
	if err := os.MkdirAll(filepath.Join(dir, "data", "photos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data", "photos", "sandals-0.jpg"), []byte("fake-jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &chat{dir: dir}
	buyer := "233550007011"
	c.say(t, buyer, "I am looking for sandals")
	recs := doorRecords(t, dir, buyer)
	images := 0
	for _, rec := range recs {
		if rec.Kind == "image" {
			images++
		}
		if strings.Contains(rec.Body, "Which one do you want to look at?") {
			t.Fatalf("a single match must not be offered as a choice: %+v", recs)
		}
	}
	if images != 1 {
		t.Fatalf("the single match should arrive with its photo, got %d image sends in %+v", images, recs)
	}
}

const twoHeelShops = `{"merchants":{
"ashop":{"id":"ashop","name":"Ama's Heels","shopCode":"AMA","ownerWhatsApp":"233550009001","dailyGiveawayAllowance":300,"products":[{"id":"stiletto","name":"Stiletto Heel","description":"Tall stiletto heel.","listPrice":300,"floorPrice":250,"stock":2}]},
"zshop":{"id":"zshop","name":"Zed's Heels","shopCode":"ZED","ownerWhatsApp":"233550009002","dailyGiveawayAllowance":300,"products":[{"id":"blockheel","name":"Block Heel","description":"Low block heel.","listPrice":120,"floorPrice":100,"stock":2}]}
}}`

// A price wish is arithmetic, not a reading: when she asks
// for the cheapest, code orders the net's hits by price,
// so the GH₵120 heel leads even though the GH₵300 one
// stands first in the catalogue walk.
func TestDiscoveryNetCheapestLeadsByPrice(t *testing.T) {
	dir := setupChat(t, "233550009002", twoHeelShops)
	c := &chat{dir: dir}
	buyer := "233550009101"

	c.say(t, buyer, "What is the cheapest heel?")
	got := strings.Join(c.replies(t, buyer), "\n")
	cheap := strings.Index(got, "Block Heel")
	dear := strings.Index(got, "Stiletto Heel")
	if cheap < 0 || dear < 0 {
		t.Fatalf("both heels should be listed, got %q", got)
	}
	if cheap > dear {
		t.Fatalf("the cheapest must lead the list, got %q", got)
	}
}

// "Cheapest pic" on the net road shows her the cheapest
// one's picture inside its shop, instead of listing both
// and making her pick blind.
func TestDiscoveryNetCheapestPicShowsTheCheapest(t *testing.T) {
	dir := setupChat(t, "233550009002", twoHeelShops)
	c := &chat{dir: dir}
	buyer := "233550009102"

	c.say(t, buyer, "send pic of the cheapest heel")
	got := strings.Join(c.replies(t, buyer), "\n")
	if bindings.get(buyer) != "zshop" {
		t.Fatalf("she should land in the cheapest heel's shop, bound to %q, got %q", bindings.get(buyer), got)
	}
	if !strings.Contains(got, "Block Heel") {
		t.Fatalf("the cheapest heel should be the one shown, got %q", got)
	}
	if strings.Contains(got, "Which one do you want to look at?") {
		t.Fatalf("a picture ask is not a list, got %q", got)
	}
}
