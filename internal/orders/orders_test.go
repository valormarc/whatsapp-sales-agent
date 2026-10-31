package orders

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// A book file that exists but does not parse is evidence,
// not an empty book: it is preserved under a .corrupt- name
// before the book reads as empty, so the next save cannot
// silently wipe orders that were ever recorded.
func TestBookPreservesACorruptFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "orders.json")
	if err := os.WriteFile(file, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := New(file)
	if got := b.GetOrder("EFUA-0001"); got != nil {
		t.Fatalf("a corrupt book reads as empty, got %+v", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	preserved := false
	for _, e := range entries {
		// One fixed aside name since the fifth audit:
		// a timestamped copy per load accumulated
		// without bound.
		if e.Name() == "orders.json.corrupt" {
			preserved = true
		}
	}
	if !preserved {
		t.Fatalf("the corrupt file must be preserved, dir holds %v", entries)
	}
	// This test used to assert the book then "keeps
	// working" — recording a new order into the empty
	// read and saving it over the corrupt file. The
	// fourth audit named that exact save as the step
	// that cements the loss of every order the shop
	// ever took, so the posture is now the opposite:
	// while the corrupt file stands unrecovered (no
	// last-known-good copy exists here), saves refuse
	// and the corrupt bytes survive untouched; once
	// the file is repaired to a book that parses, the
	// book works again.
	b.RecordOrder("EFUA-0001", Order{From: "233550000001", ProductName: "Bag", Price: 10, MerchantID: "efua"})
	if got := b.GetOrder("EFUA-0001"); got != nil {
		t.Fatalf("an unrecovered corrupt book must refuse saves, got %+v", got)
	}
	if err := os.WriteFile(file, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	b.RecordOrder("EFUA-0001", Order{From: "233550000001", ProductName: "Bag", Price: 10, MerchantID: "efua"})
	if got := b.GetOrder("EFUA-0001"); got == nil || got.ProductName != "Bag" {
		t.Fatalf("the book must keep working once its file parses again, got %+v", got)
	}
}

// The book is written from agent turns, the payment
// webhook and the unpaid watch, which do not hold the same
// locks: concurrent writers must not lose each other's
// orders.
func TestBookConcurrentWritersKeepEveryOrder(t *testing.T) {
	b := New(filepath.Join(t.TempDir(), "orders.json"))
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				ref := fmt.Sprintf("SHOP-%04d", w*25+i+1)
				b.RecordOrder(ref, Order{From: "233550000001", ProductName: "Bag", Price: 10, MerchantID: "shop"})
				b.SetStatus(ref, "paid")
			}
		}(w)
	}
	wg.Wait()
	if got := len(b.All()); got != 200 {
		t.Fatalf("concurrent writers lost orders: %d of 200 stand", got)
	}
	for _, o := range b.All() {
		if o.Status != "paid" {
			t.Fatalf("order %s lost its status update: %q", o.Reference, o.Status)
		}
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(b.file), "orders.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(b.file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("the book holds phone numbers and addresses; its mode is %v, want 0600", info.Mode().Perm())
	}
	_ = data
}
