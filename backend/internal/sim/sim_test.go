package sim

// Simulation of the core requirement in quizroom.md ("Testing"): question
// difficulty follows an improving learner gradually, with no pass-mark jumps.
//
// Why these criteria (changed from the plan's first draft, see the ledger):
// a per-quiz limit of 1.5 steps on the mean question difficulty measured the
// rating's own noise, not jumps. With the spec's K floor of 0.2, even perfect
// picking moves a quiz's level by about 0.6 steps (one SD), so the largest of
// 45 quiz-to-quiz changes lands near 1.5 whatever the picker does. A pass-mark
// design differs in kind, not degree: its aimed difficulty jumps a whole band
// (2+ steps) at once, while ours moves at most about 0.4 steps per answer.
// That per-answer step is criterion (a), and the pass-mark control below
// proves the criterion catches the behaviour the learner asked to avoid.

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"quizroom/internal/picker"
	"quizroom/internal/rating"
)

const (
	quizzes   = 50
	seedCount = 20
	// The topic card says "Few questions near your level" below this many, and
	// the simulated learner presses "Ask Claude for more" when it does.
	nearLevelPrompt = 20
)

type simQuestion struct {
	id        int64
	subtopic  int64
	trueD     float64 // true difficulty, which the app never sees
	b         float64 // the app's rating, started from Claude's noisy label
	m         int
	lastShown time.Time
}

type world struct {
	rng       *rand.Rand
	questions []*simQuestion
	nextID    int64
}

// add writes n questions with true difficulty uniform in [lo, hi]. Claude's
// label is off by up to 1.5 steps either way.
func (w *world) add(n int, lo, hi float64) {
	lo, hi = math.Max(1, lo), math.Min(10, hi)
	for i := 0; i < n; i++ {
		d := lo + w.rng.Float64()*(hi-lo)
		label := max(1, min(10, int(math.Round(d+(w.rng.Float64()*3-1.5)))))
		w.nextID++
		w.questions = append(w.questions, &simQuestion{
			id: w.nextID, subtopic: int64(1 + w.rng.IntN(9)), trueD: d, b: rating.BFromDifficulty(label),
		})
	}
}

func (w *world) eligible(now time.Time) []*simQuestion {
	var out []*simQuestion
	for _, q := range w.questions {
		if q.lastShown.IsZero() || !q.lastShown.After(now.Add(-picker.RepeatWindow)) {
			out = append(out, q)
		}
	}
	return out
}

func nearLevel(qs []*simQuestion, theta, target float64) int {
	n := 0
	for _, q := range qs {
		if math.Abs(rating.Predict(theta, q.b, 0.25)-target) <= picker.FitWindow {
			n++
		}
	}
	return n
}

// aim is the difficulty the app is aiming at: the unrounded displayed skill.
func aim(theta float64) float64 { return rating.DifficultyOf(theta - math.Ln2) }

func trueSkillAt(i int) float64 { return 1.5 + (8.0-1.5)*float64(i)/float64(quizzes-1) }

type runResult struct {
	maxAimStep float64 // from quiz 6 on: largest change in the aim caused by one answer
	maxMA3     float64 // largest change in the 3-quiz moving average of true difficulty shown
	share      float64 // share of right answers
	meanErr    float64 // from quiz 6 on: mean |displayed skill − true skill| at the end of each quiz
	meanD      []float64
	skills     []float64
}

func movingMax3(xs []float64, from int) float64 {
	m := 0.0
	for i := max(from, 3); i < len(xs); i++ {
		a := (xs[i] + xs[i-1] + xs[i-2]) / 3
		b := (xs[i-1] + xs[i-2] + xs[i-3]) / 3
		m = math.Max(m, math.Abs(a-b))
	}
	return m
}

