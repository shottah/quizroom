package quiz_test

import (
	"math"
	"testing"
	"time"

	"quizroom/internal/rating"
	"quizroom/internal/store"
	"quizroom/internal/store/storetest"
)

func TestAnswerStoresRD(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 10, 2)
	s, _ := svc.Start(ctx, "docker")
	play(t, svc, c, s.QuizID, 1)
	answers, _ := st.QuizAnswers(ctx, s.QuizID)
	a := answers[0]
	r, _ := st.Rating(ctx, topicID(t, st, "docker"))
	want := rating.Narrow(rating.RD0, rating.Info(a.P, 0.25), false)
	if r.RD == nil || math.Abs(*r.RD-want) > 1e-9 || r.RDAt == nil || !r.RDAt.Equal(*a.AnsweredAt) {
		t.Fatalf("rating RD %v at %v, want %v at %v", r.RD, r.RDAt, want, a.AnsweredAt)
	}
	if a.RDBefore == nil || *a.RDBefore != rating.RD0 || a.RDAfter == nil || math.Abs(*a.RDAfter-want) > 1e-9 {
		t.Fatalf("answer RD %v → %v", a.RDBefore, a.RDAfter)
	}
}

func TestRDWidensBetweenAnswers(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 10, 2)
	s, _ := svc.Start(ctx, "docker")
	play(t, svc, c, s.QuizID, 1)
	c.tick(10 * 24 * time.Hour)
	play(t, svc, c, s.QuizID, 1)
	answers, _ := st.QuizAnswers(ctx, s.QuizID)
	gap := answers[1].AnsweredAt.Sub(*answers[0].AnsweredAt).Hours() / 24
	want := rating.Widen(*answers[0].RDAfter, gap)
	if math.Abs(*answers[1].RDBefore-want) > 1e-9 {
		t.Fatalf("second answer starts at RD %v, want %v (widened over %.2f days)", *answers[1].RDBefore, want, gap)
	}
}

func TestBackfillRDIsIdempotent(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 10, 2)
	s, _ := svc.Start(ctx, "docker")
	for i := 0; i < 3; i++ {
		play(t, svc, c, s.QuizID, 1)
		c.tick(30 * time.Hour)
	}
	docker := topicID(t, st, "docker")
	live, _ := st.Rating(ctx, docker)
	if _, err := st.Rating(ctx, topicID(t, st, "kubernetes")); err != nil { // creates its row, with no deviation
		t.Fatal(err)
	}
	if err := st.SetRD(ctx, docker, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.BackfillRD(ctx); err != nil {
		t.Fatal(err)
	}
	filled, _ := st.Rating(ctx, docker)
	if filled.RD == nil || math.Abs(*filled.RD-*live.RD) > 1e-9 || !filled.RDAt.Equal(*live.RDAt) {
		t.Fatalf("backfill %v at %v, live %v at %v", filled.RD, filled.RDAt, *live.RD, live.RDAt)
	}
	if err := svc.BackfillRD(ctx); err != nil {
		t.Fatal(err)
	}
	again, _ := st.Rating(ctx, docker)
	if *again.RD != *filled.RD || !again.RDAt.Equal(*filled.RDAt) {
		t.Fatal("a second backfill must change nothing")
	}
	k8s, _ := st.Rating(ctx, topicID(t, st, "kubernetes"))
	if k8s.RD == nil || *k8s.RD != rating.RD0 || k8s.RDAt != nil {
		t.Fatalf("a topic with no answers backfills to RD0: %+v", k8s)
	}
}

func TestMigrationDefaults(t *testing.T) {
	svc, st, c := setup(t)
	docker := topicID(t, st, "docker")
	id, err := st.CreateQuiz(ctx, store.Quiz{TopicID: docker, Length: 5, Target: 0.75, StartedAt: c.now(), SkillBefore: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	old, _ := st.Quiz(ctx, id)
	if old.Kind != "practice" || old.ThetaBefore != nil {
		t.Fatalf("a quiz made without the new fields: %+v", old)
	}
	storetest.AddQuestions(t, st, "docker", "compose", 10, 2)
	s, _ := svc.Start(ctx, "docker")
	q, _ := st.Quiz(ctx, s.QuizID)
	if q.ThetaBefore == nil || *q.ThetaBefore != rating.ThetaFromSkill(rating.StartSkill) {
		t.Fatalf("Start stores the rating it started from: %+v", q)
	}
}

func TestResultElo(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 10, 2)
	s, _ := svc.Start(ctx, "docker")
	if _, err := svc.Next(ctx, s.QuizID); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Answer(ctx, s.QuizID, 1, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	answers, _ := st.QuizAnswers(ctx, s.QuizID)
	a := answers[0]
	if res.EloBefore != rating.Elo(*a.ThetaBefore) || res.EloAfter != rating.Elo(*a.ThetaAfter) ||
		res.MarginAfter != rating.Margin(*a.RDAfter) || res.Level != rating.LevelOfElo(res.EloAfter) {
		t.Fatalf("result %+v for answer %+v", res, a)
	}
	sum, _ := svc.Summary(ctx, s.QuizID)
	if sum.EloBefore != 17 || sum.EloAfter != res.EloAfter || sum.MarginAfter != res.MarginAfter || sum.Kind != "practice" {
		t.Fatalf("summary %+v", sum)
	}
	_ = c
}
