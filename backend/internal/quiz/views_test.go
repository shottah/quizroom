package quiz_test

import (
	"testing"
	"time"

	"quizroom/internal/quiz"
	"quizroom/internal/store"
	"quizroom/internal/store/storetest"
)

// play answers the next n questions of a quiz with option 0 (right for
// storetest questions), a minute apart, and returns when each was shown.
func play(t *testing.T, svc *quiz.Service, c *clock, quizID int64, n int) []time.Time {
	t.Helper()
	var shown []time.Time
	for i := 0; i < n; i++ {
		next, err := svc.Next(ctx, quizID)
		if err != nil || next.Question == nil {
			t.Fatalf("next %d: %+v %v", i, next, err)
		}
		shown = append(shown, c.now())
		if _, err := svc.Answer(ctx, quizID, next.Position, []int{0}); err != nil {
			t.Fatal(err)
		}
		c.tick(time.Minute)
	}
	return shown
}

func card(t *testing.T, svc *quiz.Service, slug string) quiz.Card {
	t.Helper()
	cards, err := svc.Cards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cards {
		if c.Slug == slug {
			return c
		}
	}
	t.Fatalf("no %s card in %+v", slug, cards)
	return quiz.Card{}
}

func TestBankState(t *testing.T) {
	cases := []struct {
		near, available int
		want            string
	}{
		{25, 30, "plenty"}, {20, 30, "plenty"},
		{19, 30, "low"}, {15, 30, "low"}, {10, 30, "low"},
		{9, 30, "offlevel"}, {5, 30, "offlevel"},
		// a short bank whose next quiz all fits is low, not off-level
		{7, 7, "low"}, {6, 7, "offlevel"},
		{2, 4, "blocked"}, {0, 0, "blocked"},
	}
	for _, c := range cases {
		if got := quiz.BankState(c.near, c.available); got != c.want {
			t.Errorf("BankState(%d, %d) = %q, want %q", c.near, c.available, got, c.want)
		}
	}
}

func TestCardsForEmptyTopic(t *testing.T) {
	svc, _, _ := setup(t)
	cards, err := svc.Cards(ctx)
	if err != nil || len(cards) != 2 {
		t.Fatalf("%+v %v", cards, err)
	}
	for _, c := range cards {
		if !c.Empty || c.Elo != 17 || c.Level != "Beginner" || c.Spark == nil || len(c.Spark) != 0 || c.Questions != 0 {
			t.Fatalf("empty topic card %+v", c)
		}
		if c.Bank.State != "blocked" || c.Bank.Writing || c.Bank.QuizLength != 0 || c.Bank.BackAt != nil || c.Bank.Questions != 0 {
			t.Fatalf("empty topic bank %+v", c.Bank)
		}
	}
	if cards[1].MistakesDue != 7 {
		t.Fatalf("kubernetes has 7 seed mistakes due, got %d", cards[1].MistakesDue)
	}
}

func TestCardBank(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 12, 2)
	s, err := svc.Start(ctx, "docker")
	if err != nil {
		t.Fatal(err)
	}
	shown := play(t, svc, c, s.QuizID, 5)
	b := card(t, svc, "docker").Bank
	if b.Questions != 12 || b.Available != 7 || b.Resting != 5 || b.QuizLength != 7 {
		t.Fatalf("bank %+v", b)
	}
	if b.NearLevel+b.Harder+b.Easier != b.Available {
		t.Fatalf("near %d + harder %d + easier %d must be the %d available", b.NearLevel, b.Harder, b.Easier, b.Available)
	}
	want := shown[0].Add(7 * 24 * time.Hour)
	if b.BackAt == nil || b.BackAt.Sub(want).Abs() > 2*time.Second {
		t.Fatalf("back_at %v, want %v (first shown + 7 days)", b.BackAt, want)
	}
	if b.State != quiz.BankState(b.NearLevel, b.Available) {
		t.Fatalf("state %q for %+v", b.State, b)
	}
}

func TestCardWriting(t *testing.T) {
	svc, st, _ := setup(t)
	page := store.PageContext{Page: "topic", Topic: "docker"}
	if _, err := st.CreateChatJob(ctx, "why is B wrong?", "message", store.JobRequest{Text: "why is B wrong?", Page: page}); err != nil {
		t.Fatal(err)
	}
	if b := card(t, svc, "docker").Bank; b.Writing || b.State != "blocked" {
		t.Fatalf("a typed message isn't Claude writing questions: %+v", b)
	}
	if _, err := st.CreateChatJob(ctx, "write", "request", store.JobRequest{Text: "write", Page: page, Generate: true}); err != nil {
		t.Fatal(err)
	}
	if b := card(t, svc, "docker").Bank; !b.Writing || b.State != "blocked" {
		t.Fatalf("docker %+v, want writing over a blocked bank", b)
	}
	if b := card(t, svc, "kubernetes").Bank; b.Writing {
		t.Fatalf("kubernetes %+v isn't being written", b)
	}
}

func TestDetailAndSummary(t *testing.T) {
	svc, _, _ := setup(t)
	d, err := svc.Detail(ctx, "kubernetes")
	if err != nil || d.Name != "Kubernetes" || len(d.Subtopics) != 10 || len(d.Mistakes) != 7 || d.History == nil {
		t.Fatalf("%+v %v", d, err)
	}
	s, err := svc.TopicSummary(ctx, "kubernetes")
	if err != nil || len(s.ActiveMistakes) != 7 || s.Weakest == nil || s.Recent.Answered != 0 {
		t.Fatalf("%+v %v", s, err)
	}
	all, _ := svc.Mistakes(ctx, "")
	if len(all) != 11 || all[0].Topic == "" || all[0].Subtopic == "" {
		t.Fatalf("mistakes %+v", all)
	}
}

