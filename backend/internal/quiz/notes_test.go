package quiz_test

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"quizroom/internal/format"
	"quizroom/internal/picker"
	"quizroom/internal/quiz"
	"quizroom/internal/store/storetest"
)

func TestResultNotesForAHit(t *testing.T) {
	svc, st, c := setup(t)
	ms, _ := st.Mistakes(ctx, topicID(t, st, "kubernetes"))
	seed := ms[0]
	tagged(t, st, 5, fmt.Sprintf("m%d", seed.ID))
	s, _ := svc.Start(ctx, "kubernetes")
	c.tick(time.Minute)
	if _, err := svc.Next(ctx, s.QuizID); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Answer(ctx, s.QuizID, 1, []int{1})
	if err != nil || len(res.MistakesHit) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	h := res.MistakesHit[0]
	if h.Belief != seed.Belief || h.Correction != seed.Correction || h.New || h.Hits != 1 || !h.DueAt.Equal(c.now()) {
		t.Fatalf("hit %+v, want the seed mistake with its correction, due now", h)
	}
	if len(res.MovedOn) != 0 {
		t.Fatalf("a hit moves nothing on: %+v", res.MovedOn)
	}
	again, err := svc.Answer(ctx, s.QuizID, 1, []int{0})
	if err != nil || len(again.MistakesHit) != 1 || again.MistakesHit[0] != h {
		t.Fatalf("a repeated submit returns the same notes: %+v %v", again, err)
	}
}

func TestResultNotesForAMoveOn(t *testing.T) {
	svc, st, c := setup(t)
	ms, _ := st.Mistakes(ctx, topicID(t, st, "kubernetes"))
	seed := ms[0]
	tagged(t, st, 1, fmt.Sprintf("m%d", seed.ID))
	storetest.AddQuestions(t, st, "kubernetes", "workloads", 9, 2)
	s, _ := svc.Start(ctx, "kubernetes")
	var moved []quiz.MistakeNote
	var at time.Time
	for pos := 1; pos <= s.Length; pos++ {
		c.tick(time.Minute)
		if _, err := svc.Next(ctx, s.QuizID); err != nil {
			t.Fatal(err)
		}
		res, err := svc.Answer(ctx, s.QuizID, pos, []int{0})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.MovedOn) > 0 {
			moved, at = res.MovedOn, c.now()
		}
	}
	if len(moved) != 1 {
		t.Fatalf("moved on %+v", moved)
	}
	m := moved[0]
	if m.Belief != seed.Belief || m.Correction != seed.Correction || m.NextCheck != 2 || m.Done || m.NextDue == nil || !m.NextDue.Equal(at.Add(24*time.Hour)) {
		t.Fatalf("moved on %+v, want check 2 of 5 due %v", m, at.Add(24*time.Hour))
	}
}

func TestResultNewMistake(t *testing.T) {
	svc, st, c := setup(t)
	belief := "The scheduler recreates deleted pods"
	tagged(t, st, 5, "n1", format.NewMistake{Key: "n1", Topic: "kubernetes", Subtopic: "workloads", Belief: belief, Correction: "The ReplicaSet controller does"})
	s, _ := svc.Start(ctx, "kubernetes")
	c.tick(time.Minute)
	if _, err := svc.Next(ctx, s.QuizID); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Answer(ctx, s.QuizID, 1, []int{1})
	if err != nil || len(res.MistakesHit) != 1 || !res.MistakesHit[0].New || res.MistakesHit[0].Correction != "The ReplicaSet controller does" {
		t.Fatalf("the first time a learner makes a Claude-written trap, it's new on their list: %+v %v", res, err)
	}
	for _, o := range res.Options {
		if o.Mistake != "" && !o.OnList {
			t.Fatalf("after the hit the mistake is on the list: %+v", o)
		}
	}
}

