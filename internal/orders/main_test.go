package orders

import (
	"os"
	"testing"
)

// TestMain runs the package's tests from a scratch
// directory: the books create their data directory on save
// through the shared data-dir path, and a test must not
// leave that directory inside the source tree.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "orders-tests-")
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
