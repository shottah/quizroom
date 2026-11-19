package quiz_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	"quizroom/internal/format"
	"quizroom/internal/quiz"
	"quizroom/internal/rating"
	"quizroom/internal/store"
	"quizroom/internal/store/storetest"
)

var ctx = context.Background()

type clock struct{ t time.Time }

func (c *clock) now() time.Time       { return c.t }
func (c *clock) tick(d time.Duration) { c.t = c.t.Add(d) }
func near(a, b float64) bool          { return math.Abs(a-b) < 1e-9 }

func setup(t *testing.T) (*quiz.Service, *store.Store, *clock) {
	t.Helper()
	st := storetest.New(t)
	c := &clock{t: time.Now().UTC().Add(time.Minute).Truncate(time.Second)}
	return &quiz.Service{St: st, Now: c.now, Rand: rand.New(rand.NewPCG(7, 7))}, st, c
}

func topicID(t *testing.T, st *store.Store, slug string) int64 {
	t.Helper()
	tp, err := st.TopicBySlug(ctx, slug)
	if err != nil {
		t.Fatal(err)
	}
	return tp.ID
}

func ids(opts []quiz.ShownOption) []int {
	out := []int{}
	for _, o := range opts {
		out = append(out, o.ID)
	}
	return out
}

func TestStartRefusesWithFewerThan5(t *testing.T) {
	svc, st, _ := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 4, 2)
	_, err := svc.Start(ctx, "docker")
	var ne quiz.NotEnoughError
	if !errors.As(err, &ne) || ne.Available != 4 {
		t.Fatalf("err = %v, want NotEnoughError{4}", err)
	}
	if ne.Error() != "No quiz right now: a quiz needs 5 questions you haven't seen in the last 7 days, and this topic has 4. Write 15 at your level to add more." {
		t.Fatalf("message %q", ne.Error())
	}
	if _, err := svc.Start(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown topic: %v", err)
	}
}

func TestStartLength(t *testing.T) {
	svc, st, _ := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 7, 2)
	s, err := svc.Start(ctx, "docker")
	if err != nil || s.Length != 7 {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestNextReturnsSameUnansweredQuestion(t *testing.T) {
	svc, st, _ := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 10, 2)
	s, _ := svc.Start(ctx, "docker")
	a, err := svc.Next(ctx, s.QuizID)
	if err != nil || a.Question == nil {
		t.Fatalf("%+v %v", a, err)
	}
	b, _ := svc.Next(ctx, s.QuizID)
	if a.Question.TopicSlug != "docker" {
		t.Fatalf("the shown question must carry its topic slug for the chat, got %q", a.Question.TopicSlug)
	}
	if a.Question.ID != b.Question.ID || a.Position != 1 || b.Position != 1 || !reflect.DeepEqual(ids(a.Question.Options), ids(b.Question.Options)) {
		t.Fatalf("a refresh must show the same question in the same order: %+v vs %+v", a, b)
	}
}

func TestAnswerTwiceReturnsFirstResult(t *testing.T) {
	svc, st, _ := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 10, 2)
	s, _ := svc.Start(ctx, "docker")
	if _, err := svc.Next(ctx, s.QuizID); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Answer(ctx, s.QuizID, 1, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Answer(ctx, s.QuizID, 1, []int{1})
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("second submit must return the first result:\n%+v\n%+v\n%v", first, second, err)
	}
	r, _ := st.Rating(ctx, topicID(t, st, "docker"))
	start := rating.ThetaFromSkill(rating.StartSkill)
	want := rating.Apply(start, rating.BFromDifficulty(2), 0.25, 0, 0, true, false)
	if r.N != 1 || !near(r.Theta, want.Theta) {
		t.Fatalf("rating %+v, want theta %v after one update", r, want.Theta)
	}
}