func simulate(t *testing.T, seed uint64) runResult {
	const target = rating.DefaultTarget
	w := &world{rng: rand.New(rand.NewPCG(seed, seed*3+2))}
	w.add(30, 1, 6) // a fresh topic's first batch

	theta := rating.ThetaFromSkill(rating.StartSkill)
	n := 0
	start := time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)
	res := runResult{meanD: make([]float64, quizzes), skills: make([]float64, quizzes)}
	right, answered, errSum := 0, 0, 0.0

	for i := 0; i < quizzes; i++ {
		now := start.Add(time.Duration(i) * 12 * time.Hour)
		trueTheta := rating.ThetaFromSkill(trueSkillAt(i))

		for round := 0; round < 2 && nearLevel(w.eligible(now), theta, target) < nearLevelPrompt; round++ {
			s := rating.Skill(theta)
			w.add(15, s-1, s+2)
		}
		length, ok := picker.Length(len(w.eligible(now)))
		if !ok {
			s := rating.Skill(theta)
			w.add(15, s-1, s+2)
			if length, ok = picker.Length(len(w.eligible(now))); !ok {
				t.Fatalf("seed %d quiz %d: still only %d questions after a top-up", seed, i+1, length)
			}
		}

		inQuiz, counts := map[int64]bool{}, map[int64]int{}
		var prev int64
		sumD := 0.0
		for pos := 1; pos <= length; pos++ {
			var cands []picker.Candidate
			byID := map[int64]*simQuestion{}
			for _, q := range w.eligible(now) {
				if !inQuiz[q.id] {
					byID[q.id] = q
					cands = append(cands, picker.Candidate{QuestionID: q.id, SubtopicID: q.subtopic, B: q.b, C: 0.25, NeverSeen: q.lastShown.IsZero()})
				}
			}
			pick, ok := picker.Rating(cands, picker.Quiz{Theta: theta, Target: target, PrevSubtopic: prev, SubtopicCounts: counts}, w.rng)
			if !ok {
				t.Fatalf("seed %d quiz %d position %d: no candidates", seed, i+1, pos)
			}
			q := byID[pick.Candidate.QuestionID]
			isRight := w.rng.Float64() < rating.Predict(trueTheta, rating.DiffScale*(q.trueD-rating.MidDifficulty), 0.25)

			before := aim(theta)
			r := rating.Apply(theta, q.b, 0.25, n, q.m, isRight, !q.lastShown.IsZero())
			theta, q.b = r.Theta, r.B
			if i >= 5 {
				res.maxAimStep = math.Max(res.maxAimStep, math.Abs(aim(theta)-before))
			}
			n++
			q.m++
			q.lastShown = now
			inQuiz[q.id] = true
			counts[q.subtopic]++
			prev = q.subtopic
			sumD += q.trueD
			if isRight {
				right++
			}
		}
		answered += length
		res.meanD[i] = sumD / float64(length)
		res.skills[i] = rating.Skill(theta)
		if i >= 5 {
			errSum += math.Abs(rating.Skill(theta) - trueSkillAt(i))
		}
	}
	res.maxMA3 = movingMax3(res.meanD, 5)
	res.share = float64(right) / float64(answered)
	res.meanErr = errSum / float64(quizzes-5)
	return res
}

// simulatePassMark is the design the learner rejected: three fixed bands and
// promotion after scoring 9/10. It returns the largest one-answer change in
// the difficulty it aims at (the band's middle).
func simulatePassMark(seed uint64) float64 {
	rng := rand.New(rand.NewPCG(seed, seed*3+2))
	bands := [][2]float64{{1, 3.5}, {3.5, 6.5}, {6.5, 10}}
	band, maxStep := 0, 0.0
	for i := 0; i < quizzes; i++ {
		trueTheta := rating.ThetaFromSkill(trueSkillAt(i))
		right := 0
		for pos := 0; pos < 10; pos++ {
			lo, hi := bands[band][0], bands[band][1]
			d := lo + rng.Float64()*(hi-lo)
			if rng.Float64() < rating.Predict(trueTheta, rating.DiffScale*(d-rating.MidDifficulty), 0.25) {
				right++
			}
		}
		if right >= 9 && band < 2 {
			before := (bands[band][0] + bands[band][1]) / 2
			band++
			maxStep = math.Max(maxStep, math.Abs((bands[band][0]+bands[band][1])/2-before))
		}
	}
	return maxStep
}

func TestDifficultyFollowsAnImprovingLearner(t *testing.T) {
	shareSum, errSum, smooth := 0.0, 0.0, 0
	for seed := uint64(1); seed <= seedCount; seed++ {
		r := simulate(t, seed)
		// (a) Gradual: after the first five quizzes, no single answer moves the
		// aimed difficulty by more than 0.4 steps (K·1/0.6 at the floor ≈ 0.38).
		if r.maxAimStep > 0.4 {
			t.Errorf("seed %d: one answer moved the aimed difficulty %.2f steps (limit 0.4)", seed, r.maxAimStep)
		}
		if r.maxMA3 <= 1.25 {
			smooth++
		}
		shareSum += r.share
		errSum += r.meanErr
		if testing.Verbose() && seed == 1 {
			for i := range r.meanD {
				t.Logf("seed 1 quiz %2d: true skill %.2f, shown skill %.1f, mean true difficulty %.2f", i+1, trueSkillAt(i), r.skills[i], r.meanD[i])
			}
		}
	}
	// (b) The questions shown move smoothly: in at least 18 of 20 runs, the
	// three-quiz moving average of true difficulty never changes by more than
	// 1.25 steps from one quiz to the next. (Over 60 seeds, 59 stayed at or
	// under 1.06; Claude's ±1.5-step label noise causes the odd exception.)
	if smooth < seedCount-2 {
		t.Errorf("only %d of %d runs kept the 3-quiz moving average within 1.25 steps", smooth, seedCount)
	}
	// (c) Aimed at the target: across runs, the share of right answers is within
	// 6 points of it. A learner who improves every quiz stays ahead of the
	// rating: at the K floor the rating catches up by K·dP/dθ ≈ 0.033 of the
	// gap per answer while the true skill grows 0.13 steps per quiz, which
	// settles at a lag of about 0.4 steps and a share near 0.79 by itself.
	if share := shareSum / seedCount; math.Abs(share-rating.DefaultTarget) > 0.06 {
		t.Errorf("mean share of right answers %.3f is more than 6 points from %.2f", share, rating.DefaultTarget)
	}
	// (d) Follows the learner: from quiz 6 on, the displayed skill is within
	// 0.75 of the true skill on average.
	if e := errSum / seedCount; e > 0.75 {
		t.Errorf("mean |shown skill − true skill| is %.2f (limit 0.75)", e)
	}
	t.Logf("mean share %.3f, mean skill error %.2f, smooth runs %d/%d", shareSum/seedCount, errSum/seedCount, smooth, seedCount)
}

