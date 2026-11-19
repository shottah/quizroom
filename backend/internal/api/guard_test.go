package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"quizroom/internal/api"
	"quizroom/internal/chat"
	"quizroom/internal/quiz"
	"quizroom/internal/store/storetest"
)

// through sends one request through Guard and reports its status and whether
// it reached the handler behind it.
func through(t *testing.T, extra []string, method, host string, headers map[string]string) (int, bool) {
	t.Helper()
	reached := false
	h := api.Guard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }), extra)
	req := httptest.NewRequest(method, "/api/topics", nil)
	req.Host = host
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, reached
}

// A web page can point its own domain at 127.0.0.1 (DNS rebinding) and then
// read and change everything. Only requests addressed to this computer pass.
func TestGuardOnlyAnswersThisComputer(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1:8790": true, "localhost": true, "LocalHost:8790": true, "[::1]:8790": true,
		"evil.example": false, "evil.example:8790": false, "127.0.0.1.evil.example": false, "": false,
	} {
		code, reached := through(t, nil, http.MethodGet, host, nil)
		if reached != want || (!want && code != http.StatusForbidden) {
			t.Errorf("Host %q: reached %v with status %d, want reached %v", host, reached, code, want)
		}
	}
}

// A form or script on another website can send requests to 127.0.0.1, for
// example to start a Claude job. Browsers say where a request came from.
func TestGuardRefusesOtherWebsites(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{"the app itself", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://127.0.0.1:8790"}, true},
		{"curl or an AI agent", nil, true},
		{"the address bar", map[string]string{"Sec-Fetch-Site": "none"}, true},
		{"another website", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}, false},
		{"another app on this computer", map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "http://127.0.0.1:3000"}, false},
		{"an older browser on another website", map[string]string{"Origin": "https://evil.example"}, false},
		{"a sandboxed page", map[string]string{"Origin": "null"}, false},
	} {
		code, reached := through(t, nil, http.MethodPost, "127.0.0.1:8790", tc.headers)
		if reached != tc.want || (!tc.want && code != http.StatusForbidden) {
			t.Errorf("%s: reached %v with status %d, want reached %v", tc.name, reached, code, tc.want)
		}
	}
}

// ALLOWED_HOSTS lets Quizroom answer under another name, for example behind
// your own authenticating proxy.
func TestGuardAcceptsAllowedHosts(t *testing.T) {
	extra := api.Hosts(" Quizroom.Example.com, ,study.example ")
	if !slices.Equal(extra, []string{"quizroom.example.com", "study.example"}) {
		t.Fatalf("ALLOWED_HOSTS parsed as %q", extra)
	}
	if _, ok := through(t, extra, http.MethodPost, "quizroom.example.com", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://quizroom.example.com"}); !ok {
		t.Error("a host in ALLOWED_HOSTS was refused")
	}
	if _, ok := through(t, extra, http.MethodGet, "other.example", nil); ok {
		t.Error("a host outside ALLOWED_HOSTS got through")
	}
}

// The real handler is guarded: a form on another website can't start a Claude
// job, and the app's own request still can.
func TestFormOnAnotherWebsiteCantStartAClaudeJob(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	h := api.NewHandler(api.Deps{
		Quiz: &quiz.Service{St: st}, Chat: &chat.Service{St: st}, Store: st,
		Runner: &chat.Client{BaseURL: "http://127.0.0.1:1"},
	}, nil)
	post := func(headers map[string]string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/topics/docker/generate", strings.NewReader(""))
		req.Host = "127.0.0.1:8790"
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	form := map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example", "Content-Type": "application/x-www-form-urlencoded"}
	if code := post(form); code != http.StatusForbidden {
		t.Fatalf("a form on another website got status %d, want 403", code)
	}
	if job, _ := st.PendingJob(ctx); job != nil {
		t.Fatalf("the refused request queued job %d", *job)
	}
	if code := post(map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://127.0.0.1:8790"}); code != http.StatusOK {
		t.Fatalf("the app's own request got status %d, want 200", code)
	}
	if job, _ := st.PendingJob(ctx); job == nil {
		t.Fatal("the app's own request queued no job")
	}
}
