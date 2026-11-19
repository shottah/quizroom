package chat_test

import (
	"github.com/jackc/pgx/v5"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"quizroom/internal/rating"
	"strings"
	"sync"
	"testing"
	"time"

	"quizroom/internal/chat"
	"quizroom/internal/format"
	"quizroom/internal/runner"
	"quizroom/internal/store"
	"quizroom/internal/store/storetest"
)

var ctx = context.Background()

// claudeJSON is what `claude -p --output-format json` prints.
func claudeJSON(t *testing.T, r format.Reply) string {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"type": "result", "is_error": false, "result": "", "structured_output": r, "total_cost_usd": 0.02})
	return string(b)
}

func TestParseOutput(t *testing.T) {
	r, cost, err := chat.ParseOutput(claudeJSON(t, format.Reply{Reply: "hi", Questions: []format.Question{}, NewMistakes: []format.NewMistake{}, NewTopics: []format.NewTopic{}}))
	if err != nil || r.Reply != "hi" || cost != 0.02 {
		t.Fatalf("%+v %v %v", r, cost, err)
	}
}

func TestParseOutputMissingStructuredOutput(t *testing.T) {
	for _, out := range []string{`{"type":"result","is_error":false,"result":"plain text"}`, `not json`, ``} {
		if _, _, err := chat.ParseOutput(out); !errors.Is(err, chat.ErrFormat) || err.Error() != "Claude's reply didn't match the format, so nothing was added. Press Retry." {
			t.Fatalf("output %q: %v", out, err)
		}
	}
}

func TestParseOutputClaudeError(t *testing.T) {
	_, _, err := chat.ParseOutput(`{"type":"result","is_error":true,"result":"Claude usage limit reached"}`)
	var ce chat.ClaudeError
	if !errors.As(err, &ce) || err.Error() != "Claude reported an error: Claude usage limit reached" {
		t.Fatalf("%v", err)
	}
}

