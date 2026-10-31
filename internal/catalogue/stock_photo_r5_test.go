package catalogue

import (
	"os"
	"path/filepath"
	"testing"
)

// Run-5 records at the catalogue boundary: a fractional
// take on a plain product silently truncated, and a
// flat photo reference removed the wrong path.

func seedCatalogueShop(t *testing.T, dir string, shop Catalogue) {
	t.Helper()
	b, err := marshalJSON(shop)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "catalogue.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTakeStockRefusesFractionalTakesOnPlainProducts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKATHON_AGENT_DATA_DIR", dir)
	seedCatalogueShop(t, dir, Catalogue{Merchants: map[string]Merchant{
		"demo": {
			ID: "demo", Name: "Demo",
			Products: []Product{
				{ID: "bag", Name: "Bag", ListPrice: 100, FloorPrice: 80, Stock: 3},
				{ID: "cloth", Name: "Cloth", ListPrice: 50, FloorPrice: 40, Stock: 2, StockUnits: 12, Unit: "yard", BaseQty: 6},
			},
		},
	}})
	if TakeStock("demo", "Bag", 0.5) {
		t.Fatal("a fractional take on a plain product must be refused, not truncated")
	}
	if TakeStock("demo", "Bag", 2.9) {
		t.Fatal("a fractional take on a plain product must be refused, not truncated")
	}
	if p := FindByID("demo", "bag"); p == nil || p.Stock != 3 {
		t.Fatalf("refused takes must leave the shelf untouched, got %+v", p)
	}
	if !TakeStock("demo", "Bag", 2) {
		t.Fatal("a whole take on a plain product must still work")
	}
	if !TakeStock("demo", "Cloth", 0.5) {
		t.Fatal("a by-measure product takes fractions by its nature")
	}
}

// A flat photo reference names its file at photos/<base>:
// removing a product that carries one must remove that
// file — and only once nothing references it any more.
func TestDeleteOrphanedPhotoResolvesFlatRefs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKATHON_AGENT_DATA_DIR", dir)
	if err := os.MkdirAll(filepath.Join(dir, "photos"), 0o755); err != nil {
		t.Fatal(err)
	}
	photoPath := filepath.Join(dir, "photos", "shared-1.jpg")
	if err := os.WriteFile(photoPath, []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedCatalogueShop(t, dir, Catalogue{Merchants: map[string]Merchant{
		"demo": {
			ID: "demo", Name: "Demo",
			Products: []Product{
				{ID: "bag", Name: "Bag", ListPrice: 100, FloorPrice: 80, Stock: 3, Photos: []string{"shared-1.jpg"}},
			},
		},
		"other": {
			ID: "other", Name: "Other",
			Products: []Product{
				{ID: "hat", Name: "Hat", ListPrice: 60, FloorPrice: 50, Stock: 2, Photos: []string{"shared-1.jpg"}},
			},
		},
	}})
	deleteOrphanedPhoto("shared-1.jpg")
	if _, err := os.Stat(photoPath); err != nil {
		t.Fatal("a photo still referenced by another shop must be left alone")
	}
	// One shop's listing goes; the other's reference
	// still holds the file.
	if _, err := RemoveProduct("other", "Hat"); err != nil {
		t.Fatal(err)
	}
	deleteOrphanedPhoto("shared-1.jpg")
	if _, err := os.Stat(photoPath); err != nil {
		t.Fatal("a photo still referenced by the remaining shop must be left alone")
	}
	// The last reference goes; now the file is orphaned
	// and the removal must find it at photos/<base>.
	if _, err := RemoveProduct("demo", "Bag"); err != nil {
		t.Fatal(err)
	}
	deleteOrphanedPhoto("shared-1.jpg")
	if _, err := os.Stat(photoPath); !os.IsNotExist(err) {
		t.Fatal("the flat reference's real file must be removed once orphaned")
	}
}
