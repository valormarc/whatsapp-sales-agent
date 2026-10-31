package wants

import (
	"os"
	"path/filepath"
	"testing"
)

// Run-6 record: a read error on an existing want book
// read as a fresh empty book, unpoisoned. The read-error
// road now restores from the last known good copy, or
// poisons.
func TestUnreadableWantBookRestoresAndPoisons(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "wants.json")
	b := New(file)
	if !b.Record("demo", "233550000001", "the beaded bag") {
		t.Fatal("the want must record")
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
	if got := b.List("demo"); len(got) != 1 {
		t.Fatalf("an unreadable want book must restore from its backup, got %+v", got)
	}
	if b.poisoned {
		t.Fatal("a restored book is not poisoned")
	}

	dir2 := t.TempDir()
	file2 := filepath.Join(dir2, "wants.json")
	target2 := filepath.Join(dir2, "elsewhere")
	if err := os.Mkdir(target2, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target2, file2); err != nil {
		t.Fatal(err)
	}
	b2 := New(file2)
	_ = b2.List("demo")
	if !b2.poisoned {
		t.Fatal("an unreadable want book with no good copy must be poisoned")
	}
}
