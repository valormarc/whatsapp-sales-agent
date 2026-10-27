package wants

import (
	"path/filepath"
	"testing"
)

// The want book: per shop, deduplicated within a day, and each want
// alerted at most once.

func newBook(t *testing.T) *Book {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "wants.json"))
}

func TestRecordDedupesTheSameAskTwiceInADay(t *testing.T) {
	b := newBook(t)
	if !b.Record("ama", "233550004401", "I need a tv") {
		t.Fatal("first recording should add an entry")
	}
	if b.Record("ama", "233550004401", "  i NEED a tv ") {
		t.Fatal("the same ask in different dress must not be recorded twice")
	}
	if got := len(b.List("ama")); got != 1 {
		t.Fatalf("wants for ama = %d, want 1", got)
	}
	// A different buyer asking the same thing is a different want.
	if !b.Record("ama", "233550004402", "I need a tv") {
		t.Fatal("another buyer's identical ask is her own want")
	}
	if got := len(b.List("ama")); got != 2 {
		t.Fatalf("wants for ama = %d, want 2", got)
	}
}

func TestWantsAreKeptPerShop(t *testing.T) {
	b := newBook(t)
	b.Record("ama", "233550004401", "I need a tv")
	b.Record("efua", "233550004402", "I need lace fabric")
	if got := b.List("ama"); len(got) != 1 || got[0].Text != "I need a tv" {
		t.Fatalf("ama's wants = %+v", got)
	}
	if got := b.List("efua"); len(got) != 1 || got[0].Text != "I need lace fabric" {
		t.Fatalf("efua's wants = %+v", got)
	}
	if got := b.List("nobody"); len(got) != 0 {
		t.Fatalf("an unknown shop has wants: %+v", got)
	}
}

func TestMatchAndMarkAlertsEachWantOnce(t *testing.T) {
	b := newBook(t)
	b.Record("ama", "233550004401", "I need a tv")
	b.Record("ama", "233550004402", "do you have a television stand")
	b.Record("ama", "233550004403", "tilapia needed")
	b.Record("efua", "233550004401", "I need a tv")

	matched := b.MatchAndMark("ama", "TV")
	if len(matched) != 1 || matched[0].Buyer != "233550004401" {
		t.Fatalf("matched = %+v, want only ama's tv want", matched)
	}
	// The same product arriving again alerts nobody a second time.
	if again := b.MatchAndMark("ama", "TV"); len(again) != 0 {
		t.Fatalf("second match = %+v, want none", again)
	}
	// The other shop's identical want is untouched by ama's match.
	if got := b.MatchAndMark("efua", "TV"); len(got) != 1 || got[0].Buyer != "233550004401" {
		t.Fatalf("efua match = %+v, want her tv want", got)
	}
	// A want names the product inside a longer name, the other way.
	if got := b.MatchAndMark("ama", "Smoked Tilapia"); len(got) != 1 || got[0].Buyer != "233550004403" {
		t.Fatalf("tilapia match = %+v", got)
	}
}
