package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/daybook"
)

// End-to-end merchant onboarding through HandleInbound: the merchant
// talks, the agent answers through the dry-run outbox, and the
// catalogue file on disk is the ground truth. Each test runs in its
// own data directory (t.Chdir redirects the relative data/ paths the
// day book, order book and outbox resolve), with its own phone
// numbers, so no state leaks between tests.

const shopWithBag = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Hand-beaded evening bag with a gold clasp.","listPrice":850,"floorPrice":680,"stock":3}]}}}`

const emptyShop = `{"merchants":{"demo":{"id":"demo","name":"Test Shop","shopCode":"TEST","dailyGiveawayAllowance":200,"products":[]}}}`

func setupChat(t *testing.T, merchantNumber, catalogueJSON string) string {
	t.Helper()
	// Conversational state is process-global (sessions and shop
	// bindings), exactly as in the live server. A test is a fresh
	// world, so it starts with no conversations in flight;
	// without this, one test's buyers leak into the next and
	// repeated runs (-count) replay against stale sessions.
	sessionsMu.Lock()
	sessions = map[string]*Session{}
	bindings = map[string]string{}
	doorShown = map[string]bool{}
	packageNames = map[string]string{}
	sessionsMu.Unlock()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HACKATHON_AGENT_DATA_DIR", "")
	t.Setenv("MERCHANT_WHATSAPP", merchantNumber)
	t.Setenv("META_MODEL_API_KEY", "")
	t.Setenv("WHATSAPP_TOKEN", "")
	t.Setenv("WHATSAPP_PHONE_NUMBER_ID", "")
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data", "catalogue.json"), []byte(catalogueJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

type outboxRecord struct {
	To   string `json:"to"`
	Body string `json:"body"`
}

// chat is a tiny transcript reader over the dry-run outbox. Offsets
// are tracked per recipient: one inbound message can produce replies
// to two numbers (the customer and the merchant), and neither may
// swallow the other's.
type chat struct {
	dir  string
	seen map[string]int
}

func (c *chat) say(t *testing.T, from, text string) {
	t.Helper()
	HandleInbound(Inbound{From: from, Text: text})
}

// replies returns the agent's messages to one number since the last
// call for that number, in order.
func (c *chat) replies(t *testing.T, to string) []string {
	t.Helper()
	if c.seen == nil {
		c.seen = map[string]int{}
	}
	data, err := os.ReadFile(filepath.Join(c.dir, "data", "outbox.jsonl"))
	if err != nil {
		return nil
	}
	var mine []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec outboxRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("outbox line not JSON: %v", err)
		}
		if rec.To == to {
			mine = append(mine, rec.Body)
		}
	}
	out := mine[c.seen[to]:]
	c.seen[to] = len(mine)
	return out
}

// finishDetails answers the two package detail questions that
// follow a settled fulfilment choice: the name for the package,
// then no message for the seller. A test that closes a sale calls
// it where the sale used to complete on the choice alone. Where a
// named collector already gave the package its name, only the
// message question remains: finishNote answers that one.
func (c *chat) finishDetails(t *testing.T, from string) {
	t.Helper()
	c.say(t, from, "Akosua Mensah")
	c.say(t, from, "no")
	c.say(t, from, "yes")
}

func (c *chat) finishNote(t *testing.T, from string) {
	t.Helper()
	c.say(t, from, "no")
	c.say(t, from, "yes")
}

func (c *chat) last(t *testing.T, to string) string {
	t.Helper()
	replies := c.replies(t, to)
	if len(replies) == 0 {
		t.Fatalf("no reply to %s", to)
	}
	return replies[len(replies)-1]
}

var ghsFigure = regexp.MustCompile(`GH₵(\d+(?:\.\d+)?)`)

func figuresIn(text string) []float64 {
	var out []float64
	for _, m := range ghsFigure.FindAllStringSubmatch(text, -1) {
		if f, err := strconv.ParseFloat(m[1], 64); err == nil {
			out = append(out, f)
		}
	}
	return out
}

