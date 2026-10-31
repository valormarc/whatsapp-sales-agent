package model

// The per-turn budget, enforced: one understanding call a
// turn, drafting on its own cap of two, a fresh budget at
// BeginTurn, and no counting at all before the first
// BeginTurn (the state one-off callers and these tests
// start in). A refused call returns nil, the fail-soft
// shape every caller already handles.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func countingServer(t *testing.T) *atomic.Int64 {
	t.Helper()
	hits := &atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("content-type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"unit\":\"piece\",\"certain\":true,\"sizes\":[]}"}}]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("META_MODEL_API_KEY", "test-key")
	t.Setenv("META_MODEL_API_BASE", srv.URL)
	return hits
}

func chatOnce(kind int) *string {
	return chatOnceTurn(kind, "")
}

func chatOnceTurn(kind int, turn string) *string {
	sys := "test"
	return Chat([]Message{{Role: "user", Content: "hi"}}, Options{System: &sys, Kind: kind, Turn: turn})
}

func TestTurnBudgetEnforced(t *testing.T) {
	hits := countingServer(t)
	// Leave the package as the test found it: unarmed, so
	// the other tests in this package keep their uncounted
	// contract.
	// Calls carrying no turn token are uncounted.
	for i := 0; i < 3; i++ {
		if chatOnce(KindUnderstand) == nil {
			t.Fatal("unarmed calls must not be refused")
		}
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("unarmed hits = %d, want 3", got)
	}

	// One turn: understanding capped at one, drafting at two.
	turn := BeginTurn()
	defer EndTurn(turn)
	if chatOnceTurn(KindUnderstand, turn) == nil {
		t.Fatal("the turn's understanding call must go through")
	}
	if chatOnceTurn(KindUnderstand, turn) != nil {
		t.Fatal("a second understanding call in one turn must be refused")
	}
	if chatOnceTurn(KindDraft, turn) == nil || chatOnceTurn(KindDraft, turn) == nil {
		t.Fatal("two drafting calls fit the turn")
	}
	if chatOnceTurn(KindDraft, turn) != nil {
		t.Fatal("a third drafting call in one turn must be refused")
	}
	// A second turn running alongside keeps its own budget:
	// the first turn's spent calls never refuse it.
	other := BeginTurn()
	defer EndTurn(other)
	if chatOnceTurn(KindUnderstand, other) == nil {
		t.Fatal("a parallel turn's understanding call must go through")
	}
	if got := hits.Load(); got != 7 {
		t.Fatalf("hits = %d, want 7 (3 uncounted + 1 understand + 2 drafts + 1 parallel understand)", got)
	}

	// JudgeShape is drafting-class and charged to the
	// turn that calls it (run-2: in-turn callers passed no
	// token and bypassed the budget entirely). It must not
	// touch the turn's understanding budget either way.
	turn2 := BeginTurn()
	defer EndTurn(turn2)
	if _, _, _ = JudgeShape("Heels", "Nice heels", turn2); hits.Load() != 8 {
		t.Fatalf("hits = %d, want 8 after JudgeShape", hits.Load())
	}
	// Two drafting calls fit the turn; the third is
	// refused before it reaches the wire.
	_, _, _ = JudgeShape("Heels", "Nice heels", turn2)
	if _, _, _ = JudgeShape("Heels", "Nice heels", turn2); hits.Load() != 9 {
		t.Fatalf("hits = %d, want 9: the third drafting call in a turn must be refused", hits.Load())
	}
	if chatOnceTurn(KindUnderstand, turn2) == nil {
		t.Fatal("the understanding call must still be free after a drafting judgement")
	}

	// A new turn reopens both budgets.
	turn3 := BeginTurn()
	defer EndTurn(turn3)
	if chatOnceTurn(KindUnderstand, turn3) == nil {
		t.Fatal("a fresh turn must allow an understanding call again")
	}
}
