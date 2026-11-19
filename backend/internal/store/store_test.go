package store_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"quizroom/internal/format"
	"quizroom/internal/mistakes"
	"quizroom/internal/rating"
	"quizroom/internal/store"
	"quizroom/internal/store/storetest"
)

var ctx = context.Background()

var now = time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)

func topic(t *testing.T, st *store.Store, slug string) store.Topic {
	t.Helper()
	tp, err := st.TopicBySlug(ctx, slug)
	if err != nil {
		t.Fatal(err)
	}
	return tp
}

func TestOpenMigratesAndSeeds(t *testing.T) {
	st, url := storetest.Open(t)
	tops, err := st.Topics(ctx)
	if err != nil || len(tops) != 2 {
		t.Fatalf("topics %v %v", tops, err)
	}
	for slug, want := range map[string]int{"docker": 9, "kubernetes": 10} {
		subs, _ := st.Subtopics(ctx, topic(t, st, slug).ID)
		if len(subs) != want {
			t.Fatalf("%s has %d subtopics, want %d", slug, len(subs), want)
		}
	}
	// A fresh install tracks only the mistakes its own learner makes.
	if ms, _ := st.Mistakes(ctx, 0); len(ms) != 0 {
		t.Fatalf("a fresh install starts with %d mistakes, want none", len(ms))
	}
	tp := topic(t, st, "kubernetes")
	if len(tp.ConfusionGroups) == 0 {
		t.Fatal("confusion groups were not seeded")
	}

	again, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	tops2, _ := again.Topics(ctx)
	subs2, _ := again.Subtopics(ctx, topic(t, again, "docker").ID)
	if len(tops2) != 2 || len(subs2) != 9 {
		t.Fatalf("reopening duplicated seed data: %d topics, %d Docker subtopics", len(tops2), len(subs2))
	}
}

