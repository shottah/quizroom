package mistakes

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

func TestInitial(t *testing.T) {
	for _, src := range []Source{Seed, Mock, Chat} {
		got := Initial(src, t0)
		if got != (State{Status: Active, Stage: 0, DueAt: t0}) {
			t.Fatalf("Initial(%s) = %+v", src, got)
		}
	}
	if got := Initial(Quiz, t0); got != (State{Status: Watching}) {
		t.Fatalf("Initial(quiz) = %+v", got)
	}
}

// Hit takes no prior state: from watching, resolved or any active stage, a hit
// always restarts the schedule at stage 0, due immediately.
func TestHitResetsFromAnyState(t *testing.T) {
	at := t0.Add(5 * time.Hour)
	want := State{Status: Active, Stage: 0, DueAt: at}
	if got := Hit(at); got != want {
		t.Fatalf("Hit = %+v, want %+v", got, want)
	}
	// Replay proves the reset from a late stage and from resolved.
	late := Replay(Initial(Seed, t0), []Event{{ClearEvent, t0}, {ClearEvent, t0}, {ClearEvent, t0}, {HitEvent, at}})
	if late != want {
		t.Fatalf("hit after stage 3 = %+v, want %+v", late, want)
	}
	resolved := State{Status: Resolved, Stage: 4}
	if got := Replay(resolved, []Event{{HitEvent, at}}); got != want {
		t.Fatalf("hit after resolved = %+v, want %+v", got, want)
	}
}

func TestAdvanceSchedule(t *testing.T) {
	s := Initial(Seed, t0)
	wantDue := []time.Time{
		t0.Add(24 * time.Hour),
		t0.Add(24 * time.Hour).Add(72 * time.Hour),
		t0.Add(24 * time.Hour).Add(72 * time.Hour).Add(168 * time.Hour),
		t0.Add(24 * time.Hour).Add(72 * time.Hour).Add(168 * time.Hour).Add(504 * time.Hour),
	}
	for i, due := range wantDue {
		s = Advance(s, s.DueAt)
		if s.Status != Active || s.Stage != i+1 || !s.DueAt.Equal(due) {
			t.Fatalf("after advance %d: %+v, want stage %d due %v", i+1, s, i+1, due)
		}
	}
	s = Advance(s, s.DueAt)
	if s.Status != Resolved || !s.DueAt.IsZero() {
		t.Fatalf("fifth advance = %+v, want resolved", s)
	}
}

func TestIsDue(t *testing.T) {
	quizStart := t0
	if !IsDue(State{Status: Active, DueAt: t0}, quizStart) {
		t.Fatal("active and due at quiz start should be due")
	}
	if IsDue(State{Status: Active, DueAt: t0.Add(time.Second)}, quizStart) {
		t.Fatal("due after quiz start should not be due")
	}
	if IsDue(State{Status: Watching}, quizStart) || IsDue(State{Status: Resolved}, quizStart) {
		t.Fatal("watching and resolved are never due")
	}
}

func TestReplay(t *testing.T) {
	tHit1 := t0.Add(1 * time.Hour)
	tClear1 := t0.Add(2 * time.Hour)
	tClear2 := t0.Add(30 * time.Hour)
	tHit2 := t0.Add(40 * time.Hour)
	events := []Event{
		{Kind: HitEvent, At: tHit1},
		{Kind: ClearEvent, At: tClear1},
		{Kind: ClearEvent, At: tClear2},
		{Kind: HitEvent, At: tHit2},
		{Kind: ClearEvent, At: tHit2},
	}
	got := Replay(Initial(Seed, t0), events)
	want := State{Status: Active, Stage: 1, DueAt: tHit2.Add(24 * time.Hour)}
	if got != want {
		t.Fatalf("Replay = %+v, want %+v", got, want)
	}
	if got := Replay(Initial(Quiz, t0), nil); got != (State{Status: Watching}) {
		t.Fatalf("Replay with no events = %+v", got)
	}
}

// Final review #6: flagging a question replays the other events; a clear left
// over from another question must not schedule a mistake that isn't active.
func TestAdvanceIgnoresInactive(t *testing.T) {
	if got := Replay(Initial(Quiz, t0), []Event{{ClearEvent, t0.Add(time.Hour)}}); got != (State{Status: Watching}) {
		t.Fatalf("a clear on a watching mistake = %+v, want it still watching", got)
	}
	resolved := State{Status: Resolved, Stage: 4}
	if got := Advance(resolved, t0); got != resolved {
		t.Fatalf("Advance(resolved) = %+v, want unchanged", got)
	}
}