func TestClientMapsStatuses(t *testing.T) {
	status := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_ = json.NewEncoder(w).Encode(runner.Health{OK: true, Claude: "2.1.327 (Claude Code)", Auth: "subscription"})
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"exit_code":0,"stdout":"x"}`))
	}))
	defer srv.Close()
	c := &chat.Client{BaseURL: srv.URL}
	req := runner.Request{Prompt: "p", Schema: []byte(`{}`), Model: "sonnet"}
	for code, want := range map[int]error{412: chat.ErrNotSignedIn, 409: chat.ErrBusy} {
		status = code
		if _, err := c.Call(ctx, req); !errors.Is(err, want) {
			t.Fatalf("status %d: %v", code, err)
		}
	}
	status = 200
	if resp, err := c.Call(ctx, req); err != nil || resp.Stdout != "x" {
		t.Fatalf("%+v %v", resp, err)
	}
	if h, err := c.Health(ctx); err != nil || h.Auth != "subscription" {
		t.Fatalf("%+v %v", h, err)
	}
	down := &chat.Client{BaseURL: "http://127.0.0.1:1"}
	if _, err := down.Call(ctx, req); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("closed port: %v", err)
	}
}

func TestBuildPrompt(t *testing.T) {
	st := storetest.New(t)
	long := strings.Repeat("y", 120) + "Z?"
	added, _, err := st.ApplyReply(ctx, 0, format.Clean{Questions: []format.Question{q(long), q("What does depends_on do?")}}, time.Now())
	if err != nil || len(added) != 2 {
		t.Fatal(err)
	}
	onScreen := added[1]
	k8s, _ := st.TopicBySlug(ctx, "kubernetes")
	ms, _ := st.Mistakes(ctx, k8s.ID)
	var seed store.Mistake
	for _, m := range ms {
		if m.Belief == "etcd or the scheduler brings a deleted Deployment pod back" {
			seed = m
		}
	}
	snap, err := chat.BuildSnapshot(ctx, st, store.PageContext{Page: "feedback", Topic: "docker", QuestionID: onScreen.ID, Chosen: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	history := []store.ChatMessage{{Role: "user", Text: "earlier question"}, {Role: "assistant", Text: "earlier answer"}}
	p := chat.BuildPrompt(snap, history, "why is b wrong?")
	for _, want := range []string{
		`docker "Docker"`, "- compose: Compose",
		fmt.Sprintf("m%d [kubernetes/workloads] active, due:", seed.ID),
		strings.Repeat("y", 120) + "\n", // the recent stem, cut to 120 characters
		"# On screen", "What does depends_on do?", "The learner chose: B",
		"user: earlier question", "assistant: earlier answer",
		"# Message\nwhy is b wrong?",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt is missing %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "yZ?") {
		t.Fatal("recent stems must be cut to 120 characters")
	}
	sys := chat.System()
	for _, want := range []string{"Their first topics are Docker and Kubernetes", "1–3", "Beginner", "Kubernetes 1.37", "Never mention the learner's own projects."} {
		if !strings.Contains(sys, want) {
			t.Fatalf("system prompt is missing %q", want)
		}
	}
}

func TestSummarize(t *testing.T) {
	topics := map[int64]string{1: "Docker", 2: "Kubernetes"}
	subs := map[int64]string{10: "Compose", 11: "Images", 20: "Services and networking"}
	q := func(topic, sub int64) store.Question { return store.Question{TopicID: topic, SubtopicID: sub} }
	dup := []format.Dropped{{Reason: "two options were the same"}, {Reason: "two options were the same"}, {Reason: "too long"}}
	cases := []struct {
		added   []store.Question
		dropped []format.Dropped
		want    string
	}{
		{[]store.Question{q(2, 20), q(2, 20)}, nil, "Added 2 questions to Kubernetes › Services and networking."},
		{[]store.Question{q(1, 10)}, nil, "Added 1 question to Docker › Compose."},
		{[]store.Question{q(1, 10), q(1, 11), q(1, 11)}, nil, "Added 3 questions to Docker."},
		{[]store.Question{q(1, 10), q(2, 20)}, nil, "Added 2 questions."},
		{[]store.Question{q(1, 10), q(1, 10)}, dup[:1], "Added 2 questions to Docker › Compose; dropped 1: two options were the same."},
		{nil, dup, "Added 0 questions; dropped 3: two options were the same; too long."},
		{nil, nil, ""},
	}
	for _, c := range cases {
		if got := chat.Summarize(c.added, topics, subs, c.dropped); got != c.want {
			t.Fatalf("Summarize = %q, want %q", got, c.want)
		}
	}
}

// fakeRunner answers /claude calls from a queue: an int is a status code, a
// string is a claude stdout, and an error-shaped string "exit:N" is a failed run.
type fakeRunner struct {
	mu       sync.Mutex
	replies  []any
	requests []runner.Request
	srv      *httptest.Server
}

func newFakeRunner(t *testing.T, replies ...any) *fakeRunner {
	f := &fakeRunner{replies: replies}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var req runner.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.requests = append(f.requests, req)
		if len(f.replies) == 0 {
			w.WriteHeader(500)
			return
		}
		next := f.replies[0]
		f.replies = f.replies[1:]
		switch v := next.(type) {
		case runner.Response:
			_ = json.NewEncoder(w).Encode(v)
		case int:
			w.WriteHeader(v)
		case string:
			code := 0
			if strings.HasPrefix(v, "exit:") {
				fmt.Sscanf(v, "exit:%d", &code)
				v = ""
			}
			_ = json.NewEncoder(w).Encode(runner.Response{ExitCode: code, Stdout: v})
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func q(stem string, options ...format.Option) format.Question {
	if len(options) == 0 {
		options = []format.Option{{Text: "a", Correct: true, Explanation: "e"}, {Text: "b", Explanation: "e"}, {Text: "c", Explanation: "e"}, {Text: "d", Explanation: "e"}}
	}
	return format.Question{Topic: "docker", Subtopic: "compose", Kind: format.Single, Difficulty: 3, Stem: stem, Options: options}
}

func reply(text string, qs ...format.Question) format.Reply {
	return format.Reply{Reply: text, Questions: qs, NewMistakes: []format.NewMistake{}, NewTopics: []format.NewTopic{}}
}

func worker(st *store.Store, f *fakeRunner) *chat.Worker {
	return &chat.Worker{St: st, Runner: &chat.Client{BaseURL: f.srv.URL}, Sleep: func(time.Duration) {}}
}

func lastMessage(t *testing.T, st *store.Store) string {
	t.Helper()
	h, _ := st.ChatHistory(ctx, 1)
	if len(h) == 0 {
		t.Fatal("no chat messages")
	}
	return h[0].Text
}

func TestWorkerAddsQuestionsAndSummarizes(t *testing.T) {
	st := storetest.New(t)
	dupOpts := []format.Option{{Text: "same", Correct: true, Explanation: "e"}, {Text: "SAME", Explanation: "e"}, {Text: "c", Explanation: "e"}, {Text: "d", Explanation: "e"}}
	f := newFakeRunner(t, claudeJSON(t, reply("Here are three.", q("What does depends_on do?"), q("What does a healthcheck do?"), q("Which is duplicated?", dupOpts...))))
	svc := &chat.Service{St: st}
	acc, err := svc.Send(ctx, "write 3 compose questions", store.PageContext{Page: "home"})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := worker(st, f).ProcessOne(ctx); !ok || err != nil {
		t.Fatalf("ProcessOne = %v, %v", ok, err)
	}
	job, _ := svc.Job(ctx, acc.JobID)
	if job.Status != "done" || job.Added != 2 {
		t.Fatalf("job %+v", job)
	}
	if got := lastMessage(t, st); !strings.HasPrefix(got, "Here are three.") || !strings.HasSuffix(got, "Added 2 questions to Docker › Compose; dropped 1: two options were the same.") {
		t.Fatalf("assistant message %q", got)
	}
	req := f.requests[0]
	if req.System == "" || !strings.Contains(string(req.Schema), `"new_topics"`) || req.Model != "sonnet" || req.TimeoutS != 600 || !strings.Contains(req.Prompt, "write 3 compose questions") {
		t.Fatalf("runner request %+v", req)
	}
}

func TestWorkerNotSignedIn(t *testing.T) {
	st := storetest.New(t)
	f := newFakeRunner(t, 412)
	acc, _ := (&chat.Service{St: st}).Send(ctx, "hello", store.PageContext{Page: "home"})
	_, _ = worker(st, f).ProcessOne(ctx)
	job, _ := (&chat.Service{St: st}).Job(ctx, acc.JobID)
	if job.Status != "failed" || !strings.Contains(job.Error, "make token") || !strings.Contains(lastMessage(t, st), "make token") {
		t.Fatalf("job %+v, message %q", job, lastMessage(t, st))
	}
}

func TestWorkerRunnerDown(t *testing.T) {
	st := storetest.New(t)
	acc, _ := (&chat.Service{St: st}).Send(ctx, "hello", store.PageContext{Page: "home"})
	w := &chat.Worker{St: st, Runner: &chat.Client{BaseURL: "http://127.0.0.1:1"}, Sleep: func(time.Duration) {}}
	_, _ = w.ProcessOne(ctx)
	job, _ := (&chat.Service{St: st}).Job(ctx, acc.JobID)
	if job.Status != "failed" || !strings.Contains(job.Error, "make doctor") {
		t.Fatalf("job %+v", job)
	}
}

func TestWorkerFailedRun(t *testing.T) {
	st := storetest.New(t)
	f := newFakeRunner(t, "exit:1")
	acc, _ := (&chat.Service{St: st}).Send(ctx, "hello", store.PageContext{Page: "home"})
	_, _ = worker(st, f).ProcessOne(ctx)
	job, _ := (&chat.Service{St: st}).Job(ctx, acc.JobID)
	if job.Status != "failed" || job.Error != "Claude stopped before finishing (exit code 1), so nothing was added. Press Retry. If it keeps happening, run `make logs` in quizroom/." {
		t.Fatalf("job %+v", job)
	}
}

func TestWorkerBusyRequeues(t *testing.T) {
	st := storetest.New(t)
	f := newFakeRunner(t, 409, claudeJSON(t, reply("ok")))
	svc := &chat.Service{St: st}
	acc, _ := svc.Send(ctx, "hello", store.PageContext{Page: "home"})
	w := worker(st, f)
	_, _ = w.ProcessOne(ctx)
	if job, _ := svc.Job(ctx, acc.JobID); job.Status != "queued" {
		t.Fatalf("a busy runner requeues the job: %+v", job)
	}
	_, _ = w.ProcessOne(ctx)
	if job, _ := svc.Job(ctx, acc.JobID); job.Status != "done" {
		t.Fatalf("job %+v", job)
	}
}

func TestWorkerFollowupForEmptyTopic(t *testing.T) {
	st := storetest.New(t)
	f := newFakeRunner(t, claudeJSON(t, reply("First batch.", q("first?"))), claudeJSON(t, reply("Second batch.", q("second?"))))
	svc := &chat.Service{St: st}
	acc, err := svc.Generate(ctx, "docker")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = worker(st, f).ProcessOne(ctx)
	if len(f.requests) != 2 {
		t.Fatalf("an empty topic gets two calls, got %d", len(f.requests))
	}
	if !strings.Contains(f.requests[0].Prompt, "Write my first questions for Docker: 15 questions spread across difficulties 1–6.") ||
		!strings.Contains(f.requests[1].Prompt, "Write 15 more questions for Docker, spread across difficulties 1–6, different from the ones you just wrote.") {
		t.Fatalf("prompts:\n%s\n---\n%s", f.requests[0].Prompt, f.requests[1].Prompt)
	}
	job, _ := svc.Job(ctx, acc.JobID)
	if job.Status != "done" || job.Added != 2 {
		t.Fatalf("job %+v", job)
	}
	// A topic that has questions gets one call with the "more" message.
	f2 := newFakeRunner(t, claudeJSON(t, reply("More.", q("third?"))))
	_, _ = svc.Generate(ctx, "docker")
	_, _ = worker(st, f2).ProcessOne(ctx)
	if len(f2.requests) != 1 || !strings.Contains(f2.requests[0].Prompt, "Write 15 questions for Docker around my skill") {
		t.Fatalf("requests %d", len(f2.requests))
	}
}

func TestWorkerNewTopicTriggersFollowup(t *testing.T) {
	st := storetest.New(t)
	tf := format.Reply{Reply: "Created Terraform.", NewMistakes: []format.NewMistake{},
		NewTopics: []format.NewTopic{{Slug: "terraform", Name: "Terraform", Description: "IaC", ConfusionGroups: []string{},
			Subtopics: []format.NewSubtopic{{Slug: "state", Name: "State"}, {Slug: "modules", Name: "Modules"}}}},
		Questions: []format.Question{{Topic: "terraform", Subtopic: "state", Kind: format.Single, Difficulty: 2, Stem: "What is state?",
			Options: []format.Option{{Text: "a", Correct: true, Explanation: "e"}, {Text: "b", Explanation: "e"}, {Text: "c", Explanation: "e"}, {Text: "d", Explanation: "e"}}}}}
	second := format.Reply{Reply: "More.", NewMistakes: []format.NewMistake{}, NewTopics: []format.NewTopic{},
		Questions: []format.Question{{Topic: "terraform", Subtopic: "modules", Kind: format.Single, Difficulty: 2, Stem: "What is a module?",
			Options: []format.Option{{Text: "a", Correct: true, Explanation: "e"}, {Text: "b", Explanation: "e"}, {Text: "c", Explanation: "e"}, {Text: "d", Explanation: "e"}}}}}
	f := newFakeRunner(t, claudeJSON(t, tf), claudeJSON(t, second))
	acc, _ := (&chat.Service{St: st}).Send(ctx, "quiz me on Terraform", store.PageContext{Page: "home"})
	_, _ = worker(st, f).ProcessOne(ctx)
	if len(f.requests) != 2 || !strings.Contains(f.requests[1].Prompt, "Write 15 more questions for Terraform") {
		t.Fatalf("requests %d", len(f.requests))
	}
	job, _ := (&chat.Service{St: st}).Job(ctx, acc.JobID)
	msg := lastMessage(t, st)
	if job.Added != 2 || !strings.Contains(msg, "Created topic Terraform with 2 subtopics.") {
		t.Fatalf("job %+v message %q", job, msg)
	}
	if _, err := st.TopicBySlug(ctx, "terraform"); err != nil {
		t.Fatal(err)
	}
}

func TestSendRejectsBadText(t *testing.T) {
	st := storetest.New(t)
	svc := &chat.Service{St: st}
	for _, text := range []string{"  ", strings.Repeat("x", 4001)} {
		if _, err := svc.Send(ctx, text, store.PageContext{}); !errors.Is(err, chat.ErrBadText) {
			t.Fatalf("%v", err)
		}
	}
	h, _ := svc.History(ctx)
	if len(h.Messages) != 0 || h.PendingJob != nil {
		t.Fatalf("%+v", h)
	}
}

// Final review #5: a rejected or expired credential reads as "not signed in".
func TestWorkerAuthFailure(t *testing.T) {
	st := storetest.New(t)
	f := newFakeRunner(t, "exit:1")
	f.replies = []any{runner.Response{ExitCode: 1, Stdout: `{"type":"result","is_error":true,"result":"OAuth token has expired. Please obtain a new token or refresh your existing token."}`}}
	acc, _ := (&chat.Service{St: st}).Send(ctx, "hello", store.PageContext{Page: "home"})
	_, _ = worker(st, f).ProcessOne(ctx)
	job, _ := (&chat.Service{St: st}).Job(ctx, acc.JobID)
	if job.Status != "failed" || !strings.Contains(job.Error, "make token") || !strings.Contains(job.Error, "OAuth token has expired") {
		t.Fatalf("job %+v", job)
	}
	if last, _ := st.LastJobError(ctx); !strings.Contains(last, "make token") {
		t.Fatalf("LastJobError = %q", last)
	}
}

// Final review #2: "why is B wrong?" must mean the second option on screen.
func TestBuildPromptUsesDisplayedLetters(t *testing.T) {
	st := storetest.New(t)
	added, _, err := st.ApplyReply(ctx, 0, format.Clean{Questions: []format.Question{q("Which flag publishes a port?",
		format.Option{Text: "opt-zero", Correct: true, Explanation: "e0"}, format.Option{Text: "opt-one", Explanation: "e1"},
		format.Option{Text: "opt-two", Explanation: "e2"}, format.Option{Text: "opt-three", Explanation: "e3"})}}, time.Now())
	if err != nil || len(added) != 1 {
		t.Fatal(err)
	}
	snap, _ := chat.BuildSnapshot(ctx, st, store.PageContext{Page: "feedback", Topic: "docker", QuestionID: added[0].ID, Chosen: []int{0}, Order: []int{2, 0, 3, 1}})
	p := chat.BuildPrompt(snap, nil, "why is B right?")
	for _, want := range []string{"- A (wrong): opt-two", "- B (correct): opt-zero", "- C (wrong): opt-three", "- D (wrong): opt-one", "The learner chose: B"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt is missing %q:\n%s", want, p)
		}
	}
	// Without an order, options keep their stored order.
	snap, _ = chat.BuildSnapshot(ctx, st, store.PageContext{Page: "feedback", QuestionID: added[0].ID, Chosen: []int{1}})
	if p := chat.BuildPrompt(snap, nil, "x"); !strings.Contains(p, "- A (correct): opt-zero") || !strings.Contains(p, "The learner chose: B") {
		t.Fatalf("fallback order:\n%s", p)
	}
}

// Final review #3: Claude is told which page and topic the learner is on.
func TestBuildPromptIncludesPage(t *testing.T) {
	st := storetest.New(t)
	snap, _ := chat.BuildSnapshot(ctx, st, store.PageContext{Page: "topic", Topic: "kubernetes"})
	if p := chat.BuildPrompt(snap, nil, "write 10 harder ones"); !strings.Contains(p, "# Page\nThe learner is on the topic page for kubernetes.") {
		t.Fatalf("prompt:\n%s", p)
	}
	snap, _ = chat.BuildSnapshot(ctx, st, store.PageContext{Page: "home"})
	if p := chat.BuildPrompt(snap, nil, "hi"); !strings.Contains(p, "# Page\nThe learner is on the home page.") {
		t.Fatalf("prompt:\n%s", p)
	}
}

// Final review #8: a second "write questions" press while the first is still
// running returns the same job instead of queueing another.
func TestGenerateWhilePendingReturnsSameJob(t *testing.T) {
	st := storetest.New(t)
	svc := &chat.Service{St: st}
	a, err := svc.Generate(ctx, "docker")
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Generate(ctx, "docker")
	if err != nil || b.JobID != a.JobID {
		t.Fatalf("second press made job %d, want %d (%v)", b.JobID, a.JobID, err)
	}
	if c, _ := svc.Generate(ctx, "kubernetes"); c.JobID == a.JobID {
		t.Fatal("another topic's button is a different request")
	}
}

func TestGenerateStoresRequestMessage(t *testing.T) {
	st := storetest.New(t)
	svc := &chat.Service{St: st}
	acc, err := svc.Generate(ctx, "docker")
	if err != nil {
		t.Fatal(err)
	}
	h, _ := svc.History(ctx)
	last := h.Messages[len(h.Messages)-1]
	if last.Kind != "request" || last.Role != "user" || last.Text != "You asked Claude for your first 30 Docker questions." || last.JobID == nil || *last.JobID != acc.JobID {
		t.Fatalf("message %+v", last)
	}
	j, _ := st.Job(ctx, acc.JobID)
	if !strings.HasPrefix(j.Request.Text, "Write my first questions for Docker") || !j.Request.Generate {
		t.Fatalf("Claude still gets the fixed instruction: %+v", j.Request)
	}
	if v, _ := svc.Job(ctx, acc.JobID); v.Topic != "docker" {
		t.Fatalf("job view %+v", v)
	}
	storetest.AddQuestions(t, st, "docker", "compose", 5, 2)
	if _, err := svc.Generate(ctx, "docker"); err != nil {
		t.Fatal(err)
	}
	h, _ = svc.History(ctx)
	if last := h.Messages[len(h.Messages)-1]; last.Text != "You asked Claude for 15 new Docker questions at your level (Elo 17)." || last.Kind != "request" {
		t.Fatalf("message %+v", last)
	}
}

func TestSendStoresTypedMessage(t *testing.T) {
	st := storetest.New(t)
	svc := &chat.Service{St: st}
	if _, err := svc.Send(ctx, "why is B wrong?", store.PageContext{Page: "home"}); err != nil {
		t.Fatal(err)
	}
	h, _ := svc.History(ctx)
	if m := h.Messages[0]; m.Kind != "message" || m.Failed || m.Text != "why is B wrong?" {
		t.Fatalf("message %+v", m)
	}
}

func TestRetryQueuesAgain(t *testing.T) {
	st := storetest.New(t)
	svc := &chat.Service{St: st}
	f := newFakeRunner(t, "exit:1")
	acc, _ := svc.Send(ctx, "hello", store.PageContext{Page: "home"})
	_, _ = worker(st, f).ProcessOne(ctx)
	h, _ := svc.History(ctx)
	if last := h.Messages[len(h.Messages)-1]; !last.Failed || last.Role != "assistant" || last.JobID == nil || *last.JobID != acc.JobID {
		t.Fatalf("failure %+v", last)
	}
	again, err := svc.Retry(ctx, acc.JobID)
	if err != nil || again.JobID == acc.JobID {
		t.Fatalf("retry %+v %v", again, err)
	}
	if _, err := svc.Retry(ctx, again.JobID); !errors.Is(err, store.ErrNotRetryable) {
		t.Fatalf("retrying a queued job: %v", err)
	}
}

func TestSystemPromptRules(t *testing.T) {
	for _, want := range []string{
		"Never write mistake IDs such as m12 in the reply text; refer to a mistake by its wording.",
		"Don't put \"(Pick all that apply.)\" or similar in a stem; the app shows that instruction.",
	} {
		if !strings.Contains(chat.System(), want) {
			t.Fatalf("system prompt is missing %q", want)
		}
	}
}

// Final review: a typed request sent from a topic page may write questions
// anywhere, so only a button press names the topic its questions went to.
func TestJobTopicOnlyForButtonPresses(t *testing.T) {
	st := storetest.New(t)
	svc := &chat.Service{St: st}
	typed, _ := svc.Send(ctx, "Write 10 questions on Kubernetes Services", store.PageContext{Page: "topic", Topic: "docker"})
	if v, _ := svc.Job(ctx, typed.JobID); v.Topic != "" {
		t.Fatalf("typed request names topic %q", v.Topic)
	}
	pressed, _ := svc.Generate(ctx, "docker")
	if v, _ := svc.Job(ctx, pressed.JobID); v.Topic != "docker" {
		t.Fatalf("button press names topic %q, want docker", v.Topic)
	}
}

// Claude refused "Scuba Diving Safety" because the prompt framed Quizroom as
// Docker and Kubernetes only. Any topic the learner asks for is in scope.
func TestSystemPromptIsTopicNeutral(t *testing.T) {
	p := chat.System()
	// "studying for interviews" was one learner's reason; anyone can install Quizroom.
	for _, banned := range []string{"preparing for Docker and Kubernetes interviews", "\n- Facts current for Kubernetes 1.37 and Docker Engine 29.", "studying for interviews"} {
		if strings.Contains(p, banned) {
			t.Fatalf("the prompt still carries one learner's setup: %q", banned)
		}
	}
	for _, want := range []string{
		"on any subject they choose",
		"Any subject is fine, technical or not.",
		"For Docker and Kubernetes questions, facts must be current for Kubernetes 1.37 and Docker Engine 29.",
		"## Safety-critical topics",
		"supports, but doesn't replace, certified training",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("the prompt is missing %q", want)
		}
	}
}

// Final review I-5: Claude never shows the 1–10 scale, and the level it is
// told matches the badge the learner sees.
func TestPromptKeepsTheScaleInternal(t *testing.T) {
	if !strings.Contains(chat.System(), "never write skill or difficulty numbers in reply") {
		t.Fatal("the prompt must keep 1–10 numbers out of replies")
	}
	st, url := storetest.Open(t)
	docker, _ := st.TopicBySlug(ctx, "docker")
	if _, err := st.Rating(ctx, docker.ID); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	// raw skill 3.97: Elo 99, Beginner, though the rounded skill is 4.0
	if _, err := conn.Exec(ctx, `UPDATE ratings SET theta = $1 WHERE topic_id = $2`, rating.ThetaFromSkill(3.97), docker.ID); err != nil {
		t.Fatal(err)
	}
	snap, _ := chat.BuildSnapshot(ctx, st, store.PageContext{Page: "home"})
	for _, tc := range snap.Topics {
		if tc.Topic.Slug == "docker" && tc.Level != "Beginner" {
			t.Fatalf("Claude is told %q while the badge shows Beginner (Elo %d)", tc.Level, rating.Elo(rating.ThetaFromSkill(3.97)))
		}
	}
}
