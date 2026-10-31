package agent

import "testing"

// Run-5 family (eight surfaces, one root): the shared
// figure tokeniser read a k-form figure at face value —
// "900k" verified as 900 — while the platform's own
// figure readers have always honoured the k as a
// thousand. The tokeniser now carries the k and every
// parse of its tokens goes through parseFigure, which
// applies the thousand. These tests pin the root and
// two of the surfaces the audit named; the rest share
// the same two functions.

func TestKFigureVerifiesAtItsTrueValue(t *testing.T) {
	if figuresClean("It is GH₵900k, exactly as listed.", 900) {
		t.Fatal("GH₵900k must not verify against a list price of 900")
	}
	if !figuresClean("It is GH₵900k, exactly as listed.", 900000) {
		t.Fatal("GH₵900k must verify against a list price of 900,000")
	}
	if !figuresClean("It is GH₵900, exactly as listed.", 900) {
		t.Fatal("a plain figure must still verify at face value")
	}
	// "1kg" is a weight, not a thousand: it tokenises as
	// its plain 1 and licenses nothing larger.
	if figuresClean("It weighs 1kg and costs GH₵500.", 500, 1000) {
		t.Fatal("the k in 1kg must not mint a figure of 1000")
	}
}

func TestBurstFiguresReadTheK(t *testing.T) {
	got := allFigures("170k, 75k")
	if len(got) != 2 || got[0] != 170000 || got[1] != 75000 {
		t.Fatalf("a batch answer in k shorthand must read as thousands, got %v", got)
	}
}

func TestBudgetReadsTheK(t *testing.T) {
	if v := netBudget("my budget is 5k"); v != 5000 {
		t.Fatalf("a budget stated as 5k must carry GH₵5,000, got %v", v)
	}
	if v := netBudget("my budget is 500"); v != 500 {
		t.Fatalf("a plain budget must carry at face value, got %v", v)
	}
	if !budgetFigureIn("my budget is 5k", 5000) {
		t.Fatal("the budget verification must find the k figure in her words at its true value")
	}
}
