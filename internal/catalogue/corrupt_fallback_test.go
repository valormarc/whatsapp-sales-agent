package catalogue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Run-5 record (medium): the catalogue kept the
// preserve-only corrupt behaviour — no last-known-good
// copy, no restore, no poison — so a routine new-shop
// signup's save cemented the loss of every shop. The
// catalogue now carries the platform discipline entire:
// saves refresh the good copy, a failed load restores
// from it and repairs the main file, and a double failure
// poisons saves until a load parses again.

func seedCatalogueFile(t *testing.T, dir string) string {
	t.Helper()
	t.Setenv("HACKATHON_AGENT_DATA_DIR", dir)
	shop := Catalogue{Merchants: map[string]Merchant{
		"demo": {ID: "demo", Name: "Demo", Products: []Product{
			{ID: "bag", Name: "Bag", ListPrice: 100, FloorPrice: 80, Stock: 2},
		}},
	}}
	SaveCatalogue(shop)
	return filepath.Join(dir, "catalogue.json")
}

func TestCorruptCatalogueRestoresFromGoodCopyAndRepairs(t *testing.T) {
	path := seedCatalogueFile(t, t.TempDir())
	if err := os.WriteFile(path, []byte("{torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := LoadCatalogue()
	if _, ok := c.Merchants["demo"]; !ok {
		t.Fatalf("the catalogue must restore from its good copy, got %v", c.Merchants)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var probe Catalogue
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("the restore must repair the main file, it still does not parse: %v", err)
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Fatalf("the corrupt bytes must be preserved: %v", err)
	}
}

func TestCorruptCatalogueWithNoGoodCopyPoisonsSaves(t *testing.T) {
	path := seedCatalogueFile(t, t.TempDir())
	if err := os.WriteFile(path, []byte("{torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", []byte("{also torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := LoadCatalogue()
	if len(c.Merchants) != 0 {
		t.Fatalf("a double failure reads empty, got %v", c.Merchants)
	}
	SaveCatalogue(Catalogue{Merchants: map[string]Merchant{
		"newshop": {ID: "newshop", Name: "New"},
	}})
	raw, _ := os.ReadFile(path)
	if string(raw) != "{torn" {
		t.Fatalf("a poisoned save must be refused — the signup must not cement the loss, file holds %q", raw)
	}
	// An operator repair un-poisons: the next load parses.
	good, _ := marshalJSON(Catalogue{Merchants: map[string]Merchant{"demo": {ID: "demo", Name: "Demo"}}})
	if err := os.WriteFile(path, good, 0o600); err != nil {
		t.Fatal(err)
	}
	if c := LoadCatalogue(); len(c.Merchants) != 1 {
		t.Fatalf("the repaired catalogue must load, got %v", c.Merchants)
	}
}
