package runner

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClaude writes a `claude` script whose run behaviour is body (sh).
func fakeClaude(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"2.1.327 (Claude Code)\"; exit 0; fi\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir + ":/usr/bin:/bin"
}

const echoBody = `cat
echo
echo "KEY=${ANTHROPIC_API_KEY:+set}"
echo "TOKEN=${CLAUDE_CODE_OAUTH_TOKEN:+set}"`

func post(h http.Handler, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/claude", strings.NewReader(body)))
	return rec
}

const reqBody = `{"system":"s","prompt":"PROMPT-TEXT","schema":{"type":"object"},"model":"sonnet"}`

func TestServerHealth(t *testing.T) {
	s := &Server{Env: map[string]string{"PATH": fakeClaude(t, echoBody), "CLAUDE_CODE_OAUTH_TOKEN": "tok"}}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	var h Health
	_ = json.Unmarshal(rec.Body.Bytes(), &h)
	if rec.Code != 200 || !h.OK || h.Claude != "2.1.327 (Claude Code)" || h.Auth != "subscription" {
		t.Fatalf("health %d %s", rec.Code, rec.Body.String())
	}
	s.Env["CLAUDE_CODE_OAUTH_TOKEN"] = ""
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &h)
	if h.Auth != "none" {
		t.Fatalf("auth %q, want none", h.Auth)
	}
}

func TestServerRunsClaude(t *testing.T) {
	s := &Server{Env: map[string]string{"PATH": fakeClaude(t, echoBody), "CLAUDE_CODE_OAUTH_TOKEN": "tok"}}
	rec := post(s.Handler(), reqBody)
	var r Response
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil || rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if r.ExitCode != 0 || !strings.Contains(r.Stdout, "PROMPT-TEXT") || !strings.Contains(r.Stdout, "TOKEN=set") || strings.Contains(r.Stdout, "KEY=set") {
		t.Fatalf("response %+v", r)
	}
}

func TestServer412WithoutCredential(t *testing.T) {
	s := &Server{Env: map[string]string{"PATH": fakeClaude(t, echoBody)}}
	rec := post(s.Handler(), reqBody)
	if rec.Code != http.StatusPreconditionFailed || !strings.Contains(rec.Body.String(), `"error":"not_signed_in"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestServerBusy409(t *testing.T) {
	s := &Server{Env: map[string]string{"PATH": fakeClaude(t, "sleep 2; cat"), "CLAUDE_CODE_OAUTH_TOKEN": "tok"}}
	h := s.Handler()
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			time.Sleep(time.Duration(i) * 300 * time.Millisecond)
			codes[i] = post(h, reqBody).Code
		}(i)
	}
	wg.Wait()
	if codes[0] != 200 || codes[1] != http.StatusConflict {
		t.Fatalf("codes %v, want [200 409]", codes)
	}
}

func TestServerTimeoutStopsProcess(t *testing.T) {
	s := &Server{Env: map[string]string{"PATH": fakeClaude(t, "trap 'exit 130' INT\nsleep 60 &\nwait $!"), "CLAUDE_CODE_OAUTH_TOKEN": "tok"}}
	start := time.Now()
	rec := post(s.Handler(), `{"prompt":"hi","schema":{},"model":"sonnet","timeout_s":1}`)
	var r Response
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("took %v; a stuck call must stop at its timeout", took)
	}
	if rec.Code != 200 || r.ExitCode == 0 {
		t.Fatalf("%d %+v", rec.Code, r)
	}
}

func TestServerRejectsBadRequest(t *testing.T) {
	s := &Server{Env: map[string]string{"PATH": fakeClaude(t, echoBody), "CLAUDE_CODE_OAUTH_TOKEN": "tok"}}
	rec := post(s.Handler(), `{"prompt":"","schema":{},"model":"sonnet"}`)
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"bad_request"`)) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