func TestAnswerUpdatesRatings(t *testing.T) {
	svc, st, _ := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 10, 2)
	s, _ := svc.Start(ctx, "docker")
	shown, _ := svc.Next(ctx, s.QuizID)
	res, err := svc.Answer(ctx, s.QuizID, 1, []int{0})
	if err != nil || !res.Correct {
		t.Fatalf("%+v %v", res, err)
	}
	start := rating.ThetaFromSkill(rating.StartSkill)
	want := rating.Apply(start, rating.BFromDifficulty(2), 0.25, 0, 0, true, false)
	q, _ := st.Question(ctx, shown.Question.ID)
	r, _ := st.Rating(ctx, topicID(t, st, "docker"))
	if !near(q.B, want.B) || !near(r.Theta, want.Theta) || q.N != 1 {
		t.Fatalf("b %v want %v; theta %v want %v", q.B, want.B, r.Theta, want.Theta)
	}
	if res.EloBefore != rating.Elo(start) || res.EloAfter != rating.Elo(want.Theta) || res.Level != "Beginner" {
		t.Fatalf("%+v", res)
	}
	if len(res.Options) != 4 || res.CorrectIDs[0] != 0 {
		t.Fatalf("feedback %+v", res)
	}
}

func addMulti(t *testing.T, st *store.Store, n int) {
	t.Helper()
	var qs []format.Question
	for i := 0; i < n; i++ {
		qs = append(qs, format.Question{Topic: "docker", Subtopic: "compose", Kind: format.Multi, Difficulty: 2,
			Stem: fmt.Sprintf("Which of these does Compose do? (%d)", i),
			Options: []format.Option{
				{Text: "a", Correct: true, Explanation: "e"}, {Text: "b", Correct: true, Explanation: "e"},
				{Text: "c", Correct: true, Explanation: "e"}, {Text: "d", Explanation: "e"}, {Text: "e", Explanation: "e"},
			}})
	}
	if added, _, err := st.ApplyReply(ctx, 0, format.Clean{Questions: qs}, time.Now()); err != nil || len(added) != n {
		t.Fatal(err)
	}
}

func TestPickAllIsAllOrNothing(t *testing.T) {
	svc, st, _ := setup(t)
	addMulti(t, st, 5)
	s, _ := svc.Start(ctx, "docker")
	if _, err := svc.Next(ctx, s.QuizID); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Answer(ctx, s.QuizID, 1, []int{0, 1})
	if err != nil || res.Correct {
		t.Fatalf("two of three correct options must count as wrong: %+v %v", res, err)
	}
}

func TestAnswerRejectsBadChoice(t *testing.T) {
	svc, st, _ := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 10, 2)
	s, _ := svc.Start(ctx, "docker")
	_, _ = svc.Next(ctx, s.QuizID)
	for _, bad := range [][]int{{}, {9}, {0, 0}, {0, 1}} {
		if _, err := svc.Answer(ctx, s.QuizID, 1, bad); !errors.Is(err, quiz.ErrBadChoice) {
			t.Fatalf("choice %v: err %v, want ErrBadChoice", bad, err)
		}
	}
	if res, err := svc.Answer(ctx, s.QuizID, 1, []int{0}); err != nil || !res.Correct {
		t.Fatalf("a valid answer after rejected ones: %+v %v", res, err)
	}
}

// tagged adds n kubernetes/workloads questions whose option 1 (wrong) carries ref.
func tagged(t *testing.T, st *store.Store, n int, ref string, newMistakes ...format.NewMistake) []store.Question {
	t.Helper()
	var qs []format.Question
	for i := 0; i < n; i++ {
		qs = append(qs, format.Question{Topic: "kubernetes", Subtopic: "workloads", Kind: format.Single, Difficulty: 2,
			Stem: fmt.Sprintf("What brings a deleted pod back? (%s %d)", ref, i),
			Options: []format.Option{
				{Text: "The ReplicaSet controller", Correct: true, Explanation: "e"},
				{Text: "The scheduler", Explanation: "e", Mistake: ref},
				{Text: "etcd", Explanation: "e"}, {Text: "The kubelet", Explanation: "e"},
			}})
	}
	added, _, err := st.ApplyReply(ctx, 0, format.Clean{Mistakes: newMistakes, Questions: qs}, time.Now())
	if err != nil || len(added) != n {
		t.Fatalf("%v %d", err, len(added))
	}
	return added
}

func mistakeByBelief(t *testing.T, st *store.Store, belief string) store.Mistake {
	t.Helper()
	ms, _ := st.Mistakes(ctx, 0)
	for _, m := range ms {
		if m.Belief == belief {
			return m
		}
	}
	t.Fatalf("no mistake %q", belief)
	return store.Mistake{}
}

