package orders

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Run-4 record (the run's one medium): a corrupt orders
// file read as an empty book on every load, and the first
// new order cemented the loss by saving the near-empty
// book over the main file. The book now restores from
// its last-known-good copy, and refuses to save at all
// when no good copy exists.
func TestCorruptBookRestoresFromLastKnownGood(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "orders.json")
	b := New(file)
	b.RecordOrder("EFUA-1001", Order{Reference: "EFUA-1001", Status: "paid", Price: 850})
	if _, err := os.Stat(file + ".bak"); err != nil {
		t.Fatalf("a good save must leave a last-known-good copy: %v", err)
	}
	// Corrupt the main file the way a torn write would.
	if err := os.WriteFile(file, []byte(`{"EFUA-1001":{"reference":"EFUA-10`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := b.GetOrder("EFUA-1001")
	if got == nil || got.Price != 850 {
		t.Fatalf("a corrupt main file must restore the book from its good copy, got %+v", got)
	}
	// And a new order after the restore saves the whole
	// book again — recovered, not cemented over.
	b.RecordOrder("EFUA-1002", Order{Reference: "EFUA-1002", Status: "pending", Price: 100})
	if b.GetOrder("EFUA-1001") == nil || b.GetOrder("EFUA-1002") == nil {
		t.Fatal("after a restore, the book must hold both the recovered and the new order")
	}
}

// Run-5 residual of the run-4 fix: a restore that never
// repairs the main file means every later load re-enters
// the restore path and preserves a fresh aside, so the
// data directory accumulates copies of the book without
// bound. The restore now repairs the main file, and the
// aside is a single fixed file.
func TestRestoreRepairsTheMainFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "orders.json")
	b := New(file)
	b.RecordOrder("AAA-1001", Order{Reference: "AAA-1001", Status: "paid", Price: 850})
	if err := os.WriteFile(file, []byte("{torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := b.GetOrder("AAA-1001"); got == nil {
		t.Fatal("the book must restore from its good copy")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]Order
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("the restore must repair the main file, it still does not parse: %v", err)
	}
	// A second corruption preserves into the SAME aside:
	// the directory must hold exactly one .corrupt file.
	if err := os.WriteFile(file, []byte("{torn again"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = b.loadAll()
	entries, _ := os.ReadDir(filepath.Dir(file))
	corrupt := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), ".corrupt") {
			corrupt++
		}
	}
	if corrupt != 1 {
		t.Fatalf("exactly one corrupt aside may stand, dir holds %v", entries)
	}
	asides, _ := os.ReadFile(file + ".corrupt")
	if string(asides) != "{torn again" {
		t.Fatalf("the aside must hold the latest corrupt bytes, got %q", asides)
	}
}

func TestCorruptBookWithNoGoodCopyRefusesToSave(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "orders.json")
	corrupt := []byte(`{"EFUA-1001":{"reference":"EFUA-10`)
	if err := os.WriteFile(file, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	b := New(file)
	b.RecordOrder("EFUA-1002", Order{Reference: "EFUA-1002", Status: "pending", Price: 100})
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(corrupt) {
		t.Fatalf("with no good copy to restore, a save must refuse rather than cement the loss; file now reads %q", raw)
	}
}