func TestMerchantOnboardsProductAndCustomerBuysIt(t *testing.T) {
	merchant := "233550001001"
	customer := "233550002001"
	dir := setupChat(t, merchant, emptyShop)
	c := &chat{dir: dir}

	// An empty shelf greets her with how to stock it, whatever she wrote.
	c.say(t, merchant, "Hello")
	if got := c.last(t, merchant); !strings.Contains(got, "add a product") || !strings.Contains(got, "empty") {
		t.Fatalf("empty-shelf greeting = %q", got)
	}

	// The guided flow, question by question.
	c.say(t, merchant, "add a product")
	if got := c.last(t, merchant); !strings.Contains(got, "What is it called?") {
		t.Fatalf("flow start = %q", got)
	}
	c.say(t, merchant, "Kente Cloth")
	if got := c.last(t, merchant); !strings.Contains(got, "Tell me about Kente Cloth") {
		t.Fatalf("description step = %q", got)
	}
	c.say(t, merchant, "Handwoven kente cloth, bright traditional patterns.")
	if got := c.last(t, merchant); !strings.Contains(got, "list price") {
		t.Fatalf("list step = %q", got)
	}
	c.say(t, merchant, "GH₵600")
	if got := c.last(t, merchant); !strings.Contains(got, "last price") || !strings.Contains(got, "never see") {
		t.Fatalf("last price step = %q", got)
	}
	c.say(t, merchant, "480 cedis")
	// The measure question sits between the last price and the summary:
	// does she sell it by measure, or only as a whole?
	if got := c.last(t, merchant); !strings.Contains(got, "by measure") || !strings.Contains(got, "only as a whole") {
		t.Fatalf("measure step = %q", got)
	}
	c.say(t, merchant, "only as a whole")
	summary := c.last(t, merchant)
	for _, want := range []string{"Kente Cloth", "GH₵600", "GH₵480", "GH₵120"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q missing %q", summary, want)
		}
	}
	// Nothing is saved before the yes.
	if got := len(catalogue.ListProducts(MerchantID)); got != 0 {
		t.Fatalf("products %d before confirmation, want 0", got)
	}
	c.say(t, merchant, "yes")
	if got := c.last(t, merchant); !strings.Contains(got, "Saved") || !strings.Contains(got, "live") {
		t.Fatalf("save confirmation = %q", got)
	}
	p := catalogue.FindByName(MerchantID, "Kente Cloth")
	if p == nil || p.ListPrice != 600 || p.FloorPrice != 480 {
		t.Fatalf("onboarded product on disk = %+v", p)
	}

	// A customer enters by the shop code her link carries, then buys
	// it, and never sees a figure under the floor.
	c.say(t, customer, "SHOP-TEST")
	// The welcome now arrives in pieces (greeting, then the goods);
	// read the whole reply.
	if got := strings.Join(c.replies(t, customer), "\n"); !strings.Contains(got, "Welcome to Test Shop") {
		t.Fatalf("shop entry greeting = %q", got)
	}
	c.say(t, customer, "How much is the kente cloth?")
	if got := c.last(t, customer); !strings.Contains(got, "GH₵600") {
		t.Fatalf("quote = %q", got)
	}
	c.say(t, customer, "I can offer 400")
	counter := c.last(t, customer)
	figures := figuresIn(counter)
	if len(figures) == 0 {
		t.Fatalf("counter %q names no figure", counter)
	}
	for _, f := range figures {
		if f < 480 || f > 600 {
			t.Fatalf("counter figure %v outside [floor 480, list 600] in %q", f, counter)
		}
	}
	c.say(t, customer, "560")
	// The price agreement arrives first (the deal line, then the
	// fulfilment question: this shop has no delivery terms, so she
	// is asked where she is based). Her answer completes the sale,
	// and only then does the payment link go out.
	deal := strings.Join(c.replies(t, customer), "\n")
	m := regexp.MustCompile(`yours for GH₵(\d+(?:\.\d+)?)`).FindStringSubmatch(deal)
	if m == nil {
		t.Fatalf("second offer should agree the deal, got %q", deal)
	}
	agreed, _ := strconv.ParseFloat(m[1], 64)
	if agreed < 480 {
		t.Fatalf("agreed price %v crossed the onboarded floor 480", agreed)
	}
	if !strings.Contains(deal, "Where are you based?") {
		t.Fatalf("agreed deal should ask where she is based, got %q", deal)
	}
	c.say(t, customer, "Koforidua")
	c.finishDetails(t, customer)
	if got := strings.Join(c.replies(t, customer), "\n"); !strings.Contains(got, "Pay here") {
		t.Fatalf("her location should complete the sale with the link, got %q", got)
	}
	// The merchant hears about her new product's first sale.
	notes := c.replies(t, merchant)
	found := false
	for _, n := range notes {
		if strings.Contains(n, "New order") && strings.Contains(n, "Kente Cloth") {
			found = true
		}
	}
	if !found {
		t.Fatalf("merchant sale notification missing in %q", notes)
	}
}

