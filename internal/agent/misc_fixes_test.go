package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/orders"
)

// Run-2 regression (typed yes/no binding): a bare typed
// "yes" belongs to the armed button question, not to
// whichever pending state dispatch reaches first. Here
// the merchant is mid listing walk when an in-hand
// question is armed out of band; her yes must answer the
// in-hand wait (completing the buyer's sale) and must not
// save the walk's listing.
func TestTypedYesAnswersArmedInHandNotTheWalk(t *testing.T) {
	dir := setupChat(t, trustOwner, shopTrust)
	c := &chat{dir: dir}
	buyer := "233550007101"
	bag := catalogue.FindByName("efua", "Handmade Beaded Bag")

	ms := merchantSessionFor("efua", trustOwner)
	ms.Onboarding = &OnboardingFlow{Step: "confirm", Name: "Walk Item", ListPrice: 100, FloorPrice: 80}
	sessions.put(sessionKey("efua", buyer), &Session{
		Stage: "fulfilment", MerchantID: "efua", Product: bag, AgreedPrice: bag.ListPrice,
		FulfilStep: "inhandwait", PendingFulfil: fulfilment{Mode: "pickup"},
	})
	openInHand(inHandRequest{ShopID: "efua", Buyer: buyer, Item: bag.Name, Price: bag.ListPrice})
	askYesNo(trustOwner, ms, "inhand", buyer, "Do you have it in hand now?", "Yes, in hand", "Not in hand")

	c.say(t, trustOwner, "yes")
	if oldestOpenInHand("efua") != nil {
		t.Fatal("the typed yes must settle the armed in-hand question")
	}
	if orders.Default.CountFor("efua") != 1 {
		t.Fatal("the buyer's sale must complete on the merchant's yes")
	}
	if catalogue.FindByName("efua", "Walk Item") != nil {
		t.Fatal("the yes must not save the walk's listing")
	}
	if ms.Onboarding == nil {
		t.Fatal("the walk itself must still stand, unanswered")
	}
}

// Run-2 regressions (fee quotes): with two buyers waiting,
// a bare figure binds neither — she is asked which place;
// a place named in the answer binds that quote; and her
// standing-fee command is never consumed as a quote answer.
func TestFeeQuoteAmbiguityAndCommandPrecedence(t *testing.T) {
	merchant := "233550003031"
	dir := setupChat(t, merchant, shopQuoteStyle)
	c := &chat{dir: dir}
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	for _, tc := range []struct{ buyer, place string }{
		{"233550007201", "Osu"},
		{"233550007202", "Madina"},
	} {
		sessions.put(sessionKey("demo", tc.buyer), &Session{
			Stage: "fulfilment", MerchantID: "demo", FulfilStep: "feewait",
			FeeQuotePlace: tc.place, AgreedPrice: 850, Product: bag,
		})
		openFeeQuote(feeQuote{ShopID: "demo", Buyer: tc.buyer, Item: bag.Name, Price: 850, Place: tc.place})
	}

	c.say(t, merchant, "30")
	if got := strings.Join(c.replies(t, merchant), "\n"); !strings.Contains(got, "More than one buyer is waiting") {
		t.Fatalf("a bare figure with two waiting must ask which, got %q", got)
	}
	if n := len(openFeeQuotes("demo")); n != 2 {
		t.Fatalf("nothing may bind on an ambiguous figure, %d quotes still open", n)
	}

	c.say(t, merchant, "set delivery fee 25")
	if n := len(openFeeQuotes("demo")); n != 2 {
		t.Fatal("the standing-fee command must not be consumed as a quote answer")
	}
	if m := catalogue.GetMerchant("demo"); m == nil || m.DeliveryFee != 25 {
		t.Fatalf("the standing fee command must execute, got %+v", m)
	}

	c.say(t, merchant, "Madina 40")
	quotes := openFeeQuotes("demo")
	if len(quotes) != 1 || quotes[0].Place != "Osu" {
		t.Fatalf("the named place must bind its own quote only, still open: %+v", quotes)
	}
	if s := sessions.get(sessionKey("demo", "233550007202")); s == nil || s.FulfilStep == "feewait" {
		t.Fatalf("the Madina buyer's flow must advance on her fee, got %+v", s)
	}
}

