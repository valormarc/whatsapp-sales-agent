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
	"github.com/valormarc/hackathon-agent/internal/seevwebhook"
)

// Two shops on the one shared number: the real world model. These
// tests run two seeded shops (Efua's Store, code EFUA, and Ama's
// Stall, code AMA) with different owners, payout phones, products,
// prices and floors, and prove the isolation I named: no
// product, price, haggle, day book, order or notice may cross from
// one shop into the other.

const ownerEfua = "233550003001"
const ownerAma = "233550003002"

const twoShopCatalogue = `{"merchants":{` +
	`"efua":{"id":"efua","name":"Efua's Store","shopCode":"EFUA","ownerWhatsApp":"` + ownerEfua + `","payoutPhone":"` + ownerEfua + `","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":3}]},` +
	`"ama":{"id":"ama","name":"Ama's Stall","shopCode":"AMA","ownerWhatsApp":"` + ownerAma + `","payoutPhone":"` + ownerAma + `","dailyGiveawayAllowance":200,"products":[{"id":"smoked-tilapia","name":"Smoked Tilapia","description":"Smoked tilapia from Lake Volta, well dried.","listPrice":180,"floorPrice":140,"stock":10}]}` +
	`}}`

func setupTwoShops(t *testing.T) (string, *chat) {
	t.Helper()
	// The env merchant number belongs to nobody here: both shops
	// record their own owners, so routing runs on the catalogue.
	dir := setupChat(t, "233550009999", twoShopCatalogue)
	return dir, &chat{dir: dir}
}

func dayState(t *testing.T, dir, shopID string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "data", "daybook.json"))
	if err != nil {
		t.Fatalf("day book: %v", err)
	}
	var all map[string]map[string]any
	if err := json.Unmarshal(data, &all); err != nil {
		t.Fatal(err)
	}
	return all[shopID]
}

func ordersOnDisk(t *testing.T, dir string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "data", "orders.json"))
	if err != nil {
		t.Fatalf("orders: %v", err)
	}
	var all map[string]map[string]any
	if err := json.Unmarshal(data, &all); err != nil {
		t.Fatal(err)
	}
	out := []map[string]any{}
	for _, o := range all {
		out = append(out, o)
	}
	return out
}

func TestShopCodeEntryBindsBuyerToThatShopOnly(t *testing.T) {
	_, c := setupTwoShops(t)

	// Entry greetings arrive in pieces (welcome, then the goods), so
	// these read the whole reply rather than its last message.
	buyer := "233550004001"
	c.say(t, buyer, "SHOP-AMA")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Ama's Stall") || !strings.Contains(got, "Smoked Tilapia") {
		t.Fatalf("SHOP-AMA entry greeting = %q", got)
	}
	if strings.Contains(got, "Beaded Bag") {
		t.Fatalf("Ama's greeting named Efua's product: %q", got)
	}

	buyer2 := "233550004002"
	c.say(t, buyer2, "shop:efua")
	got = strings.Join(c.replies(t, buyer2), "\n")
	if !strings.Contains(got, "Efua's Store") || !strings.Contains(got, "Handmade Beaded Bag") {
		t.Fatalf("shop:efua entry greeting = %q", got)
	}
	if strings.Contains(got, "Tilapia") {
		t.Fatalf("Efua's greeting named Ama's product: %q", got)
	}

	// A bare code word works too, mid-sentence and all.
	buyer3 := "233550004003"
	c.say(t, buyer3, "Hello, AMA sent me")
	got = strings.Join(c.replies(t, buyer3), "\n")
	if !strings.Contains(got, "Ama's Stall") {
		t.Fatalf("bare code entry = %q", got)
	}
}

