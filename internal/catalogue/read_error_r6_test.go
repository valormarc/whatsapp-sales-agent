package catalogue

import (
	"os"
	"path/filepath"
	"testing"
)

// Run-6 record: a read error on an existing catalogue
// file read as an empty catalogue, unpoisoned, and the
// next save wiped every shop. The read-error road now
// restores from the last known good copy, or poisons.
func TestUnreadableCatalogueRestoresFromBackup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKATHON_AGENT_DATA_DIR", dir)
	seedCatalogueFile(t, dir)
	target := filepath.Join(dir, "elsewhere")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "catalogue.json")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, file); err != nil {
		t.Fatal(err)
	}
	c := LoadCatalogue()
	if len(c.Merchants) == 0 {
		t.Fatal("an unreadable catalogue must restore its shops from the backup")
	}
	if cataloguePoisoned {
		t.Fatal("a restored catalogue is not poisoned")
	}
}

func TestUnreadableCatalogueWithoutBackupPoisons(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKATHON_AGENT_DATA_DIR", dir)
	target := filepath.Join(dir, "elsewhere")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "catalogue.json")); err != nil {
		t.Fatal(err)
	}
	_ = LoadCatalogue()
	if !cataloguePoisoned {
		t.Fatal("an unreadable catalogue with no good copy must be poisoned")
	}
	cataloguePoisoned = false // leave the package as the test found it
}
