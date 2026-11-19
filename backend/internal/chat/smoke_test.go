//go:build smoke

package chat_test

// One real Claude call, run by hand:
//
//	TEST_DATABASE_URL=... go test -tags smoke -run TestSmokeRealClaude -v ./internal/chat/
//
// It runs the host's own `claude` (your normal login, no throwaway config dir)
// with exactly the runner's command line, then parses, checks and stores the
// reply in a throwaway test schema. It proves the schema and prompt work with
// the real CLI, which the fake runner in the other tests can't.

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"quizroom/internal/chat"
	"quizroom/internal/format"
	"quizroom/internal/runner"
	"quizroom/internal/store"
	"quizroom/internal/store/storetest"
)

func TestSmokeRealClaude(t *testing.T) {
	st := storetest.New(t)
	snap, err := chat.BuildSnapshot(ctx, st, store.PageContext{Page: "home"})
	if err != nil {
		t.Fatal(err)
	}
	schema, err := format.ReplySchema()
	if err != nil {
		t.Fatal(err)
	}
	req := runner.Request{
		System: chat.System(), Schema: schema, Model: "sonnet", TimeoutS: runner.DefaultTimeoutS,
		Prompt: chat.BuildPrompt(snap, nil, "Write 5 Docker questions about Compose at difficulty 3–5."),
	}
	argv := runner.Argv(req, false)
	cmd := exec.CommandContext(ctx, "claude", argv[1:]...)
	cmd.Stdin = strings.NewReader(req.Prompt)
	cmd.Dir = t.TempDir()
	started := time.Now()
	out, err := cmd.Output()
	t.Logf("claude took %s", time.Since(started).Round(time.Second))
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("claude failed: %v\nstdout: %.2000s\nstderr: %.2000s", err, out, stderr)
	}
	reply, cost, err := chat.ParseOutput(string(out))
	if err != nil {
		t.Fatalf("%v\nstdout: %.3000s", err, out)
	}
	known, err := st.Known(ctx)
	if err != nil {
		t.Fatal(err)
	}
	clean := format.Validate(reply, known)
	added, dropped, err := st.ApplyReply(ctx, 0, clean, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("cost $%.4f, reply %q", cost, clean.Reply)
	for _, d := range append(clean.Dropped, dropped...) {
		t.Logf("dropped %s: %s", d.What, d.Reason)
	}
	for _, q := range added {
		t.Logf("[d%d %s] %s", q.Difficulty, q.Kind, q.Stem)
		if q.Code != "" {
			t.Logf("    code (%s):\n%s", q.CodeLang, q.Code)
		}
		for _, o := range q.Options {
			mark := " "
			if o.Correct {
				mark = "✓"
			}
			t.Logf("    %s %s — %s", mark, o.Text, o.Explanation)
		}
	}
	if len(added) < 3 {
		t.Fatalf("only %d questions survived the checks", len(added))
	}
}
