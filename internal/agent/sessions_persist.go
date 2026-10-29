package agent

// Work in progress survives a restart. Sessions used to
// live only in memory, so every restart — a deploy, a
// crash, the platform's own recovery — dropped every open
// listing, every kept photo batch and every buyer mid-haggle
// back to the front door, and the person had to start again
// or reconstruct her place herself. The durable books never
// had this problem; the conversations did.
//
// The fix is a snapshot, not a re-architecture: the whole
// session state (sessions, shop bindings, and the two small
// memories that ride with them) is written to sessions.json
// in the data directory on a short timer and again when the
// process is told to stop, and read back when the process
// starts. A snapshot is written to a temporary file and
// renamed over the old one, so a crash mid-write can never
// leave a torn file behind; a snapshot that cannot be read
// is simply not restored, and everyone starts fresh, which
// is the worst case and the old behaviour. The loss window
// is the timer's interval: seconds of conversation, never
// the conversation.

import (
	"encoding/json"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/valormarc/hackathon-agent/internal/datadir"
)

// sessionSnapshotInterval is how often the session state is
// written out while the process runs. Ten seconds keeps the
// loss window to a breath while the file stays small.
const sessionSnapshotInterval = 10 * time.Second

// sessionSnapshot is the whole in-memory conversation state
// of the process, in one file.
type sessionSnapshot struct {
	Sessions     map[string]*Session `json:"sessions"`
	Bindings     map[string]string   `json:"bindings"`
	DoorShown    map[string]bool     `json:"doorShown"`
	PackageNames map[string]string   `json:"packageNames"`
}

var sessionSnapshotsOnce sync.Once

// StartSessionSnapshots restores any snapshot the last run
// left behind, then keeps the snapshot current: a write
// every interval, and a final write when the process is
// told to stop, after which it exits as it would have.
func StartSessionSnapshots() {
	sessionSnapshotsOnce.Do(func() {
		restoreSessions()
		go func() {
			for {
				time.Sleep(sessionSnapshotInterval)
				SnapshotSessionsNow()
			}
		}()
		stops := make(chan os.Signal, 1)
		signal.Notify(stops, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-stops
			SnapshotSessionsNow()
			os.Exit(0)
		}()
	})
}

// SnapshotSessionsNow writes the current session state to
// the data directory, atomically. It is safe to call at any
// time; the write holds the session lock, as every turn
// does, so a snapshot never catches a half-finished turn.
func SnapshotSessionsNow() {
	sessionsMu.Lock()
	snap := sessionSnapshot{
		Sessions:     sessions,
		Bindings:     bindings,
		DoorShown:    doorShown,
		PackageNames: packageNames,
	}
	data, err := json.Marshal(snap)
	sessionsMu.Unlock()
	if err != nil {
		return
	}
	tmp := datadir.Path("sessions.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, datadir.Path("sessions.json"))
}

// restoreSessions reads the last snapshot back into memory
// at boot. A missing or unreadable snapshot is not an
// error: the process simply starts with empty state, as
// it always did before snapshots existed.
func restoreSessions() {
	data, err := os.ReadFile(datadir.Path("sessions.json"))
	if err != nil {
		return
	}
	var snap sessionSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return
	}
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if snap.Sessions != nil {
		sessions = snap.Sessions
	}
	if snap.Bindings != nil {
		bindings = snap.Bindings
	}
	if snap.DoorShown != nil {
		doorShown = snap.DoorShown
	}
	if snap.PackageNames != nil {
		packageNames = snap.PackageNames
	}
}
