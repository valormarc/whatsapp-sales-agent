package orders

import (
	"os"
	"path/filepath"
	"testing"
)

// Run-6 record: the corrupt-file discipline keyed only on
// parse errors, so an existing book file that could not
// be READ at all (modelled here by a symlink to a
// directory: reads fail, the path stays writable) read as
// a fresh empty book, unpoisoned, and the next routine
// save cemented the loss. The read-error road now
// restores from the last known good copy, or poisons.
func TestUnreadableBookRestoresFromBackup(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "orders.json")
	b := New(file)
	b.RecordOrder("READ-1", Order{Reference: "READ-1", Status: "paid", MerchantID: "demo"})
	if _, err := os.Stat(file + ".bak"); err != nil {
		t.Fatalf("the good save must leave a backup: %v", err)
	}
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
	all := b.All()
	if len(all) != 1 || all[0].Reference != "READ-1" {
		t.Fatalf("an unreadable book must restore from its backup, got %+v", all)
	}
	if b.poisoned {
		t.Fatal("a restored book is not poisoned")
	}
}

func TestUnreadableBookWithoutBackupPoisons(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "orders.json")
	target := filepath.Join(dir, "elsewhere")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, file); err != nil {
		t.Fatal(err)
	}
	b := New(file)
	if all := b.All(); len(all) != 0 {
		t.Fatalf("no book can be read, got %+v", all)
	}
	if !b.poisoned {
		t.Fatal("an unreadable book with no good copy must be poisoned")
	}
	b.RecordOrder("READ-2", Order{Reference: "READ-2", Status: "paid", MerchantID: "demo"})
	fi, err := os.Lstat(file)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("a poisoned save must be refused (the path must stand untouched), lstat err=%v", err)
	}
}
