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
	sys := "test"
	return Chat([]Message{{Role: "user", Content: "hi"}}, Options{System: &sys, Kind: kind})
}

func TestTurnBudgetEnforced(t *testing.T) {
	hits := countingServer(t)
	// Leave the package as the test found it: unarmed, so
	// the other tests in this package keep their uncounted
	// contract.
	t.Cleanup(func() {
		turnArmed.Store(false)
		turnUnderstand.Store(0)
		turnDraft.Store(0)
	})

	// Before any BeginTurn, calls are uncounted.
	turnArmed.Store(false)
	for i := 0; i < 3; i++ {
		if chatOnce(KindUnderstand) == nil {
			t.Fatal("unarmed calls must not be refused")
		}
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("unarmed hits = %d, want 3", got)
	}

	// One turn: understanding capped at one, drafting at two.
	BeginTurn()
	if chatOnce(KindUnderstand) == nil {
		t.Fatal("the turn's understanding call must go through")
	}
	if chatOnce(KindUnderstand) != nil {
		t.Fatal("a second understanding call in one turn must be refused")
	}
	if chatOnce(KindDraft) == nil || chatOnce(KindDraft) == nil {
		t.Fatal("two drafting calls fit the turn")
	}
	if chatOnce(KindDraft) != nil {
		t.Fatal("a third drafting call in one turn must be refused")
	}
	if got := hits.Load(); got != 6 {
		t.Fatalf("hits = %d, want 6 (3 unarmed + 1 understand + 2 drafts)", got)
	}

	// JudgeShape is drafting-class: it spends draft budget,
	// never the understanding call.
	BeginTurn()
	if _, _, _ = JudgeShape("Heels", "Nice heels"); hits.Load() != 7 {
		t.Fatalf("hits = %d, want 7 after JudgeShape", hits.Load())
	}
	if chatOnce(KindUnderstand) == nil {
		t.Fatal("the understanding call must still be free after a drafting judgement")
	}

	// A new turn reopens both budgets.
	BeginTurn()
	if chatOnce(KindUnderstand) == nil {
		t.Fatal("a fresh turn must allow an understanding call again")
	}
}
