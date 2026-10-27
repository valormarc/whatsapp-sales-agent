package catalogue

import (
	"errors"
	"testing"
)

// The measure maths: a 6-yard piece
// at GH₵450 is GH₵75 a yard, quantities scale in proportion, the
// list rounds half up to whole cedis, and the floor rounds up, so
// rounding can never dip the agent below the merchant's
// proportional floor.
func TestScaledPrices(t *testing.T) {
	ankara := Product{Name: "Ankara Fabric (6 yards)", ListPrice: 450, FloorPrice: 360, Unit: "yard", BaseQty: 6}
	cases := []struct {
		qty       float64
		wantList  float64
		wantFloor float64
	}{
		{6, 450, 360},
		{3, 225, 180},
		{1, 75, 60},
		{1.5, 113, 90}, // 112.5 rounds half up; the floor is exact
		{8, 600, 480},  // more than the base quantity scales linearly
		{0.5, 38, 30},  // 37.5 rounds half up
	}
	for _, c := range cases {
		if got := ankara.ScaledList(c.qty); got != c.wantList {
			t.Fatalf("ScaledList(%v) = %v, want %v", c.qty, got, c.wantList)
		}
		if got := ankara.ScaledFloor(c.qty); got != c.wantFloor {
			t.Fatalf("ScaledFloor(%v) = %v, want %v", c.qty, got, c.wantFloor)
		}
	}
	if got := ankara.UnitPrice(); got != 75 {
		t.Fatalf("UnitPrice() = %v, want 75", got)
	}
}

// The scaled list never sits below the scaled floor, however the
// rounding falls on a small quantity: the floor wins the tie.
func TestScaledListNeverBelowScaledFloor(t *testing.T) {
	p := Product{ListPrice: 10, FloorPrice: 9.9, Unit: "kilo", BaseQty: 100}
	if got := p.ScaledList(1); got != 1 || got < p.ScaledFloor(1) {
		t.Fatalf("ScaledList(1) = %v with floor %v, want the floor's 1", got, p.ScaledFloor(1))
	}
}

func TestByMeasureNeedsUnitAndBaseQty(t *testing.T) {
	if (Product{}).ByMeasure() {
		t.Fatal("a product with no measure fields is sold whole only")
	}
	if (Product{Unit: "yard"}).ByMeasure() {
		t.Fatal("a unit with no base quantity is not a measure")
	}
	if !(Product{Unit: "yard", BaseQty: 6}).ByMeasure() {
		t.Fatal("unit plus base quantity is a measure")
	}
}

func TestSetAndClearMeasureRoundTrip(t *testing.T) {
	testShop(t)

	if _, err := SetMeasure("demo", "beaded bag", "yard", 0); !errors.Is(err, ErrBadMeasure) {
		t.Fatalf("zero base quantity error = %v, want ErrBadMeasure", err)
	}
	if _, err := SetMeasure("demo", "no such item", "yard", 6); !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("unknown product error = %v, want ErrProductNotFound", err)
	}

	updated, err := SetMeasure("demo", "beaded bag", "yard", 6)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Unit != "yard" || updated.BaseQty != 6 || !updated.ByMeasure() {
		t.Fatalf("set measure = %+v", updated)
	}
	if got := FindByName("demo", "beaded bag"); got == nil || got.Unit != "yard" || got.BaseQty != 6 {
		t.Fatalf("measure on disk = %+v", got)
	}

	cleared, err := ClearMeasure("demo", "beaded bag")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Unit != "" || cleared.BaseQty != 0 || cleared.ByMeasure() {
		t.Fatalf("cleared measure = %+v, want sold whole only", cleared)
	}
	if got := FindByName("demo", "beaded bag"); got == nil || got.ByMeasure() {
		t.Fatalf("cleared measure on disk = %+v", got)
	}
}

// The seed carries the one by-measure product the demo opens
// with: Efua's Ankara, priced per 6 yards, cut by the yard.
func TestSeedAnkaraIsSoldByTheYard(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKATHON_AGENT_DATA_DIR", dir)
	c := LoadCatalogue()
	demo, ok := c.Merchants["demo"]
	if !ok {
		t.Fatal("seed has no demo shop")
	}
	var ankara *Product
	for i := range demo.Products {
		if demo.Products[i].ID == "ankara" {
			ankara = &demo.Products[i]
		}
	}
	if ankara == nil {
		t.Fatal("seed has no ankara product")
	}
	if ankara.Unit != "yard" || ankara.BaseQty != 6 || ankara.UnitPrice() != 75 {
		t.Fatalf("seeded ankara = %+v, want yard measure, base 6, GH₵75 a yard", ankara)
	}
	for _, p := range demo.Products {
		if p.ID != "ankara" && p.ByMeasure() {
			t.Fatalf("seeded product %s unexpectedly sold by measure", p.Name)
		}
	}
	ama, ok := c.Merchants["ama"]
	if !ok {
		t.Fatal("seed has no ama shop")
	}
	for _, p := range ama.Products {
		if p.ByMeasure() {
			t.Fatalf("seeded Ama product %s unexpectedly sold by measure", p.Name)
		}
	}
}
