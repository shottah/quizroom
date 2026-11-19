package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"

	"quizroom/internal/api"
	"quizroom/internal/chat"
	"quizroom/internal/quiz"
	"quizroom/internal/store"
	"quizroom/internal/store/storetest"
)

func TestOpenAPIMatchesFrontendCopy(t *testing.T) {
	got, err := api.OpenAPIJSON()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile("../../../frontend/src/api/openapi.json")
	if err != nil || !bytes.Equal(got, saved) {
		t.Fatalf("frontend types are stale: run make types (%v)", err)
	}
	if bytes.Contains(got, []byte(`"$schema"`)) {
		t.Fatal("response bodies must not carry $schema links")
	}
}

func newAPI(t *testing.T) (humatest.TestAPI, *store.Store) {
	t.Helper()
	st := storetest.New(t)
	_, tapi := humatest.New(t, api.Config())
	api.Register(tapi, api.Deps{
		Quiz: &quiz.Service{St: st}, Chat: &chat.Service{St: st}, Store: st,
		Runner: &chat.Client{BaseURL: "http://127.0.0.1:1"},
	})
	return tapi, st
}

func decode[T any](t *testing.T, body *bytes.Buffer) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body.Bytes(), &v); err != nil {
		t.Fatalf("%v: %s", err, body.String())
	}
	return v
}

func TestQuizFlowOverHTTP(t *testing.T) {
	tapi, st := newAPI(t)
	storetest.AddQuestions(t, st, "docker", "compose", 10, 2)
	resp := tapi.Post("/api/quizzes", map[string]any{"topic": "docker"})
	if resp.Code != 200 {
		t.Fatalf("%d %s", resp.Code, resp.Body)
	}
	started := decode[quiz.Started](t, resp.Body)
	if started.Length != 10 {
		t.Fatalf("%+v", started)
	}
	path := "/api/quizzes/" + itoa(started.QuizID)
	shown := decode[quiz.Shown](t, tapi.Get(path+"/next").Body)
	if shown.Question == nil || len(shown.Question.Options) != 4 {
		t.Fatalf("%+v", shown)
	}
	resp = tapi.Post(path+"/answers", map[string]any{"position": 1, "chosen": []int{0}})
	res := decode[quiz.Result](t, resp.Body)
	if resp.Code != 200 || len(res.CorrectIDs) != 1 || !res.Correct {
		t.Fatalf("%d %s", resp.Code, resp.Body)
	}
	sum := decode[quiz.Summary](t, tapi.Get(path).Body)
	if sum.Answered != 1 || sum.TopicSlug != "docker" {
		t.Fatalf("%+v", sum)
	}
	if resp := tapi.Post(path+"/answers", map[string]any{"position": 2, "chosen": []int{}}); resp.Code != 404 && resp.Code != 422 {
		t.Fatalf("answering an unshown position: %d", resp.Code)
	}
	if resp := tapi.Post("/api/questions/" + itoa(shown.Question.ID) + "/flag"); resp.Code != 204 {
		t.Fatalf("flag: %d %s", resp.Code, resp.Body)
	}
}

func itoa(n int64) string { return strings.TrimSpace(string(must(json.Marshal(n)))) }

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestStartQuizNotEnough(t *testing.T) {
	tapi, _ := newAPI(t)
	resp := tapi.Post("/api/quizzes", map[string]any{"topic": "docker"})
	if resp.Code != 409 || !strings.Contains(resp.Body.String(), "Write 15 at your level") {
		t.Fatalf("%d %s", resp.Code, resp.Body)
	}
	if resp := tapi.Post("/api/quizzes", map[string]any{"topic": "nope"}); resp.Code != 404 {
		t.Fatalf("unknown topic: %d", resp.Code)
	}
}

func TestTopicsEmptyState(t *testing.T) {
	tapi, _ := newAPI(t)
	resp := tapi.Get("/api/topics")
	cards := decode[[]quiz.Card](t, resp.Body)
	if resp.Code != 200 || len(cards) != 2 || !strings.Contains(resp.Body.String(), `"spark":[]`) {
		t.Fatalf("%d %s", resp.Code, resp.Body)
	}
	for _, c := range cards {
		if !c.Empty {
			t.Fatalf("%+v", c)
		}
	}
	detail := decode[quiz.Detail](t, tapi.Get("/api/topics/kubernetes").Body)
	if len(detail.Subtopics) != 10 || detail.History == nil {
		t.Fatalf("%+v", detail)
	}
	if resp := tapi.Get("/api/topics/kubernetes/summary"); resp.Code != 200 || !strings.Contains(resp.Body.String(), `"active_mistakes"`) {
		t.Fatalf("summary %d %s", resp.Code, resp.Body)
	}
}

func TestSettingsValidation(t *testing.T) {
	tapi, _ := newAPI(t)
	if resp := tapi.Put("/api/settings", map[string]any{"target": 0.5, "model": "sonnet"}); resp.Code != 422 {
		t.Fatalf("target 0.5: %d %s", resp.Code, resp.Body)
	}
	if resp := tapi.Put("/api/settings", map[string]any{"target": 0.65, "model": "opus"}); resp.Code != 200 {
		t.Fatalf("%d %s", resp.Code, resp.Body)
	}
	s := decode[store.Settings](t, tapi.Get("/api/settings").Body)
	if s != (store.Settings{Target: 0.65, Model: "opus"}) {
		t.Fatalf("%+v", s)
	}
}

func TestStatusRunnerDown(t *testing.T) {
	tapi, _ := newAPI(t)
	resp := tapi.Get("/api/status")
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), `"auth":"unreachable"`) || !strings.Contains(resp.Body.String(), `"topics":2`) {
		t.Fatalf("%d %s", resp.Code, resp.Body)
	}
}

