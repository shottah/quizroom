package picker

import (
	"math"
	"math/rand/v2"
	"testing"
)

// bFor returns the question rating that gives predicted chance p for a
// learner at theta 0 on a four-option single-answer question (c = 0.25).
func bFor(p float64) float64 {
	x := (p - 0.25) / 0.75
	return -math.Log(x / (1 - x))
}

func cand(id int64, sub int64, p float64, neverSeen bool, mistakes ...int64) Candidate {
	return Candidate{QuestionID: id, SubtopicID: sub, B: bFor(p), C: 0.25, NeverSeen: neverSeen, MistakeIDs: mistakes}
}

func quiz() Quiz { return Quiz{Theta: 0, Target: 0.75, SubtopicCounts: map[int64]int{}} }

func seeds(n int) []*rand.Rand {
	out := make([]*rand.Rand, n)
	for i := range out {
		out[i] = rand.New(rand.NewPCG(uint64(i), uint64(i*7+1)))
	}
	return out
}

// spread builds 20 seen candidates in different subtopics. The five closest
// to the target are IDs 1–5 (gaps 0, .02, .03, .05, .06); the rest are > .15.
func spread() []Candidate {
	gaps := []float64{0, 0.02, -0.03, 0.05, -0.06, 0.16, -0.17, 0.18, -0.19, 0.2, -0.21, 0.22, -0.23, -0.24, -0.25, -0.26, -0.27, -0.28, -0.29, -0.3}
	var cs []Candidate
	for i, g := range gaps {
		cs = append(cs, cand(int64(i+1), int64(i+1), 0.75+g, false))
	}
	return cs
}

func TestRatingPicksAmongGoodFits(t *testing.T) {
	seen := map[int64]bool{}
	for _, rng := range seeds(300) {
		p, ok := Rating(spread(), quiz(), rng)
		if !ok {
			t.Fatal("expected a pick")
		}
		if id := p.Candidate.QuestionID; id < 1 || id > 5 {
			t.Fatalf("picked %d, outside the good fits 1–5", id)
		}
		if p.Thin {
			t.Fatal("good fits exist, so the pick isn't thin")
		}
		seen[p.Candidate.QuestionID] = true
	}
	if len(seen) < 3 {
		t.Fatalf("only %d distinct picks; want variety", len(seen))
	}
}

func TestRatingPrefersNewWithinGoodFits(t *testing.T) {
	cs := spread()
	cs[2].NeverSeen = true // ID 3
	cs[4].NeverSeen = true // ID 5
	cs[9].NeverSeen = true // ID 10, gap .2: new but not a good fit
	for _, rng := range seeds(300) {
		p, _ := Rating(cs, quiz(), rng)
		if id := p.Candidate.QuestionID; id != 3 && id != 5 {
			t.Fatalf("picked %d; want one of the new good fits 3 or 5", id)
		}
	}
}

func TestRatingFitBeatsNewness(t *testing.T) {
	cs := []Candidate{cand(1, 1, 0.75, false), cand(2, 2, 0.45, true)}
	for _, rng := range seeds(200) {
		p, _ := Rating(cs, quiz(), rng)
		if p.Candidate.QuestionID != 1 {
			t.Fatalf("picked %d; a well-fitting seen question must beat a badly fitting new one", p.Candidate.QuestionID)
		}
	}
}

func TestRatingRelaxesSubtopicRules(t *testing.T) {
	q := quiz()
	q.PrevSubtopic = 7
	q.SubtopicCounts[7] = 3
	cs := []Candidate{cand(1, 7, 0.75, false), cand(2, 7, 0.7, false), cand(3, 7, 0.8, false)}
	if _, ok := Rating(cs, q, seeds(1)[0]); !ok {
		t.Fatal("when only one subtopic is left, the subtopic rules must give way")
	}
}

func TestRatingDropsPrevRuleBeforeCountRule(t *testing.T) {
	q := quiz()
	q.PrevSubtopic = 7
	q.SubtopicCounts[7] = 1
	q.SubtopicCounts[9] = 3
	cs := []Candidate{
		cand(1, 7, 0.74, false), cand(2, 7, 0.76, false), cand(3, 7, 0.73, false),
		cand(4, 7, 0.77, false), cand(5, 7, 0.72, false),
		cand(6, 9, 0.75, false), // best fit, but subtopic 9 already has 3 in this quiz
	}
	for _, rng := range seeds(200) {
		p, _ := Rating(cs, q, rng)
		if p.Candidate.QuestionID == 6 {
			t.Fatal("dropping the previous-subtopic rule left 5 candidates, so the 3-per-subtopic rule must still hold")
		}
	}
}

