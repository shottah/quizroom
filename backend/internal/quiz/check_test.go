package quiz_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"quizroom/internal/format"
	"quizroom/internal/quiz"
	"quizroom/internal/rating"
	"quizroom/internal/store"
	"quizroom/internal/store/storetest"
)

// runCheck answers a check to the end, with option 0 unless wrong(position).
func runCheck(t *testing.T, svc *quiz.Service, c *clock, quizID int64, wrong func(int) bool) int {
	t.Helper()
	answered := 0
	for {
		next, err := svc.Next(ctx, quizID)
		if err != nil {
			t.Fatal(err)
		}
		if next.Done {
			return answered
		}
		choice := []int{0}
		if wrong != nil && wrong(next.Position) {
			choice = []int{1}
		}
		if _, err := svc.Answer(ctx, quizID, next.Position, choice); err != nil {
			t.Fatal(err)
		}
		answered++
		c.tick(time.Minute)
	}
}

func TestCheckRefusedBelow20(t *testing.T) {
	svc, st, _ := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 19, 2)
	_, err := svc.StartCheck(ctx, "docker")
	var ne quiz.NotEnoughError
	if !errors.As(err, &ne) || ne.Available != 19 || ne.Need != 20 || ne.Kind != quiz.Check {
		t.Fatalf("err %v", err)
	}
	if ne.Error() != "No skill check right now: it needs 20 questions you haven't seen in the last 7 days, and this topic has 19. Write 15 at your level first." {
		t.Fatalf("message %q", ne.Error())
	}
}

func TestCheckKeepsPracticeReserve(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 24, 2)
	s, err := svc.StartCheck(ctx, "docker")
	if err != nil || s.Length != 14 || s.Kind != quiz.Check {
		t.Fatalf("%+v %v", s, err)
	}
	if n := runCheck(t, svc, c, s.QuizID, nil); n > 14 {
		t.Fatalf("answered %d", n)
	}
	el, _ := st.Eligible(ctx, topicID(t, st, "docker"), 0, c.now())
	if len(el) < 10 {
		t.Fatalf("only %d left for practice", len(el))
	}
}

func TestCheckStopsAtMargin(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 45, 2)
	docker := topicID(t, st, "docker")
	if _, err := st.Rating(ctx, docker); err != nil {
		t.Fatal(err)
	}
	rd, at := 0.3, c.now()
	if err := st.SetRD(ctx, docker, &rd, &at); err != nil {
		t.Fatal(err)
	}
	s, _ := svc.StartCheck(ctx, "docker")
	if n := runCheck(t, svc, c, s.QuizID, nil); n != 10 {
		t.Fatalf("a well-measured topic stops after 10, answered %d", n)
	}
	sum, _ := svc.Summary(ctx, s.QuizID)
	answers, _ := st.QuizAnswers(ctx, s.QuizID)
	last := answers[len(answers)-1]
	if sum.Check == nil || sum.Check.Elo != rating.Elo(*last.ThetaAfter) || sum.Check.Margin != rating.Margin(*last.RDAfter) || sum.Check.Previous != nil {
		t.Fatalf("check result %+v", sum.Check)
	}
}

func TestCheckRunsToCapFromNew(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 45, 2)
	s, _ := svc.StartCheck(ctx, "docker")
	if n := runCheck(t, svc, c, s.QuizID, nil); n != 20 {
		t.Fatalf("a new topic's check runs to the cap of 20, answered %d", n)
	}
}

// addMultiK adds n pick-all docker questions in the images subtopic.
func addMultiK(t *testing.T, st *store.Store, n int) {
	t.Helper()
	var qs []format.Question
	for i := 0; i < n; i++ {
		qs = append(qs, format.Question{Topic: "docker", Subtopic: "images", Kind: format.Multi, Difficulty: 2,
			Stem: fmt.Sprintf("Which of these are true of image layers? (%d)", i),
			Options: []format.Option{
				{Text: "a", Correct: true, Explanation: "e"}, {Text: "b", Correct: true, Explanation: "e"},
				{Text: "c", Explanation: "e"}, {Text: "d", Explanation: "e"}, {Text: "e", Explanation: "e"},
			}})
	}
	if added, _, err := st.ApplyReply(ctx, 0, format.Clean{Questions: qs}, time.Now()); err != nil || len(added) != n {
		t.Fatal(err)
	}
}

func TestCheckHasNoMistakeSlotsAndCapsMulti(t *testing.T) {
	svc, st, c := setup(t)
	addMultiK(t, st, 30)
	storetest.AddQuestions(t, st, "docker", "compose", 20, 2)
	s, _ := svc.StartCheck(ctx, "docker")
	runCheck(t, svc, c, s.QuizID, nil)
	answers, _ := st.QuizAnswers(ctx, s.QuizID)
	multi := 0
	for _, a := range answers {
		if a.Slot == "mistake" {
			t.Fatalf("a check has no mistake slots: %+v", a)
		}
		if q, _ := st.Question(ctx, a.QuestionID); q.Kind == "multi" {
			multi++
		}
	}
	if multi > 5 {
		t.Fatalf("%d of %d answers were pick-all", multi, len(answers))
	}
}

