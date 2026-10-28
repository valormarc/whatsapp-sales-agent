package catalogue

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// testShop writes a one-product shop into a temp data directory and
// points the store at it, so write tests never touch the real data.
func testShop(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HACKATHON_AGENT_DATA_DIR", dir)
	shop := Catalogue{Merchants: map[string]Merchant{
		"demo": {
			ID:                     "demo",
			Name:                   "Test Shop",
			DailyGiveawayAllowance: 200,
			Products: []Product{
				{ID: "beaded-bag", Name: "Handmade Beaded Bag", Description: "Hand-beaded evening bag.", ListPrice: 850, FloorPrice: 680, Stock: 3},
			},
		},
	}}
	b, err := marshalJSON(shop)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "catalogue.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAddProductPersists(t *testing.T) {
	testShop(t)
	stored, err := AddProduct("demo", Product{Name: "Palm Oil (1 litre)", Description: "Red palm oil.", ListPrice: 90, FloorPrice: 70, Stock: 20})
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != "palm-oil-1-litre" {
		t.Fatalf("generated id %q, want a slug of the name", stored.ID)
	}
	m := GetMerchant("demo")
	if len(m.Products) != 2 {
		t.Fatalf("products %d, want 2 after add (reloaded from disk)", len(m.Products))
	}
	found := FindByName("demo", "palm oil")
	if found == nil || found.ListPrice != 90 || found.FloorPrice != 70 {
		t.Fatalf("added product not found as written: %+v", found)
	}
}

func TestAddProductValidation(t *testing.T) {
	testShop(t)
	cases := []struct {
		name    string
		product Product
		want    error
	}{
		{"empty name", Product{Name: "  ", ListPrice: 100, FloorPrice: 50}, ErrEmptyName},
		{"duplicate name, different case", Product{Name: "handmade beaded bag", ListPrice: 100, FloorPrice: 50}, ErrDuplicateName},
		{"zero list", Product{Name: "X", ListPrice: 0, FloorPrice: 0}, ErrListPrice},
		{"negative list", Product{Name: "X", ListPrice: -5, FloorPrice: 1}, ErrListPrice},
		{"zero floor", Product{Name: "X", ListPrice: 100, FloorPrice: 0}, ErrFloorPrice},
		{"floor equal to list", Product{Name: "X", ListPrice: 100, FloorPrice: 100}, ErrFloorPrice},
		{"floor above list", Product{Name: "X", ListPrice: 100, FloorPrice: 140}, ErrFloorPrice},
	}
	for _, c := range cases {
		if _, err := AddProduct("demo", c.product); !errors.Is(err, c.want) {
			t.Fatalf("%s: err %v, want %v", c.name, err, c.want)
		}
	}
	if got := len(ListProducts("demo")); got != 1 {
		t.Fatalf("products %d, want 1: no rejected product may be saved", got)
	}
}

func TestUpdatePrices(t *testing.T) {
	testShop(t)
	updated, err := UpdateListPrice("demo", "beaded bag", 900)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ListPrice != 900 {
		t.Fatalf("list %v, want 900", updated.ListPrice)
	}
	if _, err := UpdateListPrice("demo", "beaded bag", 680); !errors.Is(err, ErrListBelowFloor) {
		t.Fatalf("list at the floor: err %v, want ErrListBelowFloor", err)
	}
	if _, err := UpdateListPrice("demo", "beaded bag", 0); !errors.Is(err, ErrListPrice) {
		t.Fatalf("zero list: err %v, want ErrListPrice", err)
	}
	updated, err = UpdateFloorPrice("demo", "Handmade Beaded Bag", 700)
	if err != nil {
		t.Fatal(err)
	}
	if updated.FloorPrice != 700 {
		t.Fatalf("floor %v, want 700", updated.FloorPrice)
	}
	// A last price equal to the price is her saying the price
	// does not move: the product becomes fixed price.
	fixed, err := UpdateFloorPrice("demo", "beaded bag", 900)
	if err != nil {
		t.Fatal(err)
	}
	if !fixed.Fixed || fixed.FloorPrice != 900 {
		t.Fatalf("last price at the price: %+v, want fixed at 900", fixed)
	}
	if _, err := UpdateFloorPrice("demo", "beaded bag", 950); !errors.Is(err, ErrFloorPrice) {
		t.Fatalf("last price above the price: err %v, want ErrFloorPrice", err)
	}
	// And a last price below the price again clears the flag.
	back, err := UpdateFloorPrice("demo", "Handmade Beaded Bag", 700)
	if err != nil {
		t.Fatal(err)
	}
	if back.Fixed || back.FloorPrice != 700 {
		t.Fatalf("last price below the price: %+v, want negotiable at 700", back)
	}
	if _, err := UpdateFloorPrice("demo", "no such thing", 10); !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("unknown product: err %v, want ErrProductNotFound", err)
	}
	// The rejected writes changed nothing on disk.
	p := FindByName("demo", "beaded bag")
	if p.ListPrice != 900 || p.FloorPrice != 700 {
		t.Fatalf("stored product %+v, want list 900 floor 700", p)
	}
}

