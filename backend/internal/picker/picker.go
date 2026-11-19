// Package picker chooses each quiz question. It is pure: the caller supplies
// the candidates (active questions not shown in the last 7 days) and the
// learner's current rating. See "Picking questions for a quiz" in quizroom.md.
package picker

import (
	"math"
	"math/rand/v2"
	"sort"
	"time"

	"quizroom/internal/rating"
)

const (
	MaxLength       = 10
	MinLength       = 5
	MaxMistakeSlots = 3
	TopN            = 5
	MaxPerSubtopic  = 3
	FitWindow       = 0.15
	MistakePMin     = 0.5
	MistakePMax     = 0.9
	RepeatWindow    = 7 * 24 * time.Hour
)

// Candidate is a question that may be shown.
type Candidate struct {
	QuestionID int64
	SubtopicID int64
	B          float64
	C          float64
	NeverSeen  bool
	MistakeIDs []int64
	Multi      bool // a pick-all question
}

// Quiz is the state the picker needs from the quiz in progress.
type Quiz struct {
	Theta          float64
	Target         float64
	PrevSubtopic   int64 // 0 when there is no previous question
	SubtopicCounts map[int64]int
}

// Pick is a chosen question with its prediction. Thin means even the closest
// candidate was more than FitWindow from the target.
type Pick struct {
	Candidate Candidate
	P         float64
	Thin      bool
}

type scored struct {
	c   Candidate
	p   float64
	gap float64
}

func score(cands []Candidate, q Quiz) []scored {
	out := make([]scored, len(cands))
	for i, c := range cands {
		p := rating.Predict(q.Theta, c.B, c.C)
		out[i] = scored{c: c, p: p, gap: math.Abs(p - q.Target)}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].gap != out[j].gap {
			return out[i].gap < out[j].gap
		}
		return out[i].c.QuestionID < out[j].c.QuestionID
	})
	return out
}

func filter(cands []Candidate, keep func(Candidate) bool) []Candidate {
	var out []Candidate
	for _, c := range cands {
		if keep(c) {
			out = append(out, c)
		}
	}
	return out
}

// Rating picks a question for a rating slot. It returns false only when there
// are no candidates at all.
func Rating(cands []Candidate, q Quiz, rng *rand.Rand) (Pick, bool) {
	if len(cands) == 0 {
		return Pick{}, false
	}
	underCap := func(c Candidate) bool { return q.SubtopicCounts[c.SubtopicID] < MaxPerSubtopic }
	pool := filter(cands, func(c Candidate) bool { return c.SubtopicID != q.PrevSubtopic && underCap(c) })
	if len(pool) < TopN {
		pool = filter(cands, underCap)
	}
	if len(pool) < TopN {
		pool = cands
	}

	ranked := score(pool, q)
	top := ranked[:min(TopN, len(ranked))]
	var good []scored
	for _, s := range top {
		if s.gap <= FitWindow {
			good = append(good, s)
		}
	}
	if len(good) == 0 {
		good = top[:1]
	}
	var fresh []scored
	for _, s := range good {
		if s.c.NeverSeen {
			fresh = append(fresh, s)
		}
	}
	choice := good
	if len(fresh) > 0 {
		choice = fresh
	}
	s := choice[rng.IntN(len(choice))]
	return Pick{Candidate: s.c, P: s.p, Thin: top[0].gap > FitWindow}, true
}

// Mistake picks an unseen question tagged with the mistake whose prediction is
// between MistakePMin and MistakePMax, at random among the TopN closest fits.
func Mistake(cands []Candidate, mistakeID int64, q Quiz, rng *rand.Rand) (Pick, bool) {
	tagged := filter(cands, func(c Candidate) bool {
		if !c.NeverSeen {
			return false
		}
		for _, id := range c.MistakeIDs {
			if id == mistakeID {
				return true
			}
		}
		return false
	})
	var ok []scored
	for _, s := range score(tagged, q) {
		if s.p >= MistakePMin && s.p <= MistakePMax {
			ok = append(ok, s)
		}
	}
	if len(ok) == 0 {
		return Pick{}, false
	}
	top := ok[:min(TopN, len(ok))]
	s := top[rng.IntN(len(top))]
	return Pick{Candidate: s.c, P: s.p}, true
}

// Length is the quiz length for the number of eligible questions, and whether
// a quiz can start at all.
func Length(eligible int) (int, bool) {
	if eligible < MinLength {
		return eligible, false
	}
	return min(MaxLength, eligible), true
}

// MistakeSlots spreads up to MaxMistakeSlots positions (1-based, ascending)
// through a quiz, never at position 1: positions 2..length are split into
// near-equal segments, earlier segments taking the extra position, and one
// position is drawn from each segment.
func MistakeSlots(length, due int, rng *rand.Rand) []int {
	k := min(MaxMistakeSlots, due, length-1)
	if k <= 0 {
		return []int{}
	}
	span := length - 1 // positions 2..length
	slots := make([]int, 0, k)
	start := 2
	for i := 0; i < k; i++ {
		size := span / k
		if i < span%k {
			size++
		}
		slots = append(slots, start+rng.IntN(size))
		start += size
	}
	return slots
}

// Skill checks measure the learner fast: each question is one of the most
// informative left, and the check stops once the margin is narrow enough.
const (
	CheckMin        = 20 // available questions needed to start a check
	CheckMax        = 20 // the most questions a check asks
	CheckReserve    = 10 // available questions a check always leaves for practice
	CheckMinAnswers = 10 // answers before a check may stop on its margin
	CheckStopMargin = 30 // ± Elo points at which a check stops
	checkTop        = 3  // pick at random among this many of the most informative
)

// CheckLength is how many questions a check may ask with this many available.
func CheckLength(available int) (int, bool) {
	if available < CheckMin {
		return 0, false
	}
	return min(CheckMax, available-CheckReserve), true
}

// Check picks a skill check's next question: one of the checkTop most
// informative candidates at random, preferring never-seen ones, spreading
// subtopics as Rating does, and no pick-all question once multiLeft is used.
func Check(cands []Candidate, q Quiz, multiLeft int, rng *rand.Rand) (Pick, bool) {
	if multiLeft <= 0 {
		cands = filter(cands, func(c Candidate) bool { return !c.Multi })
	}
	if len(cands) == 0 {
		return Pick{}, false
	}
	underCap := func(c Candidate) bool { return q.SubtopicCounts[c.SubtopicID] < MaxPerSubtopic }
	pool := filter(cands, func(c Candidate) bool { return c.SubtopicID != q.PrevSubtopic && underCap(c) })
	if len(pool) < checkTop {
		pool = filter(cands, underCap)
	}
	if len(pool) < checkTop {
		pool = cands
	}
	type ranked struct {
		c       Candidate
		p, info float64
	}
	rs := make([]ranked, len(pool))
	for i, c := range pool {
		p := rating.Predict(q.Theta, c.B, c.C)
		rs[i] = ranked{c: c, p: p, info: rating.Info(p, c.C)}
	}
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].info != rs[j].info {
			return rs[i].info > rs[j].info
		}
		return rs[i].c.QuestionID < rs[j].c.QuestionID
	})
	top := rs[:min(checkTop, len(rs))]
	var fresh []ranked
	for _, r := range top {
		if r.c.NeverSeen {
			fresh = append(fresh, r)
		}
	}
	if len(fresh) > 0 {
		top = fresh
	}
	r := top[rng.IntN(len(top))]
	return Pick{Candidate: r.c, P: r.p}, true
}
