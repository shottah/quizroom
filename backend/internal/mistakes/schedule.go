// Package mistakes holds the review schedule for recurring mistakes: a hit
// restarts it, each right answer on a new question while due moves it along
// (1, 3, 7, then 21 days), and one more right answer resolves it.
// See the "Mistakes" section of quizroom.md.
package mistakes

import "time"

// Status is where a mistake stands.
type Status string

const (
	// Watching mistakes are on the list (Claude invented them for a wrong
	// option) but aren't scheduled until the learner picks that option.
	Watching Status = "watching"
	Active   Status = "active"
	Resolved Status = "resolved"
)

// Source records where a mistake came from.
type Source string

const (
	Seed Source = "seed" // a starting list that older installs were seeded with
	Quiz Source = "quiz" // Claude invented it for a wrong option
	Chat Source = "chat" // the learner described it in the chat
	Mock Source = "mock" // the agent added it after a mock interview
)

// State is a mistake's place in its schedule. A zero DueAt means unscheduled.
type State struct {
	Status Status
	Stage  int
	DueAt  time.Time
}

// Gaps are the waits after each clear, by stage.
var Gaps = [4]time.Duration{24 * time.Hour, 72 * time.Hour, 168 * time.Hour, 504 * time.Hour}

// Initial is a new mistake's state: watching when Claude invented it,
// otherwise active and due straight away.
func Initial(src Source, at time.Time) State {
	if src == Quiz {
		return State{Status: Watching}
	}
	return State{Status: Active, Stage: 0, DueAt: at}
}

// Hit restarts the schedule: active, stage 0, due immediately.
func Hit(at time.Time) State {
	return State{Status: Active, Stage: 0, DueAt: at}
}

// Advance records a right answer on a new question while the mistake was due.
func Advance(s State, at time.Time) State {
	if s.Status != Active {
		return s // a clear never schedules a watching or resolved mistake
	}
	if s.Stage >= len(Gaps) {
		return State{Status: Resolved, Stage: len(Gaps)}
	}
	return State{Status: Active, Stage: s.Stage + 1, DueAt: at.Add(Gaps[s.Stage])}
}

// IsDue reports whether the mistake should come back in a quiz that started
// at quizStart. A mistake hit during a quiz is due from the next quiz on.
func IsDue(s State, quizStart time.Time) bool {
	return s.Status == Active && !s.DueAt.After(quizStart)
}

// EventKind is a recorded change to a mistake.
type EventKind string

const (
	HitEvent   EventKind = "hit"
	ClearEvent EventKind = "clear"
)

// Event is one recorded hit or clear.
type Event struct {
	Kind EventKind
	At   time.Time
}

// Replay rebuilds a mistake's state from its initial state and its events in
// time order. Flagging a bad question deletes its events and replays the rest.
func Replay(initial State, events []Event) State {
	s := initial
	for _, e := range events {
		switch e.Kind {
		case HitEvent:
			s = Hit(e.At)
		case ClearEvent:
			s = Advance(s, e.At)
		}
	}
	return s
}
