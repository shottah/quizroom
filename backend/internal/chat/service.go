package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"quizroom/internal/rating"
	"quizroom/internal/store"
)

// MaxText is the longest chat message accepted.
const MaxText = 4000

// ErrBadText is a blank or over-long chat message.
var ErrBadText = errors.New("write a message of 1 to 4000 characters")

// Fixed messages for the generate buttons (quizroom.md, "The chat bubble").
const (
	firstText    = "Write my first questions for %s: 15 questions spread across difficulties 1–6."
	firstFollow  = "Write 15 more questions for %s, spread across difficulties 1–6, different from the ones you just wrote."
	moreText     = "Write 15 questions for %s around my skill, favouring subtopics with few questions near my level and mistakes that need questions."
	historyLimit = 50

	// What the chat shows for those button presses.
	firstShown = "You asked Claude for your first 30 %s questions."
	moreShown  = "You asked Claude for 15 new %s questions at your level (Elo %d)."
)

// Service is the chat's API side: queueing messages and reading results.
type Service struct{ St *store.Store }

type Accepted struct {
	JobID int64 `json:"job_id"`
}

type Message struct {
	Role   string    `json:"role" enum:"user,assistant"`
	Kind   string    `json:"kind" enum:"message,request" doc:"request: a button press such as Write 15, shown as a system card"`
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
	JobID  *int64    `json:"job_id" doc:"the job this message asked for or answered"`
	Failed bool      `json:"failed" doc:"Claude's message about a job that failed; the job can be retried"`
}

type History struct {
	Messages   []Message `json:"messages"`
	PendingJob *int64    `json:"pending_job" doc:"a job that is still queued or running, if any"`
}

type JobView struct {
	ID      int64    `json:"id"`
	Topic   string   `json:"topic" doc:"slug of the topic a button press asked Claude to write for; empty for typed messages"`
	Status  string   `json:"status" enum:"queued,running,done,failed"`
	Error   string   `json:"error"`
	Reply   string   `json:"reply" doc:"the assistant's message once the job is done"`
	Added   int      `json:"added"`
	Dropped []string `json:"dropped"`
}

// Send queues a chat message.
func (s *Service) Send(ctx context.Context, text string, page store.PageContext) (Accepted, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > MaxText {
		return Accepted{}, ErrBadText
	}
	id, err := s.St.CreateChatJob(ctx, text, "message", store.JobRequest{Text: text, Page: page})
	return Accepted{JobID: id}, err
}

// Generate queues the fixed "write questions" message for a topic: the first
// 30 (two calls of 15) when it has none, otherwise 15 more around the skill.
func (s *Service) Generate(ctx context.Context, topicSlug string) (Accepted, error) {
	t, err := s.St.TopicBySlug(ctx, topicSlug)
	if err != nil {
		return Accepted{}, err
	}
	n, err := s.St.CountQuestions(ctx, t.ID)
	if err != nil {
		return Accepted{}, err
	}
	req := store.JobRequest{Page: store.PageContext{Page: "topic", Topic: t.Slug}, Generate: true}
	var display string
	if n == 0 {
		req.Text, req.Followup = fmt.Sprintf(firstText, t.Name), fmt.Sprintf(firstFollow, t.Name)
		display = fmt.Sprintf(firstShown, t.Name)
	} else {
		r, err := s.St.Rating(ctx, t.ID)
		if err != nil {
			return Accepted{}, err
		}
		req.Text = fmt.Sprintf(moreText, t.Name)
		display = fmt.Sprintf(moreShown, t.Name, rating.Elo(r.Theta))
	}
	if pending, err := s.St.PendingJobWithText(ctx, req.Text); err != nil || pending != nil {
		if pending != nil {
			return Accepted{JobID: *pending}, nil
		}
		return Accepted{}, err
	}
	id, err := s.St.CreateChatJob(ctx, display, "request", req)
	return Accepted{JobID: id}, err
}

// Retry queues a failed job's request again.
func (s *Service) Retry(ctx context.Context, id int64) (Accepted, error) {
	newID, err := s.St.RetryJob(ctx, id)
	return Accepted{JobID: newID}, err
}

// History is the recent conversation and any job still in progress.
func (s *Service) History(ctx context.Context) (History, error) {
	msgs, err := s.St.ChatHistory(ctx, historyLimit)
	if err != nil {
		return History{}, err
	}
	h := History{Messages: []Message{}}
	for _, m := range msgs {
		h.Messages = append(h.Messages, Message{Role: m.Role, Kind: m.Kind, Text: m.Text, At: m.CreatedAt, JobID: m.JobID, Failed: m.Failed})
	}
	h.PendingJob, err = s.St.PendingJob(ctx)
	return h, err
}

// Job reports a job's progress and result.
func (s *Service) Job(ctx context.Context, id int64) (JobView, error) {
	j, err := s.St.Job(ctx, id)
	if err != nil {
		return JobView{}, err
	}
	v := JobView{ID: j.ID, Status: j.Status, Error: j.Error, Dropped: []string{}}
	if j.Request.Generate {
		v.Topic = j.Request.Page.Topic
	}
	if j.Result != nil {
		v.Reply, v.Added = j.Result.Reply, j.Result.Added
		if j.Result.Dropped != nil {
			v.Dropped = j.Result.Dropped
		}
	}
	return v, nil
}
