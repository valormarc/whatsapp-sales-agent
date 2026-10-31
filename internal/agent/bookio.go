package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// The agent-side books (trust, in-hands, fee quotes) keep
// the same file discipline as the money books, through
// these two helpers: a save is a temp file renamed over
// the book, so a crash mid-write leaves the old book or
// the new one and never a torn half; a load that cannot
// parse preserves the file under a .corrupt- name and
// reports the failure instead of silently reading a
// control book as empty (the 2026-10-30 audit showed a
// torn trust write quietly unflagging a three-strike
// seller, because the load swallowed the parse error).
//
// Two lessons from the second audit are built in here.
// The temp file is unique to each save: a shared temp
// path let parallel writers race the file itself, so the
// last writer's stale copy won whole. And every good save
// also leaves a last-known-good copy beside the book, so
// a load that meets corrupt bytes restores from that copy
// in-process instead of reading a control book as empty
// until an operator happens by.
//
// The fifth audit closed the discipline's last gap here:
// when a book AND its backup both fail to parse, the load
// read the book as empty and the next routine save
// cemented that empty read over both copies. These
// helpers now carry the money books' full posture: a
// double failure poisons the path, saves to a poisoned
// path are refused until a load parses again, and a
// successful restore repairs the main file with the good
// bytes so later loads are clean.

var bookPoisonMu sync.Mutex
var bookPoisoned = map[string]bool{}

// bookPoisonKey keys the poison registry by absolute
// path: books are addressed by relative paths under a
// working directory that differs between processes (and
// between tests), and one directory's corrupt book must
// never poison another directory's healthy one.
func bookPoisonKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func bookPathPoisoned(path string) bool {
	bookPoisonMu.Lock()
	defer bookPoisonMu.Unlock()
	return bookPoisoned[bookPoisonKey(path)]
}

func setBookPoisoned(path string, poisoned bool) {
	bookPoisonMu.Lock()
	defer bookPoisonMu.Unlock()
	if poisoned {
		bookPoisoned[bookPoisonKey(path)] = true
	} else {
		delete(bookPoisoned, bookPoisonKey(path))
	}
}

func saveBookJSON(path string, v any) {
	if bookPathPoisoned(path) {
		fmt.Fprintf(os.Stderr, "agent: refusing to save over %s: the last load did not parse and no good copy could be restored\n", path)
		return
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	if !writeFileAtomic(path, data) {
		return
	}
	// The last-known-good copy rides the same bytes; a
	// failure here costs only the safety net, not the save.
	_ = writeFileAtomic(path+".bak", data)
}

// writeFileAtomic writes data to a temp file unique to
// this call and renames it over path. It reports whether
// the rename landed.
func writeFileAtomic(path string, data []byte) bool {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return false
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(name)
		return false
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return false
	}
	_ = os.Chmod(name, 0o600)
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return false
	}
	return true
}

// loadBookJSON reads and parses the book at path into v.
// It returns false when the file is missing (a fresh book,
// not a failure). When the file does not parse, the bytes
// are preserved under a single .corrupt name and reported
// on stderr, and the last-known-good copy is tried before
// giving up: a book restored from its backup is a working
// book — its main file repaired with the good bytes —
// where the audit's alternative was a control read as
// empty for the life of the process. When the backup
// fails too, the path is poisoned and later saves refuse.
func loadBookJSON(path string, v any) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		// An existing book that cannot be read is a failed
		// load, not a fresh one (the sixth audit): try the
		// last known good copy, and poison the path when
		// none restores, so the save refusals engage
		// exactly as they do for unparseable bytes.
		if bak, berr := os.ReadFile(path + ".bak"); berr == nil && json.Unmarshal(bak, v) == nil {
			fmt.Fprintf(os.Stderr, "agent: %s could not be read (%v); restored from the last known good copy\n", path, err)
			_ = writeFileAtomic(path, bak)
			setBookPoisoned(path, false)
			return true
		}
		fmt.Fprintf(os.Stderr, "agent: %s could not be read (%v) and no good copy could be restored; read as empty; saves are refused until the book loads again\n", path, err)
		setBookPoisoned(path, true)
		return false
	}
	if err := json.Unmarshal(data, v); err != nil {
		bad := path + ".corrupt"
		_ = os.WriteFile(bad, data, 0o600)
		if bak, berr := os.ReadFile(path + ".bak"); berr == nil && json.Unmarshal(bak, v) == nil {
			fmt.Fprintf(os.Stderr, "agent: %s does not parse (%v); preserved as %s and restored from the last known good copy\n", path, err, bad)
			_ = writeFileAtomic(path, bak)
			setBookPoisoned(path, false)
			return true
		}
		fmt.Fprintf(os.Stderr, "agent: %s does not parse (%v); preserved as %s and read as empty; saves are refused until the book parses again\n", path, err, bad)
		setBookPoisoned(path, true)
		return false
	}
	setBookPoisoned(path, false)
	return true
}