func show(t *testing.T, st *store.Store, quizID, questionID int64, pos int, at time.Time) store.Answer {
	t.Helper()
	a, err := st.InsertShown(ctx, store.Answer{QuizID: quizID, Position: pos, QuestionID: questionID, Slot: "rating", OptionOrder: []int{0, 1, 2, 3}, ShownAt: at, P: 0.75})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func newQuiz(t *testing.T, st *store.Store, topicID int64, at time.Time) int64 {
	t.Helper()
	id, err := st.CreateQuiz(ctx, store.Quiz{TopicID: topicID, Length: 10, Target: 0.75, StartedAt: at, SkillBefore: 1.5, MistakeSlots: []int{}})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestEligible(t *testing.T) {
	st := storetest.New(t)
	docker := topic(t, st, "docker")
	qs := storetest.AddQuestions(t, st, "docker", "compose", 5, 4)
	old := newQuiz(t, st, docker.ID, now.Add(-8*24*time.Hour))
	recent := newQuiz(t, st, docker.ID, now.Add(-3*24*time.Hour))
	current := newQuiz(t, st, docker.ID, now)
	show(t, st, recent, qs[0].ID, 1, now.Add(-3*24*time.Hour)) // shown 3 days ago: excluded
	show(t, st, old, qs[1].ID, 1, now.Add(-8*24*time.Hour))    // shown 8 days ago: included, seen
	if err := st.FlagQuestion(ctx, qs[2].ID); err != nil {     // flagged: excluded
		t.Fatal(err)
	}
	show(t, st, current, qs[4].ID, 1, now) // in this quiz: excluded

	el, err := st.Eligible(ctx, docker.ID, current, now)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, e := range el {
		got[e.ID] = e.NeverSeen
	}
	want := map[int64]bool{qs[1].ID: false, qs[3].ID: true}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("eligible (id → never seen) = %v, want %v", got, want)
	}
}

func TestApplyAnswerOnce(t *testing.T) {
	st := storetest.New(t)
	docker := topic(t, st, "docker")
	qs := storetest.AddQuestions(t, st, "docker", "compose", 1, 4)
	quizID := newQuiz(t, st, docker.ID, now)
	show(t, st, quizID, qs[0].ID, 1, now)

	calls := 0
	compute := func(s store.AnswerState) (store.AnswerUpdate, error) {
		calls++
		return store.AnswerUpdate{Chosen: []int{0}, Correct: true, ThetaAfter: s.Rating.Theta + 0.3, BAfter: s.Question.B - 0.1}, nil
	}
	after, already, err := st.ApplyAnswer(ctx, quizID, 1, now, compute)
	if err != nil || already || after.Answer.AnsweredAt == nil || after.Answer.Correct == nil || !*after.Answer.Correct {
		t.Fatalf("first answer: %+v already=%v err=%v", after.Answer, already, err)
	}
	start := rating.ThetaFromSkill(rating.StartSkill)
	if after.Rating.N != 1 || !near(after.Rating.Theta, start+0.3) || after.Question.N != 1 {
		t.Fatalf("after: rating %+v question n %d", after.Rating, after.Question.N)
	}
	again, already, err := st.ApplyAnswer(ctx, quizID, 1, now, compute)
	if err != nil || !already || calls != 1 || again.Rating.N != 1 {
		t.Fatalf("second answer must not change anything: already=%v calls=%d rating=%+v err=%v", already, calls, again.Rating, err)
	}
	if _, _, err := st.ApplyAnswer(ctx, quizID, 2, now, compute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown position: %v", err)
	}
}

func near(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

func seedMistake(t *testing.T, st *store.Store, topicSlug string) store.Mistake {
	t.Helper()
	tp := topic(t, st, topicSlug)
	ms, _ := st.Mistakes(ctx, tp.ID)
	if len(ms) == 0 {
		t.Fatalf("no seed mistakes in %s", topicSlug)
	}
	return ms[0]
}

func question(stem string, mistakes ...string) format.Question {
	opts := []format.Option{
		{Text: "right", Correct: true, Explanation: "e"},
		{Text: "wrong 1", Explanation: "e"}, {Text: "wrong 2", Explanation: "e"}, {Text: "wrong 3", Explanation: "e"},
	}
	for i, m := range mistakes {
		opts[i+1].Mistake = m
	}
	return format.Question{Topic: "kubernetes", Subtopic: "workloads", Kind: format.Single, Difficulty: 3, Stem: stem, Options: opts}
}

func TestApplyReplyMapsKeys(t *testing.T) {
	st := storetest.New(t)
	seed := seedMistake(t, st, "kubernetes")
	storetest.AddQuestions(t, st, "kubernetes", "workloads", 1, 3)
	existing, _ := st.RecentStems(ctx, topic(t, st, "kubernetes").ID, 1)

	c := format.Clean{
		Mistakes: []format.NewMistake{
			{Key: "n1", Topic: "kubernetes", Subtopic: "workloads", Belief: "A Deployment schedules its pods", Correction: "The scheduler does"},
			{Key: "n2", Topic: "kubernetes", Subtopic: "workloads", Belief: "I mix up ReplicaSets and Deployments", Correction: "A Deployment manages ReplicaSets", FromUser: true},
		},
		Questions: []format.Question{
			question("What recreates a deleted pod?", "n1", fmt.Sprintf("m%d", seed.ID)),
			question(existing[0]),
		},
	}
	added, dropped, err := st.ApplyReply(ctx, 0, c, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || len(dropped) != 1 || dropped[0].Reason != format.ReasonDuplicate {
		t.Fatalf("added %d, dropped %+v", len(added), dropped)
	}
	ms, _ := st.Mistakes(ctx, 0)
	byBelief := map[string]store.Mistake{}
	for _, m := range ms {
		byBelief[m.Belief] = m
	}
	n1, n2 := byBelief["A Deployment schedules its pods"], byBelief["I mix up ReplicaSets and Deployments"]
	if n1.Status != "watching" || n1.Source != "quiz" || n2.Status != "active" || n2.Source != "chat" {
		t.Fatalf("n1 %+v n2 %+v", n1, n2)
	}
	q := added[0]
	if q.Options[1].MistakeID != n1.ID || q.Options[2].MistakeID != seed.ID || q.Options[0].MistakeID != 0 {
		t.Fatalf("options %+v", q.Options)
	}
	if len(q.MistakeIDs) != 2 || !near(q.B, rating.BFromDifficulty(3)) {
		t.Fatalf("question %+v", q)
	}
}

func TestFlagQuestionUndoesHits(t *testing.T) {
	st := storetest.New(t)
	k8s := topic(t, st, "kubernetes")
	seed := seedMistake(t, st, "kubernetes")
	added, _, err := st.ApplyReply(ctx, 0, format.Clean{Questions: []format.Question{question("Which component brings the pod back?", fmt.Sprintf("m%d", seed.ID))}}, now)
	if err != nil || len(added) != 1 {
		t.Fatal(err)
	}
	quizID := newQuiz(t, st, k8s.ID, now)
	show(t, st, quizID, added[0].ID, 1, now)
	hitAt := now.Add(time.Minute)
	_, _, err = st.ApplyAnswer(ctx, quizID, 1, hitAt, func(s store.AnswerState) (store.AnswerUpdate, error) {
		return store.AnswerUpdate{Chosen: []int{1}, Correct: false, ThetaAfter: s.Rating.Theta - 0.5, BAfter: s.Question.B,
			Mistakes: []store.MistakeChange{{ID: seed.ID, State: mistakes.Hit(hitAt), Event: mistakes.HitEvent}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := st.Mistake(ctx, seed.ID)
	if m.Hits != 1 || m.DueAt == nil || !m.DueAt.Equal(hitAt) {
		t.Fatalf("after the hit: %+v", m)
	}
	if err := st.FlagQuestion(ctx, added[0].ID); err != nil {
		t.Fatal(err)
	}
	m, _ = st.Mistake(ctx, seed.ID)
	if m.Hits != 0 || m.Status != "active" || m.Stage != 0 || m.DueAt == nil || !m.DueAt.Equal(seed.CreatedAt) {
		t.Fatalf("after the flag the mistake should be back to its initial state: %+v (created %v)", m, seed.CreatedAt)
	}
	el, _ := st.Eligible(ctx, k8s.ID, 0, now.Add(30*24*time.Hour))
	for _, e := range el {
		if e.ID == added[0].ID {
			t.Fatal("a flagged question must not be eligible")
		}
	}
}

func TestJobsLifecycle(t *testing.T) {
	st := storetest.New(t)
	id, err := st.CreateChatJob(ctx, "write questions", "message", store.JobRequest{Text: "write questions", Page: store.PageContext{Page: "home"}})
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := st.PendingJob(ctx); p == nil || *p != id {
		t.Fatalf("pending %v", p)
	}
	j, err := st.ClaimJob(ctx)
	if err != nil || j == nil || j.ID != id || j.Status != "running" || j.Request.Text != "write questions" {
		t.Fatalf("claim %+v %v", j, err)
	}
	if j2, _ := st.ClaimJob(ctx); j2 != nil {
		t.Fatal("a running job must not be claimed twice")
	}
	if err := st.FinishJob(ctx, id, store.JobResult{Added: 3, Reply: "done"}, "Here you go."); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.PendingJob(ctx); p != nil {
		t.Fatal("no job should be pending")
	}
	hist, _ := st.ChatHistory(ctx, 10)
	if len(hist) != 2 || hist[0].Role != "user" || hist[1].Role != "assistant" || hist[1].Text != "Here you go." {
		t.Fatalf("history %+v", hist)
	}

	id2, _ := st.CreateChatJob(ctx, "second", "message", store.JobRequest{Text: "second"})
	before, _ := st.ChatHistoryBefore(ctx, id2, 10)
	if len(before) != 2 {
		t.Fatalf("history before job 2 has %d messages, want 2", len(before))
	}
	_, _ = st.ClaimJob(ctx)
	if n, err := st.RecoverJobs(ctx); err != nil || n != 1 {
		t.Fatalf("recovered %d, %v", n, err)
	}
	j2, _ := st.Job(ctx, id2)
	if j2.Status != "failed" || j2.Error != "Quizroom restarted while Claude was writing, so nothing was added. Press Retry." {
		t.Fatalf("job 2 %+v", j2)
	}
	hist, _ = st.ChatHistory(ctx, 10)
	if last := hist[len(hist)-1]; last.Text != store.RestartMessage || !last.Failed {
		t.Fatalf("last message %+v", last)
	}
}

func TestRetryJob(t *testing.T) {
	st := storetest.New(t)
	req := store.JobRequest{Text: "write questions", Page: store.PageContext{Page: "topic", Topic: "docker"}, Generate: true}
	id, _ := st.CreateChatJob(ctx, "You asked Claude for your first 30 Docker questions.", "request", req)
	if _, err := st.RetryJob(ctx, id); !errors.Is(err, store.ErrNotRetryable) {
		t.Fatalf("retrying a queued job: %v", err)
	}
	_, _ = st.ClaimJob(ctx)
	if _, err := st.RetryJob(ctx, id); !errors.Is(err, store.ErrNotRetryable) {
		t.Fatalf("retrying a running job: %v", err)
	}
	if err := st.FailJob(ctx, id, "Claude stopped"); err != nil {
		t.Fatal(err)
	}
	again, err := st.RetryJob(ctx, id)
	if err != nil || again == id {
		t.Fatalf("retry %d %v", again, err)
	}
	j, _ := st.Job(ctx, again)
	if j.Status != "queued" || !reflect.DeepEqual(j.Request, req) {
		t.Fatalf("the retry is a new queued job with the same request: %+v", j)
	}
	hist, _ := st.ChatHistory(ctx, 10)
	last := hist[len(hist)-1]
	if last.Kind != "request" || last.Role != "user" || last.Text != "Retrying: You asked Claude for your first 30 Docker questions." || last.JobID == nil || *last.JobID != again {
		t.Fatalf("retry message %+v", last)
	}
	if twice, err := st.RetryJob(ctx, id); err != nil || twice != again {
		t.Fatalf("a second press while the retry is pending returns it: %d %v", twice, err)
	}
	_, _ = st.ClaimJob(ctx)
	_ = st.FinishJob(ctx, again, store.JobResult{}, "Done.")
	if _, err := st.RetryJob(ctx, again); !errors.Is(err, store.ErrNotRetryable) {
		t.Fatalf("retrying a done job: %v", err)
	}
	if _, err := st.RetryJob(ctx, 999999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("retrying a missing job: %v", err)
	}
	long := strings.Repeat("why ", 30)
	id3, _ := st.CreateChatJob(ctx, long, "message", store.JobRequest{Text: long})
	_, _ = st.ClaimJob(ctx)
	_ = st.FailJob(ctx, id3, "Claude stopped")
	if _, err := st.RetryJob(ctx, id3); err != nil {
		t.Fatal(err)
	}
	hist, _ = st.ChatHistory(ctx, 10)
	if text := hist[len(hist)-1].Text; text != "Retrying: "+long[:80]+"…" {
		t.Fatalf("long retry text %q", text)
	}
}

func TestHistoryMarksFailures(t *testing.T) {
	st := storetest.New(t)
	ok, _ := st.CreateChatJob(ctx, "hello", "message", store.JobRequest{Text: "hello"})
	_, _ = st.ClaimJob(ctx)
	_ = st.FinishJob(ctx, ok, store.JobResult{}, "Hi.")
	bad, _ := st.CreateChatJob(ctx, "again", "message", store.JobRequest{Text: "again"})
	_, _ = st.ClaimJob(ctx)
	_ = st.FailJob(ctx, bad, "Claude stopped before finishing.")
	hist, _ := st.ChatHistory(ctx, 10)
	if len(hist) != 4 {
		t.Fatalf("history %+v", hist)
	}
	for i, want := range []bool{false, false, false, true} {
		if hist[i].Failed != want || hist[i].Kind != "message" {
			t.Fatalf("message %d %+v, want failed=%v", i, hist[i], want)
		}
	}
	if f := hist[3]; f.Text != "Claude stopped before finishing." || f.JobID == nil || *f.JobID != bad {
		t.Fatalf("failure message %+v", f)
	}
}

func TestAddMistakeDedupes(t *testing.T) {
	st := storetest.New(t)
	m, created, err := st.AddMistake(ctx, "docker", "compose", "depends_on waits until a service is ready", "Only with condition: service_healthy", mistakes.Mock, now)
	if err != nil || !created || m.Status != "active" || m.DueAt == nil || !m.DueAt.Equal(now) || m.Source != "mock" {
		t.Fatalf("%+v created=%v err=%v", m, created, err)
	}
	later := now.Add(time.Hour)
	again, created, err := st.AddMistake(ctx, "docker", "compose", "DEPENDS_ON waits until a service is ready", "x", mistakes.Chat, later)
	if err != nil || created || again.ID != m.ID || again.Hits != 1 || !again.DueAt.Equal(later) {
		t.Fatalf("%+v created=%v err=%v", again, created, err)
	}
	if _, _, err := st.AddMistake(ctx, "docker", "nope", "b", "c", mistakes.Mock, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown subtopic: %v", err)
	}
}

func TestSettingsDefaults(t *testing.T) {
	st := storetest.New(t)
	s, err := st.Settings(ctx)
	if err != nil || s != (store.Settings{Target: 0.75, Model: "sonnet"}) {
		t.Fatalf("%+v %v", s, err)
	}
	if err := st.SaveSettings(ctx, store.Settings{Target: 0.65, Model: "opus"}); err != nil {
		t.Fatal(err)
	}
	if s, _ = st.Settings(ctx); s != (store.Settings{Target: 0.65, Model: "opus"}) {
		t.Fatalf("%+v", s)
	}
}

// Final review #4: the "needs questions" mark is cleared once questions exist,
// and when the mistake resolves.
func TestNeedsQuestionsCleared(t *testing.T) {
	st := storetest.New(t)
	k8s := topic(t, st, "kubernetes")
	seed := seedMistake(t, st, "kubernetes")
	if err := st.SetNeedsQuestions(ctx, seed.ID, true); err != nil {
		t.Fatal(err)
	}
	added, _, err := st.ApplyReply(ctx, 0, format.Clean{Questions: []format.Question{question("Who recreates pods?", fmt.Sprintf("m%d", seed.ID))}}, now)
	if err != nil || len(added) != 1 {
		t.Fatal(err)
	}
	if m, _ := st.Mistake(ctx, seed.ID); m.NeedsQuestions {
		t.Fatal("writing a question tagged with the mistake must clear needs_questions")
	}

	other := seedMistake(t, st, "docker")
	_ = st.SetNeedsQuestions(ctx, other.ID, true)
	qs, _, _ := st.ApplyReply(ctx, 0, format.Clean{Questions: []format.Question{{Topic: "docker", Subtopic: "images", Kind: format.Single, Difficulty: 2,
		Stem: "Who builds images?", Options: []format.Option{{Text: "CI", Correct: true, Explanation: "e"}, {Text: "nodes", Explanation: "e", Mistake: fmt.Sprintf("m%d", other.ID)}, {Text: "etcd", Explanation: "e"}, {Text: "kubelet", Explanation: "e"}}}}}, now)
	_ = st.SetNeedsQuestions(ctx, other.ID, true) // marked again later
	quizID := newQuiz(t, st, topic(t, st, "docker").ID, now)
	show(t, st, quizID, qs[0].ID, 1, now)
	_, _, err = st.ApplyAnswer(ctx, quizID, 1, now, func(s store.AnswerState) (store.AnswerUpdate, error) {
		return store.AnswerUpdate{Chosen: []int{0}, Correct: true, ThetaAfter: s.Rating.Theta, BAfter: s.Question.B,
			Mistakes: []store.MistakeChange{{ID: other.ID, State: mistakes.State{Status: mistakes.Resolved, Stage: 4}, Event: mistakes.ClearEvent}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := st.Mistake(ctx, other.ID); m.Status != "resolved" || m.NeedsQuestions {
		t.Fatalf("a resolved mistake must not need questions: %+v", m)
	}
	_ = k8s
}
