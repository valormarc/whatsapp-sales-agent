// Package datadir resolves the directory that holds the agent's JSON
// data files (catalogue, day book, order book and the dry-run outbox).
//
// The Node implementation anchors these paths to its source tree. The
// Go server is a single binary, so the data directory is taken from the
// process working directory instead: run the binary from the repository
// root and data/ sits beside it, exactly as with the Node app. The
// HACKATHON_AGENT_DATA_DIR variable may point it elsewhere (tests and
// tooling only); it is unset in normal use.
package datadir

import (
	"os"
	"path/filepath"
)

// Dir returns the data directory path.
func Dir() string {
	if d := os.Getenv("HACKATHON_AGENT_DATA_DIR"); d != "" {
		return d
	}
	return "data"
}

// Path returns the full path of a named file inside the data directory.
func Path(name string) string {
	return filepath.Join(Dir(), name)
}