func TestBuyerWithNoCodeIsSoldNothing(t *testing.T) {
	_, c := setupTwoShops(t)
	buyer := "233550004010"

	c.say(t, buyer, "How much is the beaded bag?")
	got := c.last(t, buyer)
	if !strings.Contains(got, "shop code") {
		t.Fatalf("no-code reply should ask for the shop code, got %q", got)
	}
	for _, banned := range []string{"Beaded Bag", "Tilapia", "GH₵850", "GH₵180", "Here is what we have"} {
		if strings.Contains(got, banned) {
			t.Fatalf("no-code reply leaked %q: %q", banned, got)
		}
	}
	// Her bare offers buy nothing either.
	c.say(t, buyer, "I can offer 800")
	if got := c.last(t, buyer); !strings.Contains(got, "shop code") {
		t.Fatalf("no-code offer should still ask, got %q", got)
	}

	// The shop name is the fallback door, and only then is she sold to.
	c.say(t, buyer, "Efua's Store")
	if got := c.last(t, buyer); !strings.Contains(got, "Handmade Beaded Bag") {
		t.Fatalf("entry by shop name = %q", got)
	}
	c.say(t, buyer, "How much is the beaded bag?")
	if got := c.last(t, buyer); !strings.Contains(got, "GH₵850") {
		t.Fatalf("quote after entry = %q", got)
	}
}

func TestSwitchingShopsResetsTheSession(t *testing.T) {
	_, c := setupTwoShops(t)
	buyer := "233550004020"

	c.say(t, buyer, "SHOP-EFUA")
	c.last(t, buyer)
	c.say(t, buyer, "How much is the beaded bag?")
	c.last(t, buyer)
	c.say(t, buyer, "I can offer 500")
	if got := c.last(t, buyer); !strings.Contains(got, "GH₵") {
		t.Fatalf("haggle counter = %q", got)
	}

	// She walks into Ama's shop instead. The switch is announced,
	// and the Efua haggle does not follow her.
	c.say(t, buyer, "SHOP-AMA")
	got := c.last(t, buyer)
	if !strings.Contains(got, "left Efua's Store") || !strings.Contains(got, "now at Ama's Stall") {
		t.Fatalf("switch announcement = %q", got)
	}
	if strings.Contains(got, "Beaded Bag") {
		t.Fatalf("switch greeting leaked the old shop's shelf: %q", got)
	}

	// The old product cannot be quoted, offered on, or closed here.
	c.say(t, buyer, "How much is the beaded bag?")
	if got := c.last(t, buyer); strings.Contains(got, "GH₵850") || strings.Contains(got, "Beaded Bag") {
		t.Fatalf("old shop's product answered in the new shop: %q", got)
	}
	c.say(t, buyer, "740")
	for _, body := range c.replies(t, buyer) {
		if strings.Contains(body, "is yours for") {
			t.Fatalf("a leftover haggle closed in the wrong shop: %q", body)
		}
	}

	// And the new shop sells to her normally, above its own floor.
	c.say(t, buyer, "How much is the smoked tilapia?")
	if got := c.last(t, buyer); !strings.Contains(got, "GH₵180") {
		t.Fatalf("new shop quote = %q", got)
	}
	c.say(t, buyer, "I can offer 100")
	counter := c.last(t, buyer)
	for _, f := range figuresIn(counter) {
		if f < 140 || f > 180 {
			t.Fatalf("Ama counter figure %v outside [floor 140, list 180] in %q", f, counter)
		}
	}
}

