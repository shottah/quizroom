// Package rating holds the Elo-style maths behind Quizroom's difficulty curve:
// the prediction of a right answer, the rating updates after each answer, and
// the mapping between ratings (logits) and the 1–10 skill and difficulty scale.
// See the "Ratings" section of quizroom.md.
package rating

import "math"

const (
	// DiffScale is the number of logits per step on the 1–10 difficulty scale.
	DiffScale = 0.6
	// MidDifficulty is the middle of the 1–10 scale, which maps to b = 0.
	MidDifficulty = 5.5
	// StartSkill is where a learner starts in every topic: the low end of Beginner.
	StartSkill = 1.5
	// DefaultTarget is the predicted chance of success questions are aimed at.
	DefaultTarget = 0.75
)

// Predict returns the chance of a right answer for a learner rated theta on a
// question rated b, where c is the chance of guessing right.
func Predict(theta, b, c float64) float64 {
	return c + (1-c)/(1+math.Exp(-(theta-b)))
}

// GuessFloor is the chance of a lucky guess: 1/n for a single-answer question,
// 1/(2^n - 1) for a pick-all question with n options.
func GuessFloor(multi bool, nOptions int) float64 {
	if nOptions <= 0 {
		return 0
	}
	if multi {
		return 1 / (math.Pow(2, float64(nOptions)) - 1)
	}
	return 1 / float64(nOptions)
}

// KLearner is the learner's step size after n answers in a topic.
func KLearner(n int) float64 {
	return math.Max(0.2, 0.8/(1+0.05*float64(n)))
}

// KQuestion is a question's step size after m answers to it.
func KQuestion(m int) float64 {
	return math.Max(0.05, 0.3/(1+0.1*float64(m)))
}

// Result is the outcome of one answer: the prediction made before it, and the
// learner's and question's ratings after it.
type Result struct {
	P, Theta, B float64
}

// Apply updates both ratings after one answer. n is the learner's answer count
// in the topic and m the question's answer count, both before this answer.
// A repeated question moves the learner's rating at half weight.
func Apply(theta, b, c float64, n, m int, right, repeat bool) Result {
	p := Predict(theta, b, c)
	r := 0.0
	if right {
		r = 1
	}
	w := 1.0
	if repeat {
		w = 0.5
	}
	return Result{
		P:     p,
		Theta: theta + w*KLearner(n)*(r-p),
		B:     b - KQuestion(m)*(r-p),
	}
}

// BFromDifficulty is a question's starting rating from Claude's 1–10 difficulty.
func BFromDifficulty(d int) float64 {
	return DiffScale * (float64(d) - MidDifficulty)
}

// ThetaFromSkill converts a displayed skill back to a rating.
func ThetaFromSkill(skill float64) float64 {
	return DiffScale*(skill-MidDifficulty) + math.Ln2
}

// Skill is the displayed skill for a rating: the difficulty of a single-answer
// question at the learner's 75% point, clamped to 1–10 and rounded to 0.1.
func Skill(theta float64) float64 {
	s := MidDifficulty + (theta-math.Ln2)/DiffScale
	s = math.Min(10, math.Max(1, s))
	return math.Round(s*10) / 10
}

// Level is the label for a displayed skill. It never affects question choice.
func Level(skill float64) string {
	switch {
	case skill < 4.0:
		return "Beginner"
	case skill < 7.0:
		return "Intermediate"
	default:
		return "Advanced"
	}
}

// DifficultyOf maps a question rating back onto the 1–10 difficulty scale.
func DifficultyOf(b float64) float64 {
	return b/DiffScale + MidDifficulty
}

// Elo and its margin: the score each topic shows the learner.
// Elo is the 1–10 skill on a 0–300 scale: skill 4 is 100 and skill 7 is
// 200, so the levels fall on exact thirds. The margin comes from a
// Glicko-style rating deviation (RD, in θ units) that narrows with each
// answer's information and widens with time away. It never changes how far
// an answer moves the rating.
const (
	// EloPerTheta converts θ units to Elo points (about 55.6).
	EloPerTheta = 100.0 / 3 / DiffScale
	// RD0 is the deviation of a topic with no answers: a margin of ±100.
	RD0 = 1.8
	// RDGrowthPerDay is Glicko's c²: how much variance a day away adds.
	RDGrowthPerDay = 0.0175
	// RDMin is the smallest deviation the margin may claim. The rating's own
	// step size keeps it wobbling about the true level, so a smaller margin
	// would overstate the precision (set by TestMarginIsHonest).
	RDMin = 0.48
)

// rawSkill is Skill without rounding.
func rawSkill(theta float64) float64 {
	return math.Min(10, math.Max(1, MidDifficulty+(theta-math.Ln2)/DiffScale))
}

// Elo is the learner's rating on the 0–300 scale.
func Elo(theta float64) int {
	elo := math.Round((rawSkill(theta) - 1) * 100 / 3)
	return int(math.Min(300, math.Max(0, elo)))
}

// LevelOfElo names the level of a shown Elo, so the badge always agrees
// with the number.
func LevelOfElo(elo int) string {
	switch {
	case elo < 100:
		return "Beginner"
	case elo < 200:
		return "Intermediate"
	default:
		return "Advanced"
	}
}

// Margin is a deviation as ± Elo points: one standard deviation.
func Margin(rd float64) int {
	return int(math.Round(rd * EloPerTheta))
}

// Info is the Fisher information of one answer under the 3PL model with
// a = 1: how much it tells about θ when the predicted chance was p and the
// guessing floor c.
func Info(p, c float64) float64 {
	if p <= c || p >= 1 {
		return 0
	}
	x := (p - c) / (1 - c)
	return x * x * (1 - p) / p
}

// Widen grows a deviation for the days since it was last updated, up to RD0.
func Widen(rd, days float64) float64 {
	days = math.Max(0, days)
	return math.Min(RD0, math.Sqrt(rd*rd+RDGrowthPerDay*days))
}

// Narrow shrinks a deviation by one answer's information (half for a repeat
// question), down to RDMin.
func Narrow(rd, info float64, repeat bool) float64 {
	if repeat {
		info *= 0.5
	}
	return math.Max(RDMin, 1/math.Sqrt(1/(rd*rd)+info))
}
