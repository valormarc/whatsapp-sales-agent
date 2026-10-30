package insights

import (
	"os"
	"testing"
)

// TestMain runs the package's tests from a scratch
// directory. The data directory resolves relative to the
// working directory, so a test that touches a store
// without pointing it at a fixture of its own would
// otherwise leave state inside the source tree (the stray
// data/catalogue.json this file exists to prevent).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "insights-tests-")
	if err != nil {
		panic(err)
	}
	if err := os.Chdir(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
