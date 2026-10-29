package insights

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/daybook"
)

func tmpBook(t *testing.T) *daybook.Daybook {
	t.Helper()
	return daybook.New(filepath.Join(t.TempDir(), "daybook.json"))
}

func TestWalkAwaysCountedInDayBook(t *testing.T) {
	book := tmpBook(t)
	book.RecordWalkAway("m", daybook.Options{Allowance: 300, Date: "2026-10-22"})
	s := book.RecordWalkAway("m", daybook.Options{Allowance: 300, Date: "2026-10-22"})
	if s.WalkAways != 2 {
		t.Fatalf("walkAways %d, want 2", s.WalkAways)
	}
}

func TestDayReportSpeaksInDayLanguage(t *testing.T) {
	text := DayReportText(daybook.State{Sales: 2, Revenue: 1130, GivenAway: 170, WalkAways: 1, Allowance: 300})
	if !regexp.MustCompile(`2 sale\(s\) today, GH₵1,130 in`).MatchString(text) {
		t.Fatalf("report %q missing sales line", text)
	}
	if !regexp.MustCompile(`GH₵170 of your GH₵300 allowance, GH₵130 left`).MatchString(text) {
		t.Fatalf("report %q missing giveaway line", text)
	}
	if !regexp.MustCompile(`1 customer\(s\) walked away`).MatchString(text) {
		t.Fatalf("report %q missing walk-away line", text)
	}
}

func TestDayReportOmitsWalkAwaysWhenNone(t *testing.T) {
	text := DayReportText(daybook.State{Sales: 1, Revenue: 400, GivenAway: 50, WalkAways: 0, Allowance: 300})
	if regexp.MustCompile(`walked away`).MatchString(text) {
		t.Fatalf("report %q should not mention walk-aways", text)
	}
}

func TestWalkAwaysAlongsideSalesInOneDayState(t *testing.T) {
	book := tmpBook(t)
	book.RecordSale("m", daybook.Sale{ListPrice: 850, AgreedPrice: 730, Allowance: 300, Date: "2026-10-22"})
	s := book.RecordWalkAway("m", daybook.Options{Allowance: 300, Date: "2026-10-22"})
	if s.Sales != 1 || s.WalkAways != 1 {
		t.Fatalf("got %+v, want 1 sale and 1 walk-away", s)
	}
}