// The control proves criterion (a) catches a pass-mark design: its aimed
// difficulty jumps a whole band at once.
func TestPassMarkControlFailsCriterionA(t *testing.T) {
	for seed := uint64(1); seed <= seedCount; seed++ {
		if step := simulatePassMark(seed); step < 2.0 {
			t.Fatalf("seed %d: the pass-mark control's largest step was %.2f; criterion (a) would not catch it", seed, step)
		}
	}
}

// simulateMargin runs the same learner and world as simulate, also tracking
// the rating deviation, and counts answers (after the first 30) where the
// learner's true Elo lies within the shown margin.
func simulateMargin(seed uint64) (inside, total int) {
	const target = rating.DefaultTarget
	w := &world{rng: rand.New(rand.NewPCG(seed, seed*3+2))}
	w.add(30, 1, 6)
	theta, rd := rating.ThetaFromSkill(rating.StartSkill), rating.RD0
	n := 0
	start := time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)
	for i := 0; i < quizzes; i++ {
		now := start.Add(time.Duration(i) * 12 * time.Hour)
		if i > 0 {
			rd = rating.Widen(rd, 0.5)
		}
		trueTheta := rating.ThetaFromSkill(trueSkillAt(i))
		for round := 0; round < 2 && nearLevel(w.eligible(now), theta, target) < nearLevelPrompt; round++ {
			s := rating.Skill(theta)
			w.add(15, s-1, s+2)
		}
		length, ok := picker.Length(len(w.eligible(now)))
		if !ok {
			s := rating.Skill(theta)
			w.add(15, s-1, s+2)
			length, _ = picker.Length(len(w.eligible(now)))
		}
		inQuiz, counts := map[int64]bool{}, map[int64]int{}
		var prev int64
		for pos := 1; pos <= length; pos++ {
			var cands []picker.Candidate
			byID := map[int64]*simQuestion{}
			for _, q := range w.eligible(now) {
				if !inQuiz[q.id] {
					byID[q.id] = q
					cands = append(cands, picker.Candidate{QuestionID: q.id, SubtopicID: q.subtopic, B: q.b, C: 0.25, NeverSeen: q.lastShown.IsZero()})
				}
			}
			pick, ok := picker.Rating(cands, picker.Quiz{Theta: theta, Target: target, PrevSubtopic: prev, SubtopicCounts: counts}, w.rng)
			if !ok {
				return inside, total
			}
			q := byID[pick.Candidate.QuestionID]
			isRight := w.rng.Float64() < rating.Predict(trueTheta, rating.DiffScale*(q.trueD-rating.MidDifficulty), 0.25)
			repeat := !q.lastShown.IsZero()
			p := rating.Predict(theta, q.b, 0.25)
			r := rating.Apply(theta, q.b, 0.25, n, q.m, isRight, repeat)
			theta, q.b = r.Theta, r.B
			rd = rating.Narrow(rd, rating.Info(p, 0.25), repeat)
			if n >= 30 {
				total++
				if d := rating.Elo(theta) - rating.Elo(trueTheta); d >= -rating.Margin(rd) && d <= rating.Margin(rd) {
					inside++
				}
			}
			n++
			q.m++
			q.lastShown = now
			inQuiz[q.id] = true
			counts[q.subtopic]++
			prev = q.subtopic
		}
	}
	return inside, total
}

// The margin must be honest: the learner's true level lies inside it about
// 2 times in 3 (one standard deviation), not much more and not much less.
func TestMarginIsHonest(t *testing.T) {
	inside, total := 0, 0
	for seed := uint64(1); seed <= 20; seed++ {
		in, tot := simulateMargin(seed)
		inside, total = inside+in, total+tot
	}
	share := float64(inside) / float64(total)
	t.Logf("RDMin %.2f (±%d): true level inside the margin for %.3f of %d answers", rating.RDMin, rating.Margin(rating.RDMin), share, total)
	if share < 0.55 || share > 0.80 {
		t.Fatalf("margin coverage %.3f, want 0.55–0.80", share)
	}
}