func TestCheckResultPreviousAndMissed(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 50, 2)
	first, _ := svc.StartCheck(ctx, "docker")
	runCheck(t, svc, c, first.QuizID, nil)
	firstSum, _ := svc.Summary(ctx, first.QuizID)
	c.tick(time.Hour)
	second, err := svc.StartCheck(ctx, "docker")
	if err != nil {
		t.Fatal(err)
	}
	runCheck(t, svc, c, second.QuizID, func(pos int) bool { return pos%2 == 0 })
	sum, _ := svc.Summary(ctx, second.QuizID)
	if sum.Check == nil || sum.Check.Previous == nil || sum.Check.Previous.Elo != firstSum.Check.Elo || sum.Check.Previous.Margin != firstSum.Check.Margin {
		t.Fatalf("second check %+v, first %+v", sum.Check, firstSum.Check)
	}
	if len(sum.Check.MissedIn) != 1 || sum.Check.MissedIn[0].Count == 0 || sum.Check.MissedIn[0].Subtopic == "" {
		t.Fatalf("missed in %+v", sum.Check.MissedIn)
	}
	d, _ := svc.Detail(ctx, "docker")
	if len(d.Checks) != 2 || d.Checks[0].QuizID != second.QuizID || d.Checks[0].Elo != sum.Check.Elo || d.Checks[1].Margin != firstSum.Check.Margin {
		t.Fatalf("checks %+v", d.Checks)
	}
}

func TestNextShowsKind(t *testing.T) {
	svc, st, _ := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 45, 2)
	check, _ := svc.StartCheck(ctx, "docker")
	if sh, _ := svc.Next(ctx, check.QuizID); sh.Kind != quiz.Check {
		t.Fatalf("check question %+v", sh)
	}
	practice, _ := svc.Start(ctx, "docker")
	if sh, _ := svc.Next(ctx, practice.QuizID); sh.Kind != quiz.Practice {
		t.Fatalf("practice question %+v", sh)
	}
}

// Verification: a check picks questions to measure, not to fit the target,
// so none of its answers count as "outside your usual range".
func TestCheckSummaryHasNoOffLevel(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 45, 8)
	s, _ := svc.StartCheck(ctx, "docker")
	runCheck(t, svc, c, s.QuizID, nil)
	if sum, _ := svc.Summary(ctx, s.QuizID); sum.OffLevel != 0 {
		t.Fatalf("a check reports %d off-level answers", sum.OffLevel)
	}
}

// Final review I-1: the practice reserve holds even when practice uses the
// bank between a check's answers.
func TestCheckReserveSurvivesPracticeInBetween(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 24, 2)
	check, _ := svc.StartCheck(ctx, "docker")
	for i := 0; i < 4; i++ {
		next, _ := svc.Next(ctx, check.QuizID)
		if _, err := svc.Answer(ctx, check.QuizID, next.Position, []int{0}); err != nil {
			t.Fatal(err)
		}
	}
	practice, err := svc.Start(ctx, "docker")
	if err != nil {
		t.Fatal(err)
	}
	play(t, svc, c, practice.QuizID, practice.Length)
	if next, _ := svc.Next(ctx, check.QuizID); !next.Done {
		t.Fatalf("the check must stop rather than use the practice reserve: %+v", next)
	}
	if el, _ := st.Eligible(ctx, topicID(t, st, "docker"), 0, c.now()); len(el) < 10 {
		t.Fatalf("only %d left for practice", len(el))
	}
}

// Final review I-2: at most a quarter pick-all at any stopping point.
func TestCheckPickAllQuotaHoldsWhenStoppingEarly(t *testing.T) {
	svc, st, c := setup(t)
	addMultiK(t, st, 30)
	storetest.AddQuestions(t, st, "docker", "compose", 20, 2)
	docker := topicID(t, st, "docker")
	if _, err := st.Rating(ctx, docker); err != nil {
		t.Fatal(err)
	}
	rd, at := rating.RDMin, c.now()
	_ = st.SetRD(ctx, docker, &rd, &at)
	s, _ := svc.StartCheck(ctx, "docker")
	if n := runCheck(t, svc, c, s.QuizID, nil); n != 10 {
		t.Fatalf("answered %d, want 10", n)
	}
	answers, _ := st.QuizAnswers(ctx, s.QuizID)
	multi := 0
	for _, a := range answers {
		if q, _ := st.Question(ctx, a.QuestionID); q.Kind == "multi" {
			multi++
		}
	}
	if multi > 3 {
		t.Fatalf("%d of 10 answers were pick-all, want at most 3", multi)
	}
}

// Final review I-3: check questions are chosen to measure, so none is
// flagged as outside the usual range.
func TestCheckQuestionsAreNeverThin(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 45, 8)
	s, _ := svc.StartCheck(ctx, "docker")
	for {
		next, err := svc.Next(ctx, s.QuizID)
		if err != nil {
			t.Fatal(err)
		}
		if next.Done {
			return
		}
		if next.Thin {
			t.Fatalf("check question %d is marked off-level", next.Position)
		}
		_, _ = svc.Answer(ctx, s.QuizID, next.Position, []int{0})
		c.tick(time.Minute)
	}
}

// Final review I-4: the review carries each question's ID, so a wrong key
// seen in a check's review can be removed.
func TestReviewHasQuestionIDs(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 45, 2)
	s, _ := svc.StartCheck(ctx, "docker")
	runCheck(t, svc, c, s.QuizID, nil)
	sum, _ := svc.Summary(ctx, s.QuizID)
	answers, _ := st.QuizAnswers(ctx, s.QuizID)
	for i, r := range sum.Review {
		if r.QuestionID != answers[i].QuestionID {
			t.Fatalf("review %d has question %d, want %d", i, r.QuestionID, answers[i].QuestionID)
		}
	}
}
