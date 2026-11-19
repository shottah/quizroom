package chat

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"quizroom/internal/format"
	"quizroom/internal/runner"
	"quizroom/internal/store"
)

// User-facing failure messages.
const (
	MsgNotSignedIn = "Claude isn't signed in, so nothing was sent. Run `make token` in quizroom/ (it restarts the runner), then press Retry."
	MsgAuthFailed  = "Claude couldn't sign in with the saved credential (%s), so nothing was sent. Run `make token` in quizroom/, then press Retry."
	MsgUnavailable = "Can't reach Claude's runner (a container in the Quizroom stack). Run `make doctor` in quizroom/, then press Retry."
	FollowupNew    = "Write 15 more questions for %s, spread across difficulties 1–6, different from the ones you just wrote."
)

// Worker processes chat jobs one at a time.
type Worker struct {
	St     *store.Store
	Runner *Client
	Poll   time.Duration
	Sleep  func(time.Duration)
}

func (w *Worker) sleep(d time.Duration) {
	if w.Sleep != nil {
		w.Sleep(d)
		return
	}
	time.Sleep(d)
}

// Run fails jobs a restart interrupted, then processes jobs until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	if n, err := w.St.RecoverJobs(ctx); err != nil {
		log.Printf("recover jobs: %v", err)
	} else if n > 0 {
		log.Printf("failed %d jobs interrupted by a restart", n)
	}
	poll := w.Poll
	if poll == 0 {
		poll = time.Second
	}
	for ctx.Err() == nil {
		did, err := w.ProcessOne(ctx)
		if err != nil {
			log.Printf("chat job: %v", err)
		}
		if !did {
			select {
			case <-ctx.Done():
			case <-time.After(poll):
			}
		}
	}
}

// authError is a credential Claude rejected: expired, revoked or mistyped.
type authError struct{ detail string }

func (e authError) Error() string { return fmt.Sprintf(MsgAuthFailed, e.detail) }

var authHints = []string{"invalid api key", "oauth", "authentication", "unauthorized", "not logged in",
	"please run /login", "401", "invalid x-api-key", "credential"}

