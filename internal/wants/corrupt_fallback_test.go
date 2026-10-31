package wants

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Run-5 record: the want book preserved a corrupt file
// aside but never restored and never poisoned, so the
// next buyer's ordinary want cemented the loss of every
// recorded want. It now carries the platform discipline
// entire.

func TestCorruptWantsRestoreAndPoison(t *testing.T) {
	file := filepath.Join(t.TempDir(), "wants.json")
	b := New(file)
	if !b.Record("demo", "233500000001", "blue shoes") {
		t.Fatal("the seed want must record")
	}
	if err := os.WriteFile(file, []byte("{torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := b.List("demo"); len(got) != 1 {
		t.Fatalf("the book must restore from its good copy, got %v", got)
	}
	raw, _ := os.ReadFile(file)
	var probe store
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("the restore must repair the main file: %v", err)
	}
	// Double failure: reads empty, and a new want's save
	// is refused rather than cementing the empty read.
	if err := os.WriteFile(file, []byte("{torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file+".bak", []byte("{also torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := b.List("demo"); len(got) != 0 {
		t.Fatalf("a double failure reads empty, got %v", got)
	}
	b.Record("demo", "233500000002", "red hat")
	raw, _ = os.ReadFile(file)
	if string(raw) != "{torn" {
		t.Fatalf("a poisoned save must be refused, file holds %q", raw)
	}
}
