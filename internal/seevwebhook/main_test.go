package seevwebhook

import (
	"os"
	"testing"
)

// TestMain runs the package's tests from a scratch
// directory: the books and the outbox resolve their files
// through the shared data-dir path relative to the working
// directory, and a test must not leave state inside the
// source tree.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "seevwebhook-tests-")
	if err != nil {
		panic(err)
	}
	if err := os.Chdir(dir); err != nil {
		panic(err)
	}
	if err := os.MkdirAll("data", 0o700); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