func TestRatingThin(t *testing.T) {
	cs := []Candidate{cand(1, 1, 0.4, false), cand(2, 2, 0.45, false), cand(3, 3, 0.5, false)}
	for _, rng := range seeds(50) {
		p, ok := Rating(cs, quiz(), rng)
		if !ok || !p.Thin || p.Candidate.QuestionID != 3 {
			t.Fatalf("got %+v ok=%v; want the closest (3) and Thin", p, ok)
		}
	}
	if _, ok := Rating(nil, quiz(), seeds(1)[0]); ok {
		t.Fatal("no candidates means no pick")
	}
}

func TestMistakePick(t *testing.T) {
	cs := []Candidate{
		cand(1, 1, 0.70, true, 42),
		cand(2, 2, 0.75, false, 42), // seen before
		cand(3, 3, 0.95, true, 42),  // too easy
		cand(4, 4, 0.45, true, 42),  // too hard
		cand(5, 5, 0.75, true),      // not tagged
		cand(6, 6, 0.55, true, 42, 7),
	}
	for _, rng := range seeds(200) {
		p, ok := Mistake(cs, 42, quiz(), rng)
		if !ok {
			t.Fatal("expected a pick")
		}
		if id := p.Candidate.QuestionID; id != 1 && id != 6 {
			t.Fatalf("picked %d; want an unseen tagged question with P in [0.5, 0.9]", id)
		}
	}
	if _, ok := Mistake(cs, 99, quiz(), seeds(1)[0]); ok {
		t.Fatal("no question is tagged with mistake 99")
	}
}

func TestLength(t *testing.T) {
	for eligible, want := range map[int]struct {
		n  int
		ok bool
	}{12: {10, true}, 10: {10, true}, 7: {7, true}, 5: {5, true}, 4: {4, false}, 0: {0, false}} {
		n, ok := Length(eligible)
		if n != want.n || ok != want.ok {
			t.Fatalf("Length(%d) = (%d, %v), want (%d, %v)", eligible, n, ok, want.n, want.ok)
		}
	}
}

func TestMistakeSlots(t *testing.T) {
	for _, rng := range seeds(100) {
		got := MistakeSlots(10, 5, rng)
		if len(got) != 3 || got[0] < 2 || got[0] > 4 || got[1] < 5 || got[1] > 7 || got[2] < 8 || got[2] > 10 {
			t.Fatalf("MistakeSlots(10, 5) = %v; want one each in [2..4], [5..7], [8..10]", got)
		}
		one := MistakeSlots(10, 1, rng)
		if len(one) != 1 || one[0] < 2 || one[0] > 10 {
			t.Fatalf("MistakeSlots(10, 1) = %v", one)
		}
		if s := MistakeSlots(10, 0, rng); len(s) != 0 {
			t.Fatalf("MistakeSlots(10, 0) = %v", s)
		}
		five := MistakeSlots(5, 3, rng)
		if len(five) != 3 || five[0] < 2 || five[2] > 5 || five[0] == five[1] || five[1] == five[2] {
			t.Fatalf("MistakeSlots(5, 3) = %v", five)
		}
		if s := MistakeSlots(1, 3, rng); len(s) != 0 {
			t.Fatalf("MistakeSlots(1, 3) = %v", s)
		}
	}
}

func TestCheckPicksMostInformative(t *testing.T) {
	var cands []Candidate
	id := int64(0)
	add := func(p float64, n int, multi bool) {
		for i := 0; i < n; i++ {
			id++
			c := 0.25
			cands = append(cands, Candidate{QuestionID: id, SubtopicID: id, B: bFor(p), C: c, NeverSeen: true, Multi: multi})
		}
	}
	add(0.68, 3, false) // ids 1–3: the most informative
	add(0.95, 4, false)
	add(0.40, 4, false)
	rng := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 200; i++ {
		pick, ok := Check(cands, Quiz{Theta: 0, SubtopicCounts: map[int64]int{}}, 5, rng)
		if !ok || pick.Candidate.QuestionID > 3 {
			t.Fatalf("run %d picked %+v, want one of the three 68%% questions", i, pick)
		}
	}
	multi := []Candidate{{QuestionID: 99, SubtopicID: 9, B: bFor(0.68), C: 0.25, NeverSeen: true, Multi: true}}
	multi = append(multi, cands[3:]...)
	for i := 0; i < 50; i++ {
		if pick, _ := Check(multi, Quiz{Theta: 0, SubtopicCounts: map[int64]int{}}, 0, rng); pick.Candidate.Multi {
			t.Fatal("no pick-all question once the check's quota is used")
		}
	}
}

func TestCheckLength(t *testing.T) {
	if _, ok := CheckLength(19); ok {
		t.Fatal("19 available can't start a check")
	}
	for available, want := range map[int]int{20: 10, 25: 15, 45: 20} {
		if got, ok := CheckLength(available); !ok || got != want {
			t.Fatalf("CheckLength(%d) = %d %v, want %d", available, got, ok, want)
		}
	}
}