func TestTwoBuyersInTwoShopsKeepSeparateBooks(t *testing.T) {
	dir, c := setupTwoShops(t)
	buyerA := "233550004030" // Efua's bag
	buyerB := "233550004031" // Ama's tilapia

	c.say(t, buyerA, "SHOP-EFUA")
	c.last(t, buyerA)
	c.say(t, buyerB, "SHOP-AMA")
	c.last(t, buyerB)

	c.say(t, buyerA, "How much is the beaded bag?")
	c.last(t, buyerA)
	c.say(t, buyerB, "How much is the smoked tilapia?")
	c.last(t, buyerB)

	// Interleaved haggling: neither conversation may feel the other.
	c.say(t, buyerA, "I can offer 500")
	for _, f := range figuresIn(c.last(t, buyerA)) {
		if f < 680 || f > 850 {
			t.Fatalf("Efua counter figure %v outside [680, 850]", f)
		}
	}
	c.say(t, buyerB, "I can offer 100")
	for _, f := range figuresIn(c.last(t, buyerB)) {
		if f < 140 || f > 180 {
			t.Fatalf("Ama counter figure %v outside [140, 180]", f)
		}
	}
	c.say(t, buyerA, "650")
	c.last(t, buyerA)

	// Deals arrive in pieces: the agreement first (the deal line,
	// then the fulfilment question, since neither shop states
	// delivery terms), and the sale completes when she answers
	// where she is based.
	c.say(t, buyerB, "160")
	if got := strings.Join(c.replies(t, buyerB), "\n"); !strings.Contains(got, "yours for GH₵160") {
		t.Fatalf("Ama deal = %q", got)
	}
	c.say(t, buyerB, "Koforidua")
	c.finishDetails(t, buyerB)
	if got := strings.Join(c.replies(t, buyerB), "\n"); !strings.Contains(got, "Pay here") {
		t.Fatalf("Ama completion = %q", got)
	}
	c.say(t, buyerA, "740")
	if got := strings.Join(c.replies(t, buyerA), "\n"); !strings.Contains(got, "yours for GH₵740") {
		t.Fatalf("Efua deal = %q", got)
	}
	c.say(t, buyerA, "Accra")
	c.finishDetails(t, buyerA)
	if got := strings.Join(c.replies(t, buyerA), "\n"); !strings.Contains(got, "Pay here") {
		t.Fatalf("Efua completion = %q", got)
	}

	// Separate day books, each with its own sale and giveaway.
	efuaDay := dayState(t, dir, "efua")
	if efuaDay["sales"] != 1.0 || efuaDay["revenue"] != 740.0 || efuaDay["givenAway"] != 110.0 {
		t.Fatalf("Efua day book = %v", efuaDay)
	}
	amaDay := dayState(t, dir, "ama")
	if amaDay["sales"] != 1.0 || amaDay["revenue"] != 160.0 || amaDay["givenAway"] != 20.0 {
		t.Fatalf("Ama day book = %v", amaDay)
	}

	// Orders carry their shop and that shop's payout phone.
	orders := ordersOnDisk(t, dir)
	if len(orders) != 2 {
		t.Fatalf("orders on disk = %v, want 2", orders)
	}
	byShop := map[string]map[string]any{}
	for _, o := range orders {
		byShop[o["merchantId"].(string)] = o
	}
	if byShop["efua"]["productName"] != "Handmade Beaded Bag" || byShop["efua"]["payoutPhone"] != ownerEfua {
		t.Fatalf("Efua order = %v", byShop["efua"])
	}
	if byShop["ama"]["productName"] != "Smoked Tilapia" || byShop["ama"]["payoutPhone"] != ownerAma {
		t.Fatalf("Ama order = %v", byShop["ama"])
	}
	// Each order also carries its own fulfilment: both buyers
	// were asked where they are based, and each answer stayed in
	// its own shop's order.
	if byShop["efua"]["fulfilment"] != "arranged" || byShop["efua"]["location"] != "Accra" {
		t.Fatalf("Efua order fulfilment = %v", byShop["efua"])
	}
	if byShop["ama"]["fulfilment"] != "arranged" || byShop["ama"]["location"] != "Koforidua" {
		t.Fatalf("Ama order fulfilment = %v", byShop["ama"])
	}

	// Each owner heard about her own sale, and only her own.
	efuaNotes := strings.Join(c.replies(t, ownerEfua), "\n")
	if !strings.Contains(efuaNotes, "New order") || !strings.Contains(efuaNotes, "Beaded Bag") || strings.Contains(efuaNotes, "Tilapia") {
		t.Fatalf("Efua's notices = %q", efuaNotes)
	}
	amaNotes := strings.Join(c.replies(t, ownerAma), "\n")
	if !strings.Contains(amaNotes, "New order") || !strings.Contains(amaNotes, "Tilapia") || strings.Contains(amaNotes, "Beaded Bag") {
		t.Fatalf("Ama's notices = %q", amaNotes)
	}
}