// taggedAt is tagged with a chosen difficulty.
func taggedAt(t *testing.T, st *store.Store, n, difficulty int, ref string, newMistakes ...format.NewMistake) {
	t.Helper()
	var qs []format.Question
	for i := 0; i < n; i++ {
		qs = append(qs, format.Question{Topic: "kubernetes", Subtopic: "workloads", Kind: format.Single, Difficulty: difficulty,
			Stem: fmt.Sprintf("Which component recreates a deleted pod? (%s, %d, %d)", ref, difficulty, i),
			Options: []format.Option{
				{Text: "The ReplicaSet controller", Correct: true, Explanation: "e"},
				{Text: "The scheduler", Explanation: "e", Mistake: ref},
				{Text: "etcd", Explanation: "e"}, {Text: "The kubelet", Explanation: "e"},
			}})
	}
	if added, _, err := st.ApplyReply(ctx, 0, format.Clean{Mistakes: newMistakes, Questions: qs}, time.Now()); err != nil || len(added) != n {
		t.Fatalf("%v %d", err, len(added))
	}
}

func isTagged(t *testing.T, st *store.Store, questionID, mistakeID int64) bool {
	t.Helper()
	q, _ := st.Question(ctx, questionID)
	return len(q.MistakeIDs) > 0 && q.MistakeIDs[0] == mistakeID
}

func TestHitIsDueNextQuizNotThisOne(t *testing.T) {
	svc, st, c := setup(t)
	belief := "The scheduler recreates deleted pods"
	taggedAt(t, st, 12, 3, "n1", format.NewMistake{Key: "n1", Topic: "kubernetes", Subtopic: "workloads", Belief: belief, Correction: "The ReplicaSet controller does"})
	m := mistakeByBelief(t, st, belief)
	if m.Status != "watching" {
		t.Fatalf("a Claude-invented mistake starts watching: %+v", m)
	}

	// Quiz 1 only has tagged questions. The first answer picks the tagged
	// option (a hit); the rest are right.
	q1, _ := svc.Start(ctx, "kubernetes")
	c.tick(time.Minute)
	if _, err := svc.Next(ctx, q1.QuizID); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Answer(ctx, q1.QuizID, 1, []int{1})
	if err != nil || res.Correct || len(res.MistakesHit) != 1 || res.MistakesHit[0].Belief != belief || res.MistakesHit[0].Hits != 1 {
		t.Fatalf("picking the tagged option is a hit: %+v %v", res, err)
	}
	m = mistakeByBelief(t, st, belief)
	if m.Status != "active" || m.DueAt == nil || !m.DueAt.Equal(c.now()) {
		t.Fatalf("after a hit: %+v", m)
	}
	for pos := 2; pos <= q1.Length; pos++ {
		if _, err := svc.Next(ctx, q1.QuizID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Answer(ctx, q1.QuizID, pos, []int{0}); err != nil {
			t.Fatal(err)
		}
	}
	answers, _ := st.QuizAnswers(ctx, q1.QuizID)
	for _, a := range answers {
		if a.SlotMistakeID != nil && *a.SlotMistakeID == m.ID {
			t.Fatal("a mistake hit during a quiz must not come back as a mistake slot in the same quiz")
		}
	}
	if m = mistakeByBelief(t, st, belief); m.Stage != 0 {
		t.Fatalf("right answers while not due must not clear the mistake: %+v", m)
	}

	// Quiz 2: more tagged questions and some untagged ones. Wrong answers on
	// tagged questions use an untagged option, so the mistake stays due until
	// its slot comes up.
	taggedAt(t, st, 8, 3, fmt.Sprintf("m%d", m.ID))
	storetest.AddQuestions(t, st, "kubernetes", "workloads", 15, 2)
	c.tick(time.Minute)
	q2, err := svc.Start(ctx, "kubernetes")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for pos := 1; pos <= q2.Length && !found; pos++ {
		sh, err := svc.Next(ctx, q2.QuizID)
		if err != nil {
			t.Fatal(err)
		}
		answers, _ = st.QuizAnswers(ctx, q2.QuizID)
		if a := answers[len(answers)-1]; a.Slot == "mistake" && a.SlotMistakeID != nil && *a.SlotMistakeID == m.ID {
			found = true
		}
		choice := []int{0}
		if isTagged(t, st, sh.Question.ID, m.ID) {
			choice = []int{2}
		}
		if _, err := svc.Answer(ctx, q2.QuizID, pos, choice); err != nil {
			t.Fatal(err)
		}
	}
	if !found {
		t.Fatal("the mistake should fill a mistake slot in the next quiz")
	}
}

func TestMistakeSlotClears(t *testing.T) {
	svc, st, c := setup(t)
	k8s := topicID(t, st, "kubernetes")
	ms, _ := st.Mistakes(ctx, k8s)
	seed := ms[0]
	tagged(t, st, 1, fmt.Sprintf("m%d", seed.ID))
	storetest.AddQuestions(t, st, "kubernetes", "workloads", 9, 2)

	s, _ := svc.Start(ctx, "kubernetes")
	var clearedAt time.Time
	for pos := 1; pos <= s.Length; pos++ {
		c.tick(time.Minute)
		sh, err := svc.Next(ctx, s.QuizID)
		if err != nil {
			t.Fatal(err)
		}
		q, _ := st.Question(ctx, sh.Question.ID)
		if len(q.MistakeIDs) == 1 && q.MistakeIDs[0] == seed.ID {
			clearedAt = c.now()
		}
		if _, err := svc.Answer(ctx, s.QuizID, pos, []int{0}); err != nil {
			t.Fatal(err)
		}
	}
	if clearedAt.IsZero() {
		t.Fatal("the tagged question for a due mistake was never shown")
	}
	m, _ := st.Mistake(ctx, seed.ID)
	if m.Stage != 1 || m.DueAt == nil || !m.DueAt.Equal(clearedAt.Add(24*time.Hour)) {
		t.Fatalf("after a right answer while due: %+v, want stage 1 due %v", m, clearedAt.Add(24*time.Hour))
	}
}

func TestQuizFinishesAndSummarizes(t *testing.T) {
	svc, st, _ := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 10, 2)
	s, _ := svc.Start(ctx, "docker")
	right := 0
	for pos := 1; pos <= s.Length; pos++ {
		_, _ = svc.Next(ctx, s.QuizID)
		choice := []int{0}
		if pos%3 == 0 {
			choice = []int{2}
		} else {
			right++
		}
		res, err := svc.Answer(ctx, s.QuizID, pos, choice)
		if err != nil {
			t.Fatal(err)
		}
		if res.Finished != (pos == s.Length) {
			t.Fatalf("position %d: finished %v", pos, res.Finished)
		}
	}
	done, _ := svc.Next(ctx, s.QuizID)
	if !done.Done || done.Question != nil {
		t.Fatalf("after the last answer: %+v", done)
	}
	sum, err := svc.Summary(ctx, s.QuizID)
	if err != nil {
		t.Fatal(err)
	}
	answers, _ := st.QuizAnswers(ctx, s.QuizID)
	expected := 0.0
	for _, a := range answers {
		expected += a.P
	}
	if sum.Answered != 10 || sum.Right != right || !near(sum.Expected, expected) || sum.Topic != "Docker" || sum.Length != 10 {
		t.Fatalf("summary %+v, want right %d expected %v", sum, right, expected)
	}
}