// Run-2 regression: mark-sent with a named but
// unresolvable reference must not silently substitute
// the shop's sole waiting order.
func TestMarkSentNamedWrongRefDoesNotSubstitute(t *testing.T) {
	setupChat(t, trustOwner, shopTrust)
	merchant := catalogue.GetMerchant("efua")
	paidOrderFor("efua", "EFUA-0001", "233550007301", 130)

	if !merchantMarkSent(trustOwner, merchant, "efua", "mark EFUA-9999 sent") {
		t.Fatal("the mark-sent word must be handled")
	}
	if o := orders.Default.GetOrder("EFUA-0001"); o == nil || o.DeliveryState == "sent" {
		t.Fatalf("the waiting order must not be marked on another's reference, got %+v", o)
	}
	if !merchantMarkSent(trustOwner, merchant, "efua", "mark EFUA-0001 sent") {
		t.Fatal("the true reference must be handled")
	}
	if o := orders.Default.GetOrder("EFUA-0001"); o == nil || o.DeliveryState != "sent" {
		t.Fatalf("the named order must be marked sent, got %+v", o)
	}
}

// Run-2 regression: an order cannot be received before
// it is sent — no dispute closes, no delivery credit
// mints, on a confirmation for goods never dispatched.
func TestReceivedNeedsSentFirst(t *testing.T) {
	setupChat(t, trustOwner, shopTrust)
	merchant := catalogue.GetMerchant("efua")
	buyer := "233550007401"
	paidOrderFor("efua", "EFUA-0001", buyer, 130)

	if !buyerConfirmReceived(buyer, merchant, "efua", "it has arrived") {
		t.Fatal("the confirmation word must be handled")
	}
	if o := orders.Default.GetOrder("EFUA-0001"); o == nil || o.DeliveryState == "received" {
		t.Fatalf("a never-sent order must not stand received, got %+v", o)
	}
	if sellerDelivered(trustOwner) != 0 {
		t.Fatal("no delivery credit may mint for an unsent order")
	}
	orders.Default.Update("EFUA-0001", func(o *orders.Order) {
		o.DeliveryState = "sent"
		o.SentAt = nowStamp()
	})
	if !buyerConfirmReceived(buyer, merchant, "efua", "it has arrived") {
		t.Fatal("the confirmation must be handled once sent")
	}
	if o := orders.Default.GetOrder("EFUA-0001"); o == nil || o.DeliveryState != "received" {
		t.Fatalf("a sent order must confirm received, got %+v", o)
	}
	if sellerDelivered(trustOwner) != 1 {
		t.Fatal("the sent-and-received delivery must count")
	}
}

// Run-2 regression: two shops under one name — a codeless
// buyer naming the shop is asked for the shop's code, not
// delivered to the first registrant.
func TestDuplicateShopNameDoorAsksForCode(t *testing.T) {
	const twins = `{"merchants":{
		"twa":{"id":"twa","name":"Twin Shop","shopCode":"TWA","ownerWhatsApp":"233550007501","products":[]},
		"twb":{"id":"twb","name":"Twin Shop","shopCode":"TWB","ownerWhatsApp":"233550007502","products":[]}
	}}`
	dir := setupChat(t, "233550007501", twins)
	c := &chat{dir: dir}
	buyer := "233550007503"
	c.say(t, buyer, "Twin Shop")
	if got := bindings.get(buyer); got != "" {
		t.Fatalf("an ambiguous name must bind nobody, bound %q", got)
	}
	if got := strings.Join(c.replies(t, buyer), "\n"); !strings.Contains(got, "More than one shop") {
		t.Fatalf("the buyer must be told why and asked for the code, got %q", got)
	}
}