func TestOnboardingLastPriceAboveListAndEqualMeansFixed(t *testing.T) {
	merchant := "233550001002"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "new product")
	c.last(t, merchant)
	c.say(t, merchant, "Test Basket")
	c.last(t, merchant)
	c.say(t, merchant, "Woven basket.")
	c.last(t, merchant)
	c.say(t, merchant, "500")
	c.last(t, merchant)
	// A last price above her price is not a last price at all:
	// it is explained in plain words and asked again.
	c.say(t, merchant, "750")
	if got := c.last(t, merchant); !strings.Contains(got, "above your price") || !strings.Contains(got, "last price") {
		t.Fatalf("last price above the price must be explained and re-asked, got %q", got)
	}
	// Her last price is her price: the price does not move, and
	// the product is fixed price from here on.
	c.say(t, merchant, "500")
	replies := strings.Join(c.replies(t, merchant), "\n")
	if !strings.Contains(replies, "fixed") {
		t.Fatalf("last price equal to the price should be read back as fixed, got %q", replies)
	}
	if !strings.Contains(replies, "by measure") {
		t.Fatalf("a fixed last price moves to the measure question, got %q", replies)
	}
	c.say(t, merchant, "whole")
	summary := c.last(t, merchant)
	if !strings.Contains(summary, "GH₵500") || !strings.Contains(summary, "fixed") || strings.Contains(summary, "give away") {
		t.Fatalf("fixed summary = %q", summary)
	}
	c.say(t, merchant, "yes")
	if got := c.last(t, merchant); !strings.Contains(got, "fixed price") {
		t.Fatalf("save confirmation for a fixed product = %q", got)
	}
	p := catalogue.FindByName(MerchantID, "Test Basket")
	if p == nil || !p.Fixed || p.FloorPrice != 500 || p.ListPrice != 500 {
		t.Fatalf("saved product = %+v, want fixed at list 500 last 500", p)
	}
}

func TestOnboardingCancelSavesNothingAndMerchantModeResumes(t *testing.T) {
	merchant := "233550001003"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	c.say(t, merchant, "add a product")
	c.last(t, merchant)
	c.say(t, merchant, "Ghost Item")
	c.last(t, merchant)
	// Mid-flow, her words feed the flow alone: this becomes the
	// description, not an allowance change.
	c.say(t, merchant, "set allowance 999")
	if got := c.last(t, merchant); !strings.Contains(got, "list price") {
		t.Fatalf("mid-flow message should advance the flow, got %q", got)
	}
	if got := catalogue.GetMerchant(MerchantID).DailyGiveawayAllowance; got != 300 {
		t.Fatalf("allowance %v, want 300 untouched by a mid-flow message", got)
	}
	c.say(t, merchant, "cancel")
	if got := c.last(t, merchant); !strings.Contains(got, "Nothing was saved") {
		t.Fatalf("cancel reply = %q", got)
	}
	if got := len(catalogue.ListProducts(MerchantID)); got != 1 {
		t.Fatalf("products %d after cancel, want 1 (nothing saved)", got)
	}
	// Merchant mode behaves as before once the flow is gone.
	c.say(t, merchant, "How is today going?")
	if got := c.last(t, merchant); !strings.Contains(got, "sale(s) today") {
		t.Fatalf("day answer after cancel = %q", got)
	}
}

