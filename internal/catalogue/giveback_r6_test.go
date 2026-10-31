package catalogue

import (
	"testing"
)

// Run-6 record: a fractional give-back on a product that
// had become plain was truncated by int conversion —
// stock silently inflated or lost. GiveStockMeasured
// restores in the take-time denomination, and the plain
// road refuses a bare fraction whole, mirroring
// TakeStock.
func TestGiveBackInTakeTimeMeasure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKATHON_AGENT_DATA_DIR", dir)
	seedCatalogueShop(t, dir, Catalogue{Merchants: map[string]Merchant{
		"demo": {ID: "demo", Name: "Demo", Products: []Product{
			{ID: "fabric", Name: "Ankara Fabric", ListPrice: 100, FloorPrice: 80, Stock: 4, Unit: "yard", BaseQty: 1, StockUnits: 10},
		}},
	}})
	// Take 2.5 yards.
	if !TakeStock("demo", "Ankara Fabric", 2.5) {
		t.Fatal("the measured take must succeed")
	}
	// The product's measure is cleared afterwards: it
	// stands plain with 4 pieces counted whole.
	c := LoadCatalogue()
	m := c.Merchants["demo"]
	m.Products[0].Unit = ""
	m.Products[0].BaseQty = 0
	m.Products[0].StockUnits = 0
	m.Products[0].Stock = 4
	c.Merchants["demo"] = m
	SaveCatalogue(c)
	// The give-back arrives denominated in the take-time
	// measure: 2.5 yards at a base of 1 yard must restore
	// whole — 4 pieces + ceil(2.5/1) = 7 by the measured
	// branch — never int(2.5) = 2.
	GiveStockMeasured("demo", "Ankara Fabric", 2.5, "yard", 1)
	p := FindByName("demo", "Ankara Fabric")
	if p == nil {
		t.Fatal("the product must be found")
	}
	if p.Stock != 7 {
		t.Fatalf("the measured give-back must land whole: stock = %d, want 7", p.Stock)
	}
	// A bare fractional give on the plain road is refused
	// whole rather than truncated into the shelf.
	before := p.Stock
	GiveStock("demo", "Ankara Fabric", 0.5)
	p = FindByName("demo", "Ankara Fabric")
	if p.Stock != before {
		t.Fatalf("a fractional plain give must be refused: stock moved %d -> %d", before, p.Stock)
	}
}