func TestAddMistakeFromMock(t *testing.T) {
	tapi, _ := newAPI(t)
	body := map[string]any{"topic": "kubernetes", "subtopic": "workloads", "belief": "A Deployment talks to the kubelet directly",
		"correction": "Controllers write desired state to the API server", "source": "mock"}
	a := decode[quiz.MistakeView](t, tapi.Post("/api/mistakes", body).Body)
	b := decode[quiz.MistakeView](t, tapi.Post("/api/mistakes", body).Body)
	if a.Status != "active" || a.ID == 0 || b.ID != a.ID || b.Hits != 1 {
		t.Fatalf("%+v %+v", a, b)
	}
	if resp := tapi.Get("/api/mistakes?topic=kubernetes"); resp.Code != 200 || len(decode[[]quiz.MistakeView](t, resp.Body)) != 8 {
		t.Fatalf("%d %s", resp.Code, resp.Body)
	}
	bad := map[string]any{"topic": "kubernetes", "subtopic": "workloads", "belief": "x", "correction": "y", "source": "seed"}
	if resp := tapi.Post("/api/mistakes", bad); resp.Code != 422 {
		t.Fatalf("source seed: %d", resp.Code)
	}
}

func TestChatOverHTTP(t *testing.T) {
	tapi, _ := newAPI(t)
	page := map[string]any{"page": "home", "topic": "", "question_id": 0, "chosen": []int{}}
	resp := tapi.Post("/api/chat", map[string]any{"text": "quiz me on compose", "page": page})
	acc := decode[chat.Accepted](t, resp.Body)
	if resp.Code != 200 || acc.JobID == 0 {
		t.Fatalf("%d %s", resp.Code, resp.Body)
	}
	h := decode[chat.History](t, tapi.Get("/api/chat").Body)
	if len(h.Messages) != 1 || h.PendingJob == nil || *h.PendingJob != acc.JobID {
		t.Fatalf("%+v", h)
	}
	job := decode[chat.JobView](t, tapi.Get("/api/jobs/"+itoa(acc.JobID)).Body)
	if job.Status != "queued" {
		t.Fatalf("%+v", job)
	}
	if resp := tapi.Post("/api/chat", map[string]any{"text": " ", "page": page}); resp.Code != 422 {
		t.Fatalf("blank text: %d", resp.Code)
	}
	if resp := tapi.Post("/api/topics/docker/generate"); resp.Code != 200 {
		t.Fatalf("generate: %d %s", resp.Code, resp.Body)
	}
	if resp := tapi.Get("/api/jobs/999999"); resp.Code != http.StatusNotFound {
		t.Fatalf("unknown job: %d", resp.Code)
	}
}

// Final review #5: the last failed job's error shows in the status.
func TestStatusShowsLastError(t *testing.T) {
	tapi, st := newAPI(t)
	id, _ := st.CreateChatJob(context.Background(), "hi", "message", store.JobRequest{Text: "hi"})
	_ = st.FailJob(context.Background(), id, "Claude couldn't sign in. Run `make token` in quizroom/, then try again.")
	resp := tapi.Get("/api/status")
	if !strings.Contains(resp.Body.String(), `"last_error":"Claude couldn't sign in.`) {
		t.Fatalf("%s", resp.Body)
	}
}

func TestRetryOverHTTP(t *testing.T) {
	tapi, st := newAPI(t)
	ctx := context.Background()
	id, _ := st.CreateChatJob(ctx, "hi", "message", store.JobRequest{Text: "hi"})
	if resp := tapi.Post("/api/jobs/" + itoa(id) + "/retry"); resp.Code != http.StatusConflict {
		t.Fatalf("retrying a queued job: %d %s", resp.Code, resp.Body)
	}
	_, _ = st.ClaimJob(ctx)
	_ = st.FailJob(ctx, id, "Claude stopped")
	resp := tapi.Post("/api/jobs/" + itoa(id) + "/retry")
	if resp.Code != http.StatusOK {
		t.Fatalf("retrying a failed job: %d %s", resp.Code, resp.Body)
	}
	if acc := decode[chat.Accepted](t, resp.Body); acc.JobID == 0 || acc.JobID == id {
		t.Fatalf("%+v", acc)
	}
	if resp := tapi.Post("/api/jobs/999999/retry"); resp.Code != http.StatusNotFound {
		t.Fatalf("unknown job: %d", resp.Code)
	}
}

func TestStartCheckOverHTTP(t *testing.T) {
	tapi, st := newAPI(t)
	storetest.AddQuestions(t, st, "docker", "compose", 19, 2)
	if resp := tapi.Post("/api/quizzes", map[string]any{"topic": "docker", "kind": "check"}); resp.Code != http.StatusConflict ||
		!strings.Contains(resp.Body.String(), "needs 20") {
		t.Fatalf("19 available: %d %s", resp.Code, resp.Body)
	}
	storetest.AddQuestions(t, st, "docker", "compose", 26, 2)
	resp := tapi.Post("/api/quizzes", map[string]any{"topic": "docker", "kind": "check"})
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"kind":"check"`) {
		t.Fatalf("45 available: %d %s", resp.Code, resp.Body)
	}
	if resp := tapi.Post("/api/quizzes", map[string]any{"topic": "docker", "kind": "exam"}); resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad kind: %d", resp.Code)
	}
	if resp := tapi.Post("/api/quizzes", map[string]any{"topic": "docker"}); resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"kind":"practice"`) {
		t.Fatalf("default kind: %d %s", resp.Code, resp.Body)
	}
}