// A fixed-price product is the one case where the last price
// equals the price: it saves only when the fixed flag says so
// outright, and repricing it moves both figures together so it
// stays fixed at the new price.
func TestFixedPriceProduct(t *testing.T) {
	testShop(t)
	stored, err := AddProduct("demo", Product{Name: "Plantain Chips", Description: "Crisp plantain chips.", ListPrice: 50, FloorPrice: 50, Fixed: true, Stock: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Fixed || stored.FloorPrice != 50 || stored.ListPrice != 50 {
		t.Fatalf("stored fixed product = %+v", stored)
	}
	updated, err := UpdateListPrice("demo", "Plantain Chips", 60)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Fixed || updated.ListPrice != 60 || updated.FloorPrice != 60 {
		t.Fatalf("repriced fixed product = %+v, want fixed at 60", updated)
	}
}

func TestRemoveProduct(t *testing.T) {
	testShop(t)
	removed, err := RemoveProduct("demo", "beaded bag")
	if err != nil {
		t.Fatal(err)
	}
	if removed.Name != "Handmade Beaded Bag" {
		t.Fatalf("removed %q, want the bag", removed.Name)
	}
	if got := len(ListProducts("demo")); got != 0 {
		t.Fatalf("products %d, want 0 after removal (reloaded from disk)", got)
	}
	if _, err := RemoveProduct("demo", "beaded bag"); !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("second removal: err %v, want ErrProductNotFound", err)
	}
}

func TestSetAllowance(t *testing.T) {
	testShop(t)
	if err := SetAllowance("demo", 400); err != nil {
		t.Fatal(err)
	}
	if got := GetMerchant("demo").DailyGiveawayAllowance; got != 400 {
		t.Fatalf("allowance %v, want 400 (reloaded from disk)", got)
	}
	if err := SetAllowance("demo", -1); !errors.Is(err, ErrNegativeAllowance) {
		t.Fatalf("negative allowance: err %v, want ErrNegativeAllowance", err)
	}
	if err := SetAllowance("noshop", 100); !errors.Is(err, ErrMerchantNotFound) {
		t.Fatalf("unknown shop: err %v, want ErrMerchantNotFound", err)
	}
}

// twoShops writes a catalogue with two shops that carry the full
// real world fields: distinct codes, owners, payout phones, prices
// and floors, so lookups can be checked for cross-shop leaks.
func twoShops(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HACKATHON_AGENT_DATA_DIR", dir)
	cat := Catalogue{Merchants: map[string]Merchant{
		"efua": {
			ID: "efua", Name: "Efua's Store", DailyGiveawayAllowance: 300,
			ShopCode: "EFUA", OwnerWhatsApp: "+233 55 000 1001", PayoutPhone: "233550001001",
			Products: []Product{
				{ID: "beaded-bag", Name: "Handmade Beaded Bag", Description: "Hand-beaded evening bag.", ListPrice: 850, FloorPrice: 680, Stock: 3},
			},
		},
		"ama": {
			ID: "ama", Name: "Ama's Stall", DailyGiveawayAllowance: 200,
			ShopCode: "AMA", OwnerWhatsApp: "233550001002", PayoutPhone: "233550001002",
			Products: []Product{
				{ID: "smoked-tilapia", Name: "Smoked Tilapia", Description: "Smoked tilapia.", ListPrice: 180, FloorPrice: 140, Stock: 10},
			},
		},
	}}
	b, err := marshalJSON(cat)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "catalogue.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestShopLookupsStayInTheirOwnShop(t *testing.T) {
	twoShops(t)

	if m := MerchantByShopCode("efua"); m == nil || m.ID != "efua" {
		t.Fatalf("code lookup ignoring case: %+v", m)
	}
	if m := MerchantByShopCode(" AMA "); m == nil || m.ID != "ama" {
		t.Fatalf("code lookup with spaces: %+v", m)
	}
	if m := MerchantByShopCode("NOPE"); m != nil {
		t.Fatalf("unknown code found %+v, want nil: no silent routing", m)
	}
	if m := MerchantByShopCode(""); m != nil {
		t.Fatalf("empty code found %+v, want nil", m)
	}

	if m := MerchantByOwner("233550001001"); m == nil || m.ID != "efua" {
		t.Fatalf("owner digits must find her shop across formats: %+v", m)
	}
	if m := MerchantByOwner("+233550001002"); m == nil || m.ID != "ama" {
		t.Fatalf("second owner: %+v", m)
	}
	if m := MerchantByOwner("233550009999"); m != nil {
		t.Fatalf("a stranger's number owns %+v, want nil", m)
	}

	if m := MerchantByShopName("I want Efua's Store please"); m == nil || m.ID != "efua" {
		t.Fatalf("full shop name in text: %+v", m)
	}
	if m := MerchantByShopName("ama"); m == nil || m.ID != "ama" {
		t.Fatalf("short shop name reply: %+v", m)
	}
	if m := MerchantByShopName("the market down the road"); m != nil {
		t.Fatalf("unrelated text resolved to %+v, want nil", m)
	}
}

func TestCreateMerchantMakesUniqueShopWithCodeAndOwner(t *testing.T) {
	twoShops(t)

	created, err := CreateMerchant("Efua's Store", "233550001777")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "efua" {
		t.Fatal("a second shop with the same name must get its own id")
	}
	if created.ShopCode == "" || created.ShopCode == "EFUA" {
		t.Fatalf("shop code %q, want a fresh unique code", created.ShopCode)
	}
	if created.OwnerWhatsApp != "233550001777" {
		t.Fatalf("owner %q not recorded", created.OwnerWhatsApp)
	}
	// It is on disk, findable by owner and by code, and empty.
	if m := MerchantByOwner("233550001777"); m == nil || m.ID != created.ID {
		t.Fatalf("new shop not findable by owner: %+v", m)
	}
	if m := MerchantByShopCode(created.ShopCode); m == nil || m.ID != created.ID {
		t.Fatalf("new shop not findable by code: %+v", m)
	}
	if got := len(ListProducts(created.ID)); got != 0 {
		t.Fatalf("new shelf has %d products, want 0", got)
	}
	// The existing shops were not disturbed.
	if m := MerchantByShopCode("EFUA"); m == nil || len(m.Products) != 1 {
		t.Fatalf("existing shop changed by a signup: %+v", m)
	}
	if _, err := CreateMerchant("   ", "233550001778"); err == nil {
		t.Fatal("a shop with no name must be refused")
	}
}

func TestDeliveryFeeAndNoteRoundTripThroughTheStore(t *testing.T) {
	testShop(t)
	if err := SetDeliveryFee("demo", 25); err != nil {
		t.Fatalf("set delivery fee: %v", err)
	}
	if err := SetDeliveryNote("demo", "Sent by bus to your station."); err != nil {
		t.Fatalf("set delivery note: %v", err)
	}
	m := GetMerchant("demo")
	if m.DeliveryFee != 25 || m.DeliveryNote != "Sent by bus to your station." {
		t.Fatalf("stored delivery terms = %+v", m)
	}
	// Another shop's terms do not move.
	if other := GetMerchant("ama"); other != nil && other.DeliveryFee != 0 {
		t.Fatalf("ama's delivery fee moved to %v", other.DeliveryFee)
	}
	// A negative fee is refused and nothing changes.
	if err := SetDeliveryFee("demo", -5); err == nil {
		t.Fatal("a negative delivery fee must be refused")
	}
	if got := GetMerchant("demo").DeliveryFee; got != 25 {
		t.Fatalf("delivery fee after refused change = %v, want 25", got)
	}
	// Zero clears the fee: no fixed fee, arranged with the seller.
	if err := SetDeliveryFee("demo", 0); err != nil {
		t.Fatalf("clear delivery fee: %v", err)
	}
	if got := GetMerchant("demo").DeliveryFee; got != 0 {
		t.Fatalf("delivery fee after clearing = %v, want 0", got)
	}
	if err := SetDeliveryFee("nope", 10); err == nil {
		t.Fatal("an unknown shop must be refused")
	}
}

func TestFindByName(t *testing.T) {
	testShop(t)
	if p := FindByName("demo", "HANDMADE BEADED BAG"); p == nil {
		t.Fatal("exact match ignoring case must find the bag")
	}
	if p := FindByName("demo", "beaded bag"); p == nil {
		t.Fatal("partial name must find the bag")
	}
	if p := FindByName("demo", "shea butter"); p != nil {
		t.Fatalf("unknown name found %+v, want nil", p)
	}
	if p := FindByName("demo", "  "); p != nil {
		t.Fatalf("blank name found %+v, want nil", p)
	}
}
