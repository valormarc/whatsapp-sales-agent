package agent

import (
	"math/rand"
	"os"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/money"
)

// Regression for the audit finding that the env number kept
// merchant authority over a shop that had recorded its own
// owner: the fallback now applies only while the shop
// records no owner, the same precondition the notice phone
// already used.
func TestEnvFallbackYieldsToARecordedOwner(t *testing.T) {
	// twinBagShops' shopa records 233550008001 as its owner.
	// Point the env number elsewhere: it must get nothing,
	// while the recorded owner keeps her authority.
	setupChat(t, "233559999999", twinBagShops)
	if got := ownerShopFor("233559999999"); got != nil {
		t.Fatalf("the env number must hold no authority over a shop with a recorded owner, got %v", got)
	}
	if got := ownerShopFor("233550008001"); got == nil || got.ID != "shopa" {
		t.Fatalf("the recorded owner keeps her authority, got %v", got)
	}

	// And while a shop records no owner at all (the seeded
	// demo shop), the env fallback still stands.
	setupChat(t, stockOwner, shopWithBag)
	if got := ownerShopFor(stockOwner); got == nil {
		t.Fatal("with no recorded owner, the env number is the demo shop's owner")
	}
}

// The total-haggle shares must sum to the agreed total
// exactly, every share inside its own bounds: the audit's
// sweep over ordinary hands found 34 totals the old
// single-item residue drop got wrong.
func TestApportionTotalAlwaysClosesTheSum(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 3000; trial++ {
		n := 2 + rng.Intn(4)
		hand := make([]catalogue.Product, n)
		var listSum, floorSum float64
		for i := range hand {
			list := 20 + rng.Float64()*900
			floor := list * (0.5 + rng.Float64()*0.45)
			hand[i] = catalogue.Product{ListPrice: money.Round(list), FloorPrice: money.Round(floor)}
			listSum += hand[i].ListPrice
			floorSum += hand[i].FloorPrice
		}
		total := money.Round(floorSum + rng.Float64()*(listSum-floorSum))
		prices := apportionTotal(hand, total)
		var sum float64
		for i, p := range prices {
			sum += p
			if p < hand[i].FloorPrice-0.001 || p > hand[i].ListPrice+0.001 {
				t.Fatalf("trial %d: share %v outside [%v, %v]", trial, p, hand[i].FloorPrice, hand[i].ListPrice)
			}
		}
		if money.Round(sum*100) != money.Round(total*100) {
			t.Fatalf("trial %d: shares sum to %v, agreed total %v (hand %v)", trial, sum, total, hand)
		}
	}
}

const twinBagShops = `{"merchants":{
"shopa":{"id":"shopa","name":"Ama's Bags","shopCode":"AMA","ownerWhatsApp":"233550008001","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Beaded bag.","listPrice":850,"floorPrice":680,"stock":2}]},
"shopb":{"id":"shopb","name":"Efua's Bags","shopCode":"EFUA","ownerWhatsApp":"233550008002","dailyGiveawayAllowance":300,"products":[{"id":"beaded-bag","name":"Handmade Beaded Bag","description":"Beaded bag.","listPrice":500,"floorPrice":400,"stock":2}]}
}}`

// Regression for the audit findings on stored photos: two
// shops' products with the same id shared one file path
// (the second shop's photo overwrote the first's), and a
// removed product's files survived and were still sent.
// Paths are now shop-scoped, readers re-resolve against
// the live catalogue, and removal takes the files with it.
func TestPhotosAreShopScopedAndDieWithTheProduct(t *testing.T) {
	stubPhotoMedia(t)
	setupChat(t, "233550008001", twinBagShops)
	var uploaded [][]byte
	oldUp := uploadMediaFn
	uploadMediaFn = func(data []byte, mime string) (string, bool) {
		uploaded = append(uploaded, append([]byte{}, data...))
		return "uploaded-ref", true
	}
	t.Cleanup(func() { uploadMediaFn = oldUp })

	bagA := catalogue.FindByName("shopa", "Handmade Beaded Bag")
	bagB := catalogue.FindByName("shopb", "Handmade Beaded Bag")
	attachPhotos("shopa", *bagA, []string{"media-a"})
	attachPhotos("shopb", *bagB, []string{"media-b"})

	bagA = catalogue.FindByName("shopa", "Handmade Beaded Bag")
	bagB = catalogue.FindByName("shopb", "Handmade Beaded Bag")
	if len(bagA.Photos) != 1 || len(bagB.Photos) != 1 {
		t.Fatalf("each listing keeps its photo: A %v, B %v", bagA.Photos, bagB.Photos)
	}
	if bagA.Photos[0] == bagB.Photos[0] {
		t.Fatalf("two shops must never share a photo path, both are %q", bagA.Photos[0])
	}
	dataA, err := os.ReadFile(datadir.Path(bagA.Photos[0]))
	if err != nil || string(dataA) != "fake-jpeg-bytes-media-a" {
		t.Fatalf("shop A's file holds shop A's photo, got %q (%v)", dataA, err)
	}

	// Each shop's buyer is sent that shop's own picture.
	if !sendProductPhotos("233550008101", "shopa", bagA) {
		t.Fatal("shop A's photos should send")
	}
	if !sendProductPhotos("233550008102", "shopb", bagB) {
		t.Fatal("shop B's photos should send")
	}
	if len(uploaded) != 2 || string(uploaded[0]) != "fake-jpeg-bytes-media-a" || string(uploaded[1]) != "fake-jpeg-bytes-media-b" {
		t.Fatalf("each buyer must receive her own shop's picture, got %q", uploaded)
	}

	// Removal takes the files with it, and a session-held
	// copy of the removed product sends nothing afterwards.
	removedPath := bagA.Photos[0]
	if _, err := catalogue.RemoveProduct("shopa", "Handmade Beaded Bag"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(datadir.Path(removedPath)); !os.IsNotExist(err) {
		t.Fatal("a removed product's photo file must be deleted")
	}
	if sendProductPhotos("233550008101", "shopa", bagA) {
		t.Fatal("a removed product's photos must never be sent again")
	}
	if _, err := os.Stat(datadir.Path(bagB.Photos[0])); err != nil {
		t.Fatal("the other shop's photo is untouched by the removal")
	}
}
