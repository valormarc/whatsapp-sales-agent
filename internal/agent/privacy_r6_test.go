package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/datadir"
)

// Run-6 records, erasure family: the purge's aside filter
// matched only the retired timestamped shape; narration
// was read as an ask; the restore filter missed the
// package-names and door-shown maps and foreign armed
// questions; the tombstone's second precision let a
// same-second session survive.

func TestPurgePreservedCopiesCurrentAsideShape(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	her := "233550000881"
	dir := datadir.Dir()
	files := map[string]string{
		"trust.json.corrupt":          "buyer " + her + " record",
		"trust.json.corrupt-20260101": "buyer " + her + " record",
		"orders.json.bak":             "buyer " + her + " record",
		"orders.json.corrupt":         "someone else entirely",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	purgePreservedCopies(her)
	for name := range files {
		_, err := os.Stat(filepath.Join(dir, name))
		if name == "orders.json.corrupt" {
			if err != nil {
				t.Fatalf("a preserved copy without her number must survive: %v", err)
			}
			continue
		}
		if !os.IsNotExist(err) {
			t.Fatalf("%s carries her number and must be purged", name)
		}
	}
}

func TestDeleteMyDataNarrationIsNotAnAsk(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"she said, delete my data", false},
		{"my sister told me: delete my data", false},
		{"please delete my data", true},
		// First-person framing was never an ask shape on
		// this lane (the clause is no lead-in), and the
		// record does not ask for it to become one: an
		// unrecognised phrasing is re-asked, an irreversible
		// erasure on reported speech is not recoverable.
		{"i said delete my data", false},
		{"do not delete my data", false},
		{"delete my data", true},
	}
	for _, c := range cases {
		if got := isDeleteMyDataAsk(c.text); got != c.want {
			t.Fatalf("isDeleteMyDataAsk(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestRestoreFiltersEverySnapshotMap(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	her := "233550000882"
	control := "233550000883"
	merchantNumber := "233550000884"
	old := time.Now().Add(-time.Hour)
	// The arrange/restore cycle runs through a closure
	// for the standing harness hazard named in
	// restore_fixes_test.go: a leaked snapshot write from
	// another test's completion goroutine can land in the
	// wipe window and empty this test's snapshot. A
	// missing control after the restore re-runs the
	// cycle once; the sweep logic is deterministic.
	arrange := func() {
		packageNames.put(sessionKey("demo", her), "Her Parcel")
		packageNames.put(sessionKey("demo", control), "Control Parcel")
		doorShown.put(her, true)
		doorShown.put(control, true)
		sessions.put(sessionKey("demo", merchantNumber), &Session{
			Stage: "fulfilment", MerchantID: "demo", TouchedAt: old,
			YesNoQ: &YesNoQuestion{Kind: "inhand", Ref: her, Body: "Is the bag in hand?"},
		})
		sessions.put(sessionKey("demo", "233550000885"), &Session{
			Stage: "fulfilment", MerchantID: "demo", TouchedAt: old,
			YesNoQ: &YesNoQuestion{Kind: "inhand", Ref: control, Body: "Is the bag in hand?"},
		})
		SnapshotSessionsNow()
		sessions.replaceAll(nil)
		packageNames.replaceAll(nil)
		doorShown.replaceAll(nil)
		erasedMu.Lock()
		erasedLoaded, erasedParties = false, nil
		erasedMu.Unlock()
		markErased(her)
		restoreSessions()
	}
	arrange()
	if packageNames.get(sessionKey("demo", control)) == "" {
		arrange()
	}
	if got := packageNames.get(sessionKey("demo", her)); got != "" {
		t.Fatalf("her package name must not restore, got %q", got)
	}
	if got := packageNames.get(sessionKey("demo", control)); got != "Control Parcel" {
		t.Fatalf("the control package name must restore, got %q", got)
	}
	if doorShown.get(her) {
		t.Fatal("her door-shown mark must not restore")
	}
	if !doorShown.get(control) {
		t.Fatal("the control door-shown mark must restore")
	}
	if ms := sessions.get(sessionKey("demo", merchantNumber)); ms == nil || ms.YesNoQ != nil {
		t.Fatalf("the merchant session must survive with its erased-party question cleared, got %+v", ms)
	}
	if cs := sessions.get(sessionKey("demo", "233550000885")); cs == nil || cs.YesNoQ == nil {
		t.Fatal("the control foreign question must be untouched")
	}
	erasedMu.Lock()
	erasedLoaded, erasedParties = false, nil
	erasedMu.Unlock()
}

func TestTombstoneSubSecondPrecision(t *testing.T) {
	setupChat(t, stockOwner, shopWithBag)
	her := "233550000886"
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// Erasure at .50 of the second; her session was
	// touched at .25 (before it) and the control at .75
	// is a different party entirely.
	erasedMu.Lock()
	erasedLoaded = true
	erasedParties = []erasedParty{{Number: her, At: base.Add(500 * time.Millisecond).Format(time.RFC3339Nano)}}
	erasedMu.Unlock()
	sessions.put(sessionKey("demo", her), &Session{Stage: "browse", MerchantID: "demo", TouchedAt: base.Add(250 * time.Millisecond)})
	SnapshotSessionsNow()
	sessions.replaceAll(nil)
	restoreSessions()
	if s := sessions.get(sessionKey("demo", her)); s != nil {
		t.Fatal("a session touched before the erasure, in the same second, must not survive the tombstone")
	}
	// And one touched after it (.75) is genuinely hers to keep.
	// (One re-run against the leaked-snapshot hazard
	// named in restore_fixes_test.go.)
	postErasure := func() {
		sessions.put(sessionKey("demo", her), &Session{Stage: "browse", MerchantID: "demo", TouchedAt: base.Add(750 * time.Millisecond)})
		SnapshotSessionsNow()
		sessions.replaceAll(nil)
		restoreSessions()
	}
	postErasure()
	if sessions.get(sessionKey("demo", her)) == nil {
		postErasure()
	}
	if s := sessions.get(sessionKey("demo", her)); s == nil {
		t.Fatal("a session touched after the erasure must survive")
	}
	erasedMu.Lock()
	erasedLoaded, erasedParties = false, nil
	erasedMu.Unlock()
}
