package catalogue

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Run-3 record: TakeStock composed its check and deduction
// through updateMerchant's load-modify-save cycle, but no
// lock spanned the cycle, so concurrent takes interleaved
// their loads and saves and sold the same units repeatedly
// (the audit's parent check took 46 against a stock of
// 20). The cycle lock now spans load to save: exactly the
// shelf's units can be taken, and the shelf ends empty.
func TestConcurrentTakesCannotOversell(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKATHON_AGENT_DATA_DIR", dir)
	shop := Catalogue{Merchants: map[string]Merchant{
		"demo": {
			ID:   "demo",
			Name: "Demo",
			Products: []Product{
				{ID: "bag", Name: "Bag", ListPrice: 100, FloorPrice: 80, Stock: 20},
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

	const attempts = 46
	var wg sync.WaitGroup
	var mu sync.Mutex
	took := 0
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if TakeStock("demo", "Bag", 1) {
				mu.Lock()
				took++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if took != 20 {
		t.Fatalf("exactly the shelf's 20 units may be taken, got %d", took)
	}
	p := FindByName("demo", "Bag")
	if p == nil || p.AvailableUnits() != 0 {
		t.Fatalf("the shelf must end empty, got %+v", p)
	}
}

// Run-4 record: TakeStock had no positivity guard — a
// negative take added stock. The boundary refuses
// non-positive takes now.
func TestTakeStockRefusesNonPositiveQuantities(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKATHON_AGENT_DATA_DIR", dir)
	shop := Catalogue{Merchants: map[string]Merchant{
		"demo": {
			ID:   "demo",
			Name: "Demo",
			Products: []Product{
				{ID: "bag", Name: "Bag", ListPrice: 100, FloorPrice: 80, Stock: 3},
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
	if TakeStock("demo", "Bag", -3) {
		t.Fatal("a negative take must be refused")
	}
	if TakeStock("demo", "Bag", 0) {
		t.Fatal("a zero take must be refused")
	}
	p := FindByID("demo", "bag")
	if p == nil || p.Stock != 3 {
		t.Fatalf("refused takes must leave the shelf untouched, got %+v", p)
	}
	if !TakeStock("demo", "Bag", 1) {
		t.Fatal("an ordinary positive take must still work")
	}
}