func TestFlagUndoesHits(t *testing.T) {
	svc, st, c := setup(t)
	k8s := topicID(t, st, "kubernetes")
	ms, _ := st.Mistakes(ctx, k8s)
	seed := ms[0]
	qs := tagged(t, st, 5, fmt.Sprintf("m%d", seed.ID))
	s, _ := svc.Start(ctx, "kubernetes")
	c.tick(time.Minute)
	sh, _ := svc.Next(ctx, s.QuizID)
	if _, err := svc.Answer(ctx, s.QuizID, 1, []int{1}); err != nil {
		t.Fatal(err)
	}
	if m, _ := st.Mistake(ctx, seed.ID); m.Hits != 1 {
		t.Fatalf("hit not recorded: %+v", m)
	}
	if err := svc.Flag(ctx, sh.Question.ID); err != nil {
		t.Fatal(err)
	}
	m, _ := st.Mistake(ctx, seed.ID)
	if m.Hits != 0 || !m.DueAt.Equal(seed.CreatedAt) {
		t.Fatalf("after the flag: %+v", m)
	}
	el, _ := st.Eligible(ctx, k8s, 0, c.now().Add(30*24*time.Hour))
	for _, e := range el {
		if e.ID == sh.Question.ID {
			t.Fatal("a flagged question must not be shown again")
		}
	}
	_ = qs
}
