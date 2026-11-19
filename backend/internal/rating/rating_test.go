package rating

import (
	"math"
	"testing"
)

func near(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %.12f, want %.12f", got, want)
	}
}

func TestPredictAtOwnLevelIs75(t *testing.T) {
	for _, s := range []float64{1.5, 4.3, 5.5, 9} {
		theta, b := ThetaFromSkill(s), DiffScale*(s-MidDifficulty)
		near(t, Predict(theta, b, 0.25), 0.75)
	}
}

func TestGuessFloor(t *testing.T) {
	near(t, GuessFloor(false, 4), 0.25)
	near(t, GuessFloor(true, 4), 1.0/15)
	near(t, GuessFloor(true, 5), 1.0/31)
}

func TestKSchedules(t *testing.T) {
	near(t, KLearner(0), 0.8)
	near(t, KLearner(20), 0.4)
	near(t, KLearner(60), 0.2)
	near(t, KLearner(500), 0.2)
	near(t, KQuestion(0), 0.3)
	near(t, KQuestion(10), 0.15)
	near(t, KQuestion(100), 0.05)
}

func TestApplyWorkedExamples(t *testing.T) {
	r := Apply(0, 0, 0.25, 0, 0, true, false)
	near(t, r.P, 0.625)
	near(t, r.Theta, 0.3)
	near(t, r.B, -0.1125)

	r = Apply(0, 0, 0.25, 0, 0, true, true)
	near(t, r.Theta, 0.15)
	near(t, r.B, -0.1125)

	r = Apply(0, 0, 0.25, 0, 0, false, false)
	near(t, r.Theta, -0.5)
	near(t, r.B, 0.1875)
}

func TestSkillAndLevel(t *testing.T) {
	near(t, Skill(ThetaFromSkill(4.3)), 4.3)
	near(t, Skill(-100), 1)
	near(t, Skill(100), 10)
	near(t, Skill(ThetaFromSkill(StartSkill)), 1.5)

	for skill, want := range map[float64]string{3.9: "Beginner", 4.0: "Intermediate", 6.9: "Intermediate", 7.0: "Advanced"} {
		if got := Level(skill); got != want {
			t.Fatalf("Level(%v) = %q, want %q", skill, got, want)
		}
	}

	near(t, BFromDifficulty(1), -2.7)
	near(t, BFromDifficulty(10), 2.7)
	near(t, DifficultyOf(BFromDifficulty(7)), 7)
}

func TestEloBoundaries(t *testing.T) {
	cases := []struct {
		skill float64
		want  int
	}{{4, 100}, {7, 200}, {1, 0}, {10, 300}, {3.995, 100}, {StartSkill, 17}}
	for _, c := range cases {
		if got := Elo(ThetaFromSkill(c.skill)); got != c.want {
			t.Errorf("Elo(skill %v) = %d, want %d", c.skill, got, c.want)
		}
	}
	if got := Elo(ThetaFromSkill(12)); got != 300 {
		t.Errorf("beyond skill 10: %d, want 300", got)
	}
	if got := Elo(ThetaFromSkill(-3)); got != 0 {
		t.Errorf("below skill 1: %d, want 0", got)
	}
	for elo, want := range map[int]string{0: "Beginner", 99: "Beginner", 100: "Intermediate", 199: "Intermediate", 200: "Advanced", 300: "Advanced"} {
		if got := LevelOfElo(elo); got != want {
			t.Errorf("LevelOfElo(%d) = %q, want %q", elo, got, want)
		}
	}
}

func TestInfoPeaksNear68(t *testing.T) {
	best, bestP := 0.0, 0.0
	for p := 0.30; p <= 0.99; p += 0.001 {
		if i := Info(p, 0.25); i > best {
			best, bestP = i, p
		}
	}
	if math.Abs(bestP-0.68) > 0.02 {
		t.Fatalf("information peaks at p=%.3f, want about 0.68", bestP)
	}
	if Info(0.25, 0.25) != 0 || Info(1, 0.25) != 0 || Info(0.1, 0.25) != 0 {
		t.Fatal("no information at or below the guessing floor, or at certainty")
	}
}

func TestWidenCapsAndNarrowFloors(t *testing.T) {
	if Widen(0.3, 200) != RD0 {
		t.Fatalf("a long break caps at RD0: %v", Widen(0.3, 200))
	}
	near(t, Widen(0.3, 1), math.Sqrt(0.09+RDGrowthPerDay))
	if Widen(0.3, -2) != 0.3 {
		t.Fatal("negative days count as none")
	}
	full := Narrow(RD0, Info(0.68, 0.25), false)
	half := Narrow(RD0, Info(0.68, 0.25), true)
	if !(full < half && half < RD0) {
		t.Fatalf("narrowing: full %v, repeat %v, start %v", full, half, RD0)
	}
	rd := RD0
	for i := 0; i < 1000; i++ {
		rd = Narrow(rd, Info(0.68, 0.25), false)
	}
	if rd != RDMin {
		t.Fatalf("narrowing stops at the floor: %v, want %v", rd, RDMin)
	}
	if Margin(RD0) != 100 {
		t.Fatalf("Margin(RD0) = %d, want 100", Margin(RD0))
	}
}
