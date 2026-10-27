package daybook

import (
	"path/filepath"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

var testProduct = catalogue.Product{ListPrice: 850, FloorPrice: 680}

func tmpBook(t *testing.T) *Daybook {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "daybook.json"))
}

func TestSalesAccumulate(t *testing.T) {
	book := tmpBook(t)
	book.RecordSale("m", Sale{ListPrice: 850, AgreedPrice: 730, Allowance: 300, Date: "2026-10-20"})
	s := book.RecordSale("m", Sale{ListPrice: 450, AgreedPrice: 400, Allowance: 300, Date: "2026-10-20"})
	if s.Sales != 2 {
		t.Fatalf("sales %d, want 2", s.Sales)
	}
	if s.GivenAway != 170 { // 120 + 50 handed out across the day
		t.Fatalf("givenAway %v, want 170", s.GivenAway)
	}
	if s.Revenue != 1130 {
		t.Fatalf("revenue %v, want 1130", s.Revenue)
	}
}

func TestNewDayStartsFreshBook(t *testing.T) {
	book := tmpBook(t)
	book.RecordSale("m", Sale{ListPrice: 850, AgreedPrice: 700, Allowance: 300, Date: "2026-10-20"})
	s := book.GetDayState("m", Options{Allowance: 300, Date: "2026-10-21"})
	if s.Sales != 0 || s.GivenAway != 0 {
		t.Fatalf("got %+v, want a fresh book", s)
	}
}

func TestPoolRatioDrains(t *testing.T) {
	if PoolRatio(State{Allowance: 300, GivenAway: 0}) != 1 {
		t.Fatal("full pool should be 1")
	}
	if PoolRatio(State{Allowance: 300, GivenAway: 150}) != 0.5 {
		t.Fatal("half pool should be 0.5")
	}
	if PoolRatio(State{Allowance: 300, GivenAway: 450}) != 0 {
		t.Fatal("overspend clamps at empty")
	}
	if PoolRatio(State{Allowance: 0, GivenAway: 50}) != 1 {
		t.Fatal("no allowance set: book off, ratio 1")
	}
}

func TestPoolDrainsHaggleFirms(t *testing.T) {
	full := HaggleContext(testProduct, State{Allowance: 300, GivenAway: 0})
	half := HaggleContext(testProduct, State{Allowance: 300, GivenAway: 150})
	empty := HaggleContext(testProduct, State{Allowance: 300, GivenAway: 300})

	if full.Floor != 680 {
		t.Fatalf("full pool floor %v, want untouched hard floor 680", full.Floor)
	}
	if empty.Floor != 765 {
		t.Fatalf("empty pool floor %v, want 765 (hard floor plus half the gap)", empty.Floor)
	}
	if !(half.Floor > full.Floor && half.Floor < empty.Floor) {
		t.Fatal("half pool floor should sit between full and empty")
	}
	if !(empty.StepScale < half.StepScale && half.StepScale < full.StepScale) {
		t.Fatal("step scale should shrink as the pool drains")
	}
	if full.Floor < testProduct.FloorPrice {
		t.Fatal("firm never dips below the hard floor")
	}
}