func TestOwnerCommandsTouchOnlyHerOwnShop(t *testing.T) {
	_, c := setupTwoShops(t)

	// Efua reprices her own bag; Ama's shelf does not move.
	c.say(t, ownerEfua, "change price of beaded bag to 900")
	if got := c.last(t, ownerEfua); !strings.Contains(got, "GH₵900") {
		t.Fatalf("owner change reply = %q", got)
	}
	if p := catalogue.FindByName("ama", "smoked tilapia"); p.ListPrice != 180 {
		t.Fatalf("Ama's price moved to %v under Efua's command", p.ListPrice)
	}

	// Efua cannot reach Ama's product at all.
	c.say(t, ownerEfua, "change price of smoked tilapia to 1")
	if got := c.last(t, ownerEfua); !strings.Contains(got, "Nothing changed") {
		t.Fatalf("cross-shop command should fail, got %q", got)
	}
	if p := catalogue.FindByName("ama", "smoked tilapia"); p.ListPrice != 180 {
		t.Fatalf("Ama's price was changed by another shop's owner: %v", p.ListPrice)
	}

	// Ama's own listing shows her floors and none of Efua's goods.
	c.say(t, ownerAma, "my catalogue")
	got := c.last(t, ownerAma)
	if !strings.Contains(got, "Smoked Tilapia") || !strings.Contains(got, "your floor GH₵140") {
		t.Fatalf("Ama's catalogue = %q", got)
	}
	if strings.Contains(got, "Beaded Bag") || strings.Contains(got, "GH₵680") {
		t.Fatalf("Ama's catalogue leaked Efua's shop: %q", got)
	}
}

func TestSettlementNoticeGoesToTheSellingShopsOwner(t *testing.T) {
	t.Setenv("SEEV_SECRET_KEY", "")
	t.Setenv("SEEV_WEBHOOK_SECRET", "")
	dir, c := setupTwoShops(t)
	buyer := "233550004040"

	c.say(t, buyer, "SHOP-AMA")
	c.last(t, buyer)
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.last(t, buyer)
	c.say(t, buyer, "I can offer 100")
	c.last(t, buyer)
	c.say(t, buyer, "160")
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "yours for GH₵160") {
		t.Fatalf("deal = %q", got)
	}
	// The agreement waits on fulfilment; her answer completes the
	// sale and the order exists from that moment.
	c.say(t, buyer, "Koforidua")
	c.finishDetails(t, buyer)
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Pay here") {
		t.Fatalf("completion = %q", got)
	}
	c.replies(t, ownerAma) // drain the New order note
	c.replies(t, ownerEfua)

	var ref string
	for _, o := range ordersOnDisk(t, dir) {
		if o["merchantId"] == "ama" {
			ref = o["reference"].(string)
		}
	}
	if ref == "" {
		t.Fatal("no Ama order on disk to settle")
	}
	body := `{"data":{"reference":"SEEVSIM1","status":"completed","amount":160},"meta":{"order_reference":"` + ref + `"}}`
	if status := seevwebhook.Handle(body, "", ""); status != 200 {
		t.Fatalf("webhook status %d, want 200", status)
	}

	amaNotes := strings.Join(c.replies(t, ownerAma), "\n")
	if !strings.Contains(amaNotes, "Payment received") || !strings.Contains(amaNotes, "Smoked Tilapia") ||
		!strings.Contains(amaNotes, "GH₵157.6 settles to your MoMo tomorrow") {
		t.Fatalf("Ama's settlement notice = %q", amaNotes)
	}
	if got := c.replies(t, ownerEfua); len(got) != 0 {
		t.Fatalf("Efua was told about Ama's settlement: %q", got)
	}
	paid := ordersOnDisk(t, dir)
	if paid[0]["status"] != "paid" {
		t.Fatalf("order status = %v, want paid", paid[0]["status"])
	}
}