// Run-2 regression: "no" at the signup holder
// confirmation is an answer, not the MoMo holder name.
func TestSignupHolderConfirmationNoIsNotAName(t *testing.T) {
	owner := "233550007601"
	setupChat(t, owner, shopWithBag)
	s := &Session{Stage: "merchant", MerchantID: "", Signup: &SignupFlow{Step: "confirmholder", Name: "New Shop", HolderName: "Ama Serwaa"}}
	sessions.put(sessionKey("", owner), s)
	advanceSignup(owner, s, "no", "no")
	if s.Signup == nil {
		t.Fatal("the signup flow must still stand")
	}
	if s.Signup.HolderName == "no" {
		t.Fatal("'no' must never become the holder name")
	}
	if s.Signup.Step != "holder" {
		t.Fatalf("a no must ask for the right account's name, step is %q", s.Signup.Step)
	}
}

// Run-2 needs-validation closure: a payment link routes
// through the shop's subaccount only while the number it
// was minted for is still her payout number.
func TestSubaccountForChecksTheBinding(t *testing.T) {
	m := &catalogue.Merchant{ID: "demo", PayoutPhone: "233550000001", SubaccountCode: "SUB-1", SubaccountPhone: "233550000001"}
	if got := subaccountFor(m); got != "SUB-1" {
		t.Fatalf("a bound code for the standing number must route, got %q", got)
	}
	m.PayoutPhone = "233550000099"
	if got := subaccountFor(m); got != "" {
		t.Fatalf("a code minted for another number must not route, got %q", got)
	}
	m.SubaccountPhone = ""
	if got := subaccountFor(m); got != "SUB-1" {
		t.Fatalf("a legacy record with no binding recorded stands as it always did, got %q", got)
	}
}

// Run-2 needs-validation closure: migrating a legacy
// flat photo on read must not take the file away from a
// second shop whose product names the same flat path.
// The first reader copies it into her own scope; the
// legacy file stands until no product names it.
func TestLegacyPhotoMigrationLeavesSharedFile(t *testing.T) {
	const shops = `{"merchants":{
		"shopa":{"id":"shopa","name":"A Shop","shopCode":"ASH","ownerWhatsApp":"233550007701","products":[{"id":"pa","name":"Bag A","description":"A bag.","listPrice":100,"floorPrice":80,"stock":1,"photos":["shared-1.jpg"]}]},
		"shopb":{"id":"shopb","name":"B Shop","shopCode":"BSH","ownerWhatsApp":"233550007702","products":[{"id":"pb","name":"Bag B","description":"B bag.","listPrice":100,"floorPrice":80,"stock":1,"photos":["shared-1.jpg"]}]}
	}}`
	setupChat(t, "233550007701", shops)
	legacy := datadir.Path("photos/shared-1.jpg")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("shared-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	pa := catalogue.FindByName("shopa", "Bag A")
	if data, ok := readStoredPhoto("shopa", pa, "shared-1.jpg"); !ok || string(data) != "shared-bytes" {
		t.Fatalf("shop A must read the legacy photo, got %q %v", data, ok)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("the legacy file must stand while shop B still names it")
	}
	pb := catalogue.FindByName("shopb", "Bag B")
	if data, ok := readStoredPhoto("shopb", pb, "shared-1.jpg"); !ok || string(data) != "shared-bytes" {
		t.Fatalf("shop B must still read the photo after A's migration, got %q %v", data, ok)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("once no product names the legacy path, the file drains away")
	}
}

// Run-2 needs-validation closure: orphan deletion
// consults the references sessions hold, not only the
// catalogue — a photo a live burst still names survives
// a product removal that would otherwise orphan it.
func TestOrphanDeletionRespectsSessionRefs(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	bag := catalogue.FindByName("demo", "Handmade Beaded Bag")
	rel := "photos/demo/held-1.jpg"
	full := datadir.Path(rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("held-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A live session's burst holds the only reference.
	sessions.put(sessionKey("demo", stockOwner), &Session{
		Stage:      "merchant",
		MerchantID: "demo",
		BurstBatch: []burstItem{{Name: "Held Item", PhotoRefs: []string{rel}}},
	})
	if !catalogue.PhotoReferenced(rel) {
		t.Fatal("a session-held photo must count as referenced")
	}
	_ = bag
}