func looksLikeAuth(msg string) bool {
	m := strings.ToLower(msg)
	for _, h := range authHints {
		if strings.Contains(m, h) {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if len(line) > 200 {
		line = line[:200]
	}
	return line
}

type exitError struct{ code int }

func (e exitError) Error() string {
	return fmt.Sprintf("Claude stopped before finishing (exit code %d), so nothing was added. Press Retry. If it keeps happening, run `make logs` in quizroom/.", e.code)
}

func userMessage(err error) string {
	var ce ClaudeError
	var ee exitError
	switch {
	case errors.Is(err, ErrNotSignedIn):
		return MsgNotSignedIn
	case errors.Is(err, ErrUnavailable):
		return MsgUnavailable
	case errors.As(err, &ce) && looksLikeAuth(ce.Message):
		return authError{detail: firstLine(ce.Message)}.Error()
	case errors.Is(err, ErrFormat), errors.As(err, &ce), errors.As(err, &ee):
		return err.Error()
	}
	return "Something went wrong: " + err.Error()
}

type callResult struct {
	reply    string
	added    []store.Question
	dropped  []format.Dropped
	topic    *format.NewTopic
	mistakes []format.NewMistake
	cost     float64
}

// ProcessOne claims and processes one job. It returns false when there was none.
func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	job, err := w.St.ClaimJob(ctx)
	if err != nil || job == nil {
		return false, err
	}
	history, err := w.St.ChatHistoryBefore(ctx, job.ID, 10)
	if err != nil {
		return true, w.St.FailJob(ctx, job.ID, userMessage(err))
	}
	first, err := w.call(ctx, job, history, job.Request.Text)
	if errors.Is(err, ErrBusy) {
		if rerr := w.St.RequeueJob(ctx, job.ID); rerr != nil {
			return true, rerr
		}
		w.sleep(5 * time.Second)
		return true, nil
	}
	if err != nil {
		return true, w.St.FailJob(ctx, job.ID, userMessage(err))
	}
	results := []callResult{first}

	followup := job.Request.Followup
	if followup == "" && first.topic != nil {
		followup = fmt.Sprintf(FollowupNew, first.topic.Name)
	}
	var secondErr error
	if followup != "" {
		history = append(history,
			store.ChatMessage{Role: "user", Text: job.Request.Text},
			store.ChatMessage{Role: "assistant", Text: first.reply})
		second, err := w.call(ctx, job, history, followup)
		if err != nil {
			secondErr = err
		} else {
			results = append(results, second)
		}
	}
	text, res, err := w.compose(ctx, results, secondErr)
	if err != nil {
		return true, w.St.FailJob(ctx, job.ID, userMessage(err))
	}
	return true, w.St.FinishJob(ctx, job.ID, res, text)
}

func (w *Worker) call(ctx context.Context, job *store.Job, history []store.ChatMessage, text string) (callResult, error) {
	snap, err := BuildSnapshot(ctx, w.St, job.Request.Page)
	if err != nil {
		return callResult{}, err
	}
	settings, err := w.St.Settings(ctx)
	if err != nil {
		return callResult{}, err
	}
	schema, err := format.ReplySchema()
	if err != nil {
		return callResult{}, err
	}
	resp, err := w.Runner.Call(ctx, runner.Request{System: System(), Prompt: BuildPrompt(snap, history, text),
		Schema: schema, Model: settings.Model, TimeoutS: runner.DefaultTimeoutS})
	if err != nil {
		return callResult{}, err
	}
	reply, cost, perr := ParseOutput(resp.Stdout)
	if resp.ExitCode != 0 {
		var ce ClaudeError
		if errors.As(perr, &ce) {
			return callResult{}, ce
		}
		if looksLikeAuth(resp.Stderr) {
			return callResult{}, ClaudeError{Message: firstLine(resp.Stderr)}
		}
		return callResult{}, exitError{resp.ExitCode}
	}
	if perr != nil {
		return callResult{}, perr
	}
	known, err := w.St.Known(ctx)
	if err != nil {
		return callResult{}, err
	}
	clean := format.Validate(reply, known)
	added, dropped, err := w.St.ApplyReply(ctx, job.ID, clean, time.Now().UTC())
	if err != nil {
		return callResult{}, err
	}
	return callResult{reply: strings.TrimSpace(clean.Reply), added: added, dropped: append(clean.Dropped, dropped...),
		topic: clean.Topic, mistakes: clean.Mistakes, cost: cost}, nil
}

func (w *Worker) compose(ctx context.Context, results []callResult, secondErr error) (string, store.JobResult, error) {
	tops, err := w.St.Topics(ctx)
	if err != nil {
		return "", store.JobResult{}, err
	}
	topics := map[int64]string{}
	subtopics := map[int64]string{}
	for _, t := range tops {
		topics[t.ID] = t.Name
		subs, err := w.St.Subtopics(ctx, t.ID)
		if err != nil {
			return "", store.JobResult{}, err
		}
		for _, s := range subs {
			subtopics[s.ID] = s.Name
		}
	}
	var replies, lines []string
	var added []store.Question
	var dropped []format.Dropped
	res := store.JobResult{Dropped: []string{}}
	for _, r := range results {
		if r.reply != "" {
			replies = append(replies, r.reply)
		}
		if r.topic != nil {
			lines = append(lines, fmt.Sprintf("Created topic %s with %d subtopics.", r.topic.Name, len(r.topic.Subtopics)))
		}
		for _, m := range r.mistakes {
			if m.FromUser {
				lines = append(lines, fmt.Sprintf("Added mistake: “%s”.", m.Belief))
			}
		}
		added = append(added, r.added...)
		dropped = append(dropped, r.dropped...)
		res.CostUSD += r.cost
	}
	if s := Summarize(added, topics, subtopics, dropped); s != "" {
		lines = append(lines, s)
	}
	if secondErr != nil {
		lines = append(lines, "The second batch didn't finish: "+userMessage(secondErr))
	}
	res.Added = len(added)
	for _, d := range dropped {
		res.Dropped = append(res.Dropped, d.What+": "+d.Reason)
	}
	text := strings.Join(replies, "\n\n")
	if len(lines) > 0 {
		if text != "" {
			text += "\n\n"
		}
		text += strings.Join(lines, "\n")
	}
	if text == "" {
		text = "Done."
	}
	res.Reply = text
	return text, res, nil
}

// Summarize is the "Added …" line shown under Claude's reply.
func Summarize(added []store.Question, topics, subtopics map[int64]string, dropped []format.Dropped) string {
	if len(added) == 0 && len(dropped) == 0 {
		return ""
	}
	noun := "questions"
	if len(added) == 1 {
		noun = "question"
	}
	line := fmt.Sprintf("Added %d %s", len(added), noun)
	if len(added) > 0 {
		oneTopic, oneSub := true, true
		for _, q := range added[1:] {
			oneTopic = oneTopic && q.TopicID == added[0].TopicID
			oneSub = oneSub && q.SubtopicID == added[0].SubtopicID
		}
		switch {
		case oneTopic && oneSub:
			line += fmt.Sprintf(" to %s › %s", topics[added[0].TopicID], subtopics[added[0].SubtopicID])
		case oneTopic:
			line += " to " + topics[added[0].TopicID]
		}
	}
	if len(dropped) > 0 {
		var reasons []string
		seen := map[string]bool{}
		for _, d := range dropped {
			if !seen[d.Reason] {
				seen[d.Reason] = true
				reasons = append(reasons, d.Reason)
			}
		}
		line += fmt.Sprintf("; dropped %d: %s", len(dropped), strings.Join(reasons, "; "))
	}
	return line + "."
}