func TestOpenAndLastQuiz(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 12, 2)
	s, _ := svc.Start(ctx, "docker")
	play(t, svc, c, s.QuizID, 3)
	cd := card(t, svc, "docker")
	if cd.OpenQuiz == nil || cd.OpenQuiz.ID != s.QuizID || cd.OpenQuiz.Answered != 3 || cd.OpenQuiz.Length != 10 || cd.OpenQuiz.Finished || cd.LastQuiz != nil {
		t.Fatalf("open %+v last %+v", cd.OpenQuiz, cd.LastQuiz)
	}
	play(t, svc, c, s.QuizID, 7)
	cd = card(t, svc, "docker")
	if cd.LastQuiz == nil || !cd.LastQuiz.Finished || cd.LastQuiz.Answered != 10 || cd.LastQuiz.Right != 10 || cd.OpenQuiz != nil {
		t.Fatalf("open %+v last %+v", cd.OpenQuiz, cd.LastQuiz)
	}
	if cd.LastQuiz.EloBefore != 17 || cd.LastQuiz.EloAfter != cd.Elo {
		t.Fatalf("last quiz Elo %v → %v, card %v", cd.LastQuiz.EloBefore, cd.LastQuiz.EloAfter, cd.Elo)
	}
}

func TestDetailHasQuizzesBankAndRecent(t *testing.T) {
	svc, st, c := setup(t)
	storetest.AddQuestions(t, st, "docker", "compose", 12, 2)
	s, _ := svc.Start(ctx, "docker")
	play(t, svc, c, s.QuizID, 10)
	sum, err := svc.Summary(ctx, s.QuizID)
	if err != nil {
		t.Fatal(err)
	}
	d, err := svc.Detail(ctx, "docker")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Quizzes) != 1 {
		t.Fatalf("quizzes %+v", d.Quizzes)
	}
	q := d.Quizzes[0]
	if q.ID != s.QuizID || q.Right != sum.Right || !near(q.Expected, sum.Expected) || q.Answered != 10 || !q.Finished || q.EloAfter != sum.EloAfter {
		t.Fatalf("row %+v vs summary %+v", q, sum)
	}
	if d.Recent.Answered != 10 || d.Recent.Right != 10 || d.Bank.Questions != 12 || d.Bank.Resting != 10 {
		t.Fatalf("recent %+v bank %+v", d.Recent, d.Bank)
	}
	if len(d.History) != 10 {
		t.Fatalf("history %+v", d.History)
	}
	for _, p := range d.History {
		if p.QuizID != s.QuizID {
			t.Fatalf("history point %+v, want quiz %d", p, s.QuizID)
		}
	}
}

func TestWeakRuleSame(t *testing.T) {
	svc, st, c := setup(t)
	subs := []string{"images", "dockerfile", "tags", "compose"}
	for _, sub := range subs {
		storetest.AddQuestions(t, st, "docker", sub, 5, 4)
	}
	for range 2 {
		s, err := svc.Start(ctx, "docker")
		if err != nil {
			t.Fatal(err)
		}
		for {
			next, err := svc.Next(ctx, s.QuizID)
			if err != nil {
				t.Fatal(err)
			}
			if next.Question == nil {
				break
			}
			if _, err := svc.Answer(ctx, s.QuizID, next.Position, []int{1}); err != nil {
				t.Fatal(err)
			}
			c.tick(time.Minute)
		}
	}
	d, err := svc.Detail(ctx, "docker")
	if err != nil {
		t.Fatal(err)
	}
	weak := map[string]bool{}
	for _, v := range d.Subtopics {
		if v.Weak {
			weak[v.Slug] = true
		}
	}
	sum, err := svc.TopicSummary(ctx, "docker")
	if err != nil {
		t.Fatal(err)
	}
	if len(weak) != 4 || len(sum.Weakest) != 4 {
		t.Fatalf("every subtopic with 5+ answers below prediction is weak: detail %v, summary %+v", weak, sum.Weakest)
	}
	for i, v := range sum.Weakest {
		if !weak[v.Slug] {
			t.Fatalf("summary weak %s isn't weak on the topic page", v.Slug)
		}
		if i > 0 && float64(v.Right)-v.Expected < float64(sum.Weakest[i-1].Right)-sum.Weakest[i-1].Expected {
			t.Fatalf("weakest first: %+v", sum.Weakest)
		}
	}
}

func TestDetailEmptyTopic(t *testing.T) {
	svc, _, _ := setup(t)
	d, err := svc.Detail(ctx, "docker")
	if err != nil {
		t.Fatal(err)
	}
	if d.Quizzes == nil || len(d.Quizzes) != 0 || d.History == nil || len(d.History) != 0 || d.Recent.Answered != 0 || d.Bank.State != "blocked" {
		t.Fatalf("empty detail %+v", d)
	}
	for _, m := range d.Mistakes {
		if m.Topic != "docker" {
			t.Fatalf("mistake %+v", m)
		}
	}
}

func TestCardNewTopicElo(t *testing.T) {
	svc, _, _ := setup(t)
	c := card(t, svc, "docker")
	if c.Elo != 17 || c.Margin != 100 || c.Level != "Beginner" {
		t.Fatalf("a new topic is Elo 17 ± 100, Beginner: %+v", c)
	}
}