func TestOptionOnList(t *testing.T) {
	svc, st, c := setup(t)
	belief := "The scheduler recreates deleted pods"
	tagged(t, st, 5, "n1", format.NewMistake{Key: "n1", Topic: "kubernetes", Subtopic: "workloads", Belief: belief, Correction: "The ReplicaSet controller does"})
	s, _ := svc.Start(ctx, "kubernetes")
	c.tick(time.Minute)
	if _, err := svc.Next(ctx, s.QuizID); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Answer(ctx, s.QuizID, 1, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	traps := 0
	for _, o := range res.Options {
		if o.Mistake == belief {
			traps++
			if o.OnList {
				t.Fatalf("a trap the learner never fell for isn't on their list: %+v", o)
			}
		}
	}
	if traps != 1 {
		t.Fatalf("options %+v", res.Options)
	}
}

func TestResultPredictionAndCount(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 10, 2)
	s, _ := svc.Start(ctx, "docker")
	for pos := 1; pos <= 2; pos++ {
		c.tick(time.Minute)
		if _, err := svc.Next(ctx, s.QuizID); err != nil {
			t.Fatal(err)
		}
		res, err := svc.Answer(ctx, s.QuizID, pos, []int{0})
		if err != nil {
			t.Fatal(err)
		}
		answers, _ := st.QuizAnswers(ctx, s.QuizID)
		if !near(res.P, answers[pos-1].P) || res.AnswersInTopic != pos || res.LevelBefore != "Beginner" {
			t.Fatalf("position %d: %+v, want p %v", pos, res, answers[pos-1].P)
		}
	}
}

func TestSummaryReviewAndNotes(t *testing.T) {
	svc, st, c := setup(t)
	ms, _ := st.Mistakes(ctx, topicID(t, st, "kubernetes"))
	clear, hit := ms[0], ms[1]
	tagged(t, st, 1, fmt.Sprintf("m%d", clear.ID))
	tagged(t, st, 1, fmt.Sprintf("m%d", hit.ID))
	storetest.AddQuestions(t, st, "kubernetes", "workloads", 8, 2)
	s, _ := svc.Start(ctx, "kubernetes")
	if s.Length != 10 {
		t.Fatalf("length %d", s.Length)
	}
	for pos := 1; pos <= s.Length; pos++ {
		c.tick(time.Minute)
		sh, err := svc.Next(ctx, s.QuizID)
		if err != nil {
			t.Fatal(err)
		}
		choice := []int{0}
		if isTagged(t, st, sh.Question.ID, hit.ID) {
			choice = []int{1}
		}
		if _, err := svc.Answer(ctx, s.QuizID, pos, choice); err != nil {
			t.Fatal(err)
		}
	}
	sum, err := svc.Summary(ctx, s.QuizID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.CameBack) != 1 || sum.CameBack[0].Belief != hit.Belief || sum.CameBack[0].NextCheck != 1 {
		t.Fatalf("came back %+v", sum.CameBack)
	}
	if len(sum.MovedOn) != 1 || sum.MovedOn[0].Belief != clear.Belief || sum.MovedOn[0].NextCheck != 2 {
		t.Fatalf("moved on %+v", sum.MovedOn)
	}
	if sum.LevelBefore != "Beginner" || sum.BankState == "" {
		t.Fatalf("summary %+v", sum)
	}
	answers, _ := st.QuizAnswers(ctx, s.QuizID)
	if len(sum.Review) != 10 {
		t.Fatalf("review has %d items", len(sum.Review))
	}
	off := 0
	for i, a := range answers {
		r := sum.Review[i]
		var shown []int
		for _, o := range r.Options {
			shown = append(shown, o.ID)
		}
		if r.Position != a.Position || r.Correct != *a.Correct || !slices.Equal(shown, a.OptionOrder) || r.Stem == "" || r.Subtopic == "" {
			t.Fatalf("review item %+v for answer %+v", r, a)
		}
		if a.Slot == "rating" && math.Abs(a.P-0.75) > picker.FitWindow {
			off++
		}
	}
	if sum.OffLevel != off {
		t.Fatalf("off level %d, want %d", sum.OffLevel, off)
	}
}

func TestSummaryEarlyEnd(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 10, 2)
	s, _ := svc.Start(ctx, "docker")
	play(t, svc, c, s.QuizID, 3)
	sum, err := svc.Summary(ctx, s.QuizID)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Length != 10 || sum.Answered != 3 || len(sum.Review) != 3 || sum.CameBack == nil || sum.MovedOn == nil {
		t.Fatalf("summary %+v", sum)
	}
	empty, _ := svc.Start(ctx, "docker")
	if sum, err = svc.Summary(ctx, empty.QuizID); err != nil || sum.Answered != 0 || sum.Review == nil || len(sum.Review) != 0 {
		t.Fatalf("summary with no answers %+v %v", sum, err)
	}
}