func TestMerchantCatalogueCommands(t *testing.T) {
	merchant := "233550001004"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	// Her own listing shows last prices; only her number can ask for it.
	c.say(t, merchant, "my catalogue")
	listing := c.last(t, merchant)
	for _, want := range []string{"Handmade Beaded Bag", "GH₵850", "GH₵680", "GH₵300"} {
		if !strings.Contains(listing, want) {
			t.Fatalf("catalogue listing %q missing %q", listing, want)
		}
	}

	c.say(t, merchant, "change price of beaded bag to 900")
	if got := c.last(t, merchant); !strings.Contains(got, "GH₵900") {
		t.Fatalf("change price reply = %q", got)
	}
	if p := catalogue.FindByName(MerchantID, "beaded bag"); p.ListPrice != 900 {
		t.Fatalf("list price on disk %v, want 900", p.ListPrice)
	}

	// A last price above the corrected price is refused.
	c.say(t, merchant, "change last price of beaded bag to 950")
	if got := c.last(t, merchant); !strings.Contains(got, "Nothing changed") {
		t.Fatalf("bad last price reply = %q", got)
	}
	if p := catalogue.FindByName(MerchantID, "beaded bag"); p.FloorPrice != 680 {
		t.Fatalf("floor on disk %v, want 680 unchanged", p.FloorPrice)
	}
	c.say(t, merchant, "change last price of beaded bag to GH₵700")
	if got := c.last(t, merchant); !strings.Contains(got, "GH₵700") || !strings.Contains(got, "last price") {
		t.Fatalf("change last price reply = %q", got)
	}

	// The allowance change lands in the shop, and the day book reads
	// the shop, so new negotiations use it.
	c.say(t, merchant, "set allowance 400")
	if got := c.last(t, merchant); !strings.Contains(got, "GH₵400") {
		t.Fatalf("set allowance reply = %q", got)
	}
	allowance := catalogue.GetMerchant(MerchantID).DailyGiveawayAllowance
	if allowance != 400 {
		t.Fatalf("allowance on disk %v, want 400", allowance)
	}
	day := daybook.Default.GetDayState(MerchantID, daybook.Options{Allowance: allowance})
	if day.Allowance != 400 {
		t.Fatalf("day book allowance %v, want 400", day.Allowance)
	}

	// Removal needs her yes.
	c.say(t, merchant, "remove beaded bag")
	if got := c.last(t, merchant); !strings.Contains(got, "Remove Handmade Beaded Bag") {
		t.Fatalf("remove prompt = %q", got)
	}
	c.say(t, merchant, "no")
	c.last(t, merchant)
	if got := len(catalogue.ListProducts(MerchantID)); got != 1 {
		t.Fatalf("products %d after saying no, want 1", got)
	}
	c.say(t, merchant, "remove beaded bag")
	c.last(t, merchant)
	c.say(t, merchant, "yes")
	if got := c.last(t, merchant); !strings.Contains(got, "out of your catalogue") {
		t.Fatalf("remove confirmation = %q", got)
	}
	if got := len(catalogue.ListProducts(MerchantID)); got != 0 {
		t.Fatalf("products %d after confirmed removal, want 0", got)
	}
}

func TestCustomerCannotOnboardOrSeeFloors(t *testing.T) {
	merchant := "233550001005"
	customer := "233550002005"
	dir := setupChat(t, merchant, shopWithBag)
	c := &chat{dir: dir}

	// Inside the shop as a buyer; merchant powers stay shut to her.
	c.say(t, customer, "SHOP-TEST")
	c.last(t, customer)

	c.say(t, customer, "add a product")
	if got := c.last(t, customer); strings.Contains(got, "What is it called?") {
		t.Fatalf("customer was onboarded: %q", got)
	}
	c.say(t, customer, "my catalogue")
	got := c.last(t, customer)
	if strings.Contains(strings.ToLower(got), "floor") || strings.Contains(got, "GH₵680") {
		t.Fatalf("customer saw merchant-only information: %q", got)
	}
	c.say(t, customer, "remove beaded bag")
	c.last(t, customer)
	c.say(t, customer, "set allowance 5")
	c.last(t, customer)
	if got := len(catalogue.ListProducts(MerchantID)); got != 1 {
		t.Fatalf("products %d, want 1: customer commands changed the catalogue", got)
	}
	if got := catalogue.GetMerchant(MerchantID).DailyGiveawayAllowance; got != 300 {
		t.Fatalf("allowance %v, want 300: a customer changed it", got)
	}
	if p := catalogue.FindByName(MerchantID, "beaded bag"); p.ListPrice != 850 {
		t.Fatalf("list price %v, want 850 untouched", p.ListPrice)
	}
}