func TestMerchantSignupCreatesShopABuyerCanEnter(t *testing.T) {
	_, c := setupTwoShops(t)
	newOwner := "233550005001"
	buyer := "233550005002"

	c.say(t, newOwner, "I want to sell")
	if got := c.last(t, newOwner); !strings.Contains(got, "what is your shop called") && !strings.Contains(got, "What is your shop called") {
		t.Fatalf("signup opening = %q", got)
	}
	c.say(t, newOwner, "Yaa's Provisions")
	replies := c.replies(t, newOwner)
	joined := strings.Join(replies, "\n")
	if !strings.Contains(joined, "is open") || !strings.Contains(joined, "shop code") {
		t.Fatalf("signup creation replies = %q", joined)
	}
	if !strings.Contains(joined, "What is it called?") {
		t.Fatalf("signup should hand straight into product onboarding, got %q", joined)
	}

	shop := catalogue.MerchantByOwner(newOwner)
	if shop == nil || shop.Name != "Yaa's Provisions" || shop.ShopCode == "" {
		t.Fatalf("created shop = %+v", shop)
	}

	// She stocks her shelf through the ordinary onboarding walk.
	c.say(t, newOwner, "Plantain Chips")
	c.last(t, newOwner)
	c.say(t, newOwner, "Crisp fried plantain chips.")
	c.last(t, newOwner)
	c.say(t, newOwner, "40")
	c.last(t, newOwner)
	c.say(t, newOwner, "30")
	c.last(t, newOwner)
	c.say(t, newOwner, "only as a whole")
	c.last(t, newOwner)
	c.say(t, newOwner, "yes")
	if got := c.last(t, newOwner); !strings.Contains(got, "Saved") {
		t.Fatalf("first product save = %q", got)
	}

	// A buyer enters by the code she was given and buys from her,
	// above her floor, while the seeded shops stay untouched.
	c.say(t, buyer, "SHOP-"+shop.ShopCode)
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "Yaa's Provisions") {
		t.Fatalf("buyer entry to the new shop = %q", got)
	}
	c.say(t, buyer, "How much are the plantain chips?")
	if got := c.last(t, buyer); !strings.Contains(got, "GH₵40") {
		t.Fatalf("new shop quote = %q", got)
	}
	c.say(t, buyer, "35")
	deal := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(deal, "yours for") && !strings.Contains(deal, "GH₵") {
		t.Fatalf("new shop haggle = %q", deal)
	}
	if got := len(catalogue.ListProducts("efua")); got != 1 {
		t.Fatalf("Efua's shelf changed during a stranger's signup: %d products", got)
	}
	if got := len(catalogue.ListProducts("ama")); got != 1 {
		t.Fatalf("Ama's shelf changed during a stranger's signup: %d products", got)
	}
}

func TestModelContextIsGroundedInTheSessionShopOnly(t *testing.T) {
	_, c := setupTwoShops(t)

	var systemPrompt string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, m := range body.Messages {
			if m.Role == "system" {
				systemPrompt = m.Content
			}
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{
				"content": `{"action":"counter","price":170,"message":"From GH₵180, I have come down to GH₵170 for the Smoked Tilapia."}`,
			}}},
		})
	}))
	defer stub.Close()
	t.Setenv("META_MODEL_API_KEY", "stub-key")
	t.Setenv("META_MODEL_API_BASE", stub.URL+"/v1")

	buyer := "233550004050"
	c.say(t, buyer, "SHOP-AMA")
	c.last(t, buyer)
	c.say(t, buyer, "How much is the smoked tilapia?")
	c.last(t, buyer)
	c.say(t, buyer, "I can offer 100")
	if got := c.last(t, buyer); !strings.Contains(got, "GH₵170") {
		t.Fatalf("model counter reply = %q", got)
	}
	if systemPrompt == "" {
		t.Fatal("the stub model was never called")
	}
	if !strings.Contains(systemPrompt, "Smoked Tilapia") || !strings.Contains(systemPrompt, "Ama's Stall") {
		t.Fatalf("model context is not grounded in the session shop:\n%s", systemPrompt)
	}
	for _, other := range []string{"Handmade Beaded Bag", "Ankara", "Efua"} {
		if strings.Contains(systemPrompt, other) {
			t.Fatalf("model context for Ama's shop leaked %q from another shop:\n%s", other, systemPrompt)
		}
	}
}
