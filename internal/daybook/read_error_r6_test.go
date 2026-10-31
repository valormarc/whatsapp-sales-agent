package daybook

import (
	"os"
	"path/filepath"
	"testing"
)

// Run-6 record: a read error on an existing day book read
// as a fresh empty book, unpoisoned. The read-error road
// now restores from the last known good copy, or poisons.
func TestUnreadableDaybookRestoresAndPoisons(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "daybook.json")
	b := New(file)
	b.RecordSale("demo", Sale{ListPrice: 850, AgreedPrice: 730, Allowance: 300})
	target := filepath.Join(dir, "elsewhere")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, file); err != nil {
		t.Fatal(err)
	}
	state := b.GetDayState("demo", Options{Allowance: 300})
	if state.Sales != 1 || state.Revenue != 730 {
		t.Fatalf("an unreadable day book must restore from its backup, got %+v", state)
	}
	if b.poisoned {
		t.Fatal("a restored book is not poisoned")
	}

	dir2 := t.TempDir()
	file2 := filepath.Join(dir2, "daybook.json")
	target2 := filepath.Join(dir2, "elsewhere")
	if err := os.Mkdir(target2, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target2, file2); err != nil {
		t.Fatal(err)
	}
	b2 := New(file2)
	_ = b2.GetDayState("demo", Options{})
	if !b2.poisoned {
		t.Fatal("an unreadable day book with no good copy must be poisoned")
	}
	b2.RecordSale("demo", Sale{ListPrice: 10, AgreedPrice: 10})
	fi, err := os.Lstat(file2)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("a poisoned save must be refused, lstat err=%v", err)
	}
}
