// Package api is Quizroom's HTTP API. Huma builds the OpenAPI description
// from the Go types the handlers use, and the frontend's TypeScript types are
// generated from that description (`make types`), so no format is written twice.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"quizroom/internal/chat"
	"quizroom/internal/quiz"
	"quizroom/internal/store"
)

// Deps are the services the handlers call.
type Deps struct {
	Quiz   *quiz.Service
	Chat   *chat.Service
	Store  *store.Store
	Runner *chat.Client
}

// Config is the Huma configuration: OpenAPI at /openapi.json, no docs UI and
// no $schema links in responses.
func Config() huma.Config {
	c := huma.DefaultConfig("Quizroom API", "1.0.0")
	c.OpenAPIPath = "/openapi"
	c.DocsPath = ""
	c.SchemasPath = ""
	c.CreateHooks = nil
	return c
}

// NewHandler serves the API behind Guard. hosts are names to accept besides
// localhost and 127.0.0.1 (ALLOWED_HOSTS).
func NewHandler(d Deps, hosts []string) http.Handler {
	mux := http.NewServeMux()
	Register(humago.New(mux, Config()), d)
	return Guard(mux, hosts)
}

// OpenAPIJSON is the API description the frontend's types are generated from.
func OpenAPIJSON() ([]byte, error) {
	a := humago.New(http.NewServeMux(), Config())
	Register(a, Deps{})
	b, err := json.MarshalIndent(a.OpenAPI(), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Status reports the Claude runner and how much the app holds.
type Status struct {
	Claude    string `json:"claude" doc:"the Claude CLI version in the runner"`
	Auth      string `json:"auth" enum:"api_key,subscription,none,unreachable"`
	Topics    int    `json:"topics"`
	Questions int    `json:"questions"`
	LastError string `json:"last_error" doc:"the error of the last chat job, if it failed"`
}

type StartQuizRequest struct {
	Topic string `json:"topic" doc:"topic slug"`
	Kind  string `json:"kind,omitempty" enum:"practice,check" default:"practice" doc:"practice, or a skill check that measures you"`
}

type AnswerRequest struct {
	Position int   `json:"position" minimum:"1"`
	Chosen   []int `json:"chosen" doc:"the IDs of the chosen options"`
}

type ChatRequest struct {
	Text string            `json:"text"`
	Page store.PageContext `json:"page"`
}

type slugIn struct {
	Slug string `path:"slug"`
}

type idIn struct {
	ID int64 `path:"id"`
}

type body[T any] struct{ Body T }

func out[T any](v T, err error) (*body[T], error) {
	if err != nil {
		return nil, fail(err)
	}
	return &body[T]{Body: v}, nil
}

func fail(err error) error {
	var ne quiz.NotEnoughError
	switch {
	case errors.Is(err, store.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.As(err, &ne):
		return huma.Error409Conflict(ne.Error())
	case errors.Is(err, store.ErrNotRetryable):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, quiz.ErrBadChoice), errors.Is(err, chat.ErrBadText), errors.Is(err, quiz.ErrBadSource):
		return huma.Error422UnprocessableEntity(err.Error())
	}
	log.Printf("api: %v", err)
	return huma.Error500InternalServerError("something went wrong; see the backend logs")
}

func op(id, method, path, summary string) huma.Operation {
	return huma.Operation{OperationID: id, Method: method, Path: path, Summary: summary}
}

// Register adds every operation to the API.
func Register(a huma.API, d Deps) {
	huma.Register(a, op("list-topics", http.MethodGet, "/api/topics", "Topic cards for the home page"),
		func(ctx context.Context, _ *struct{}) (*body[[]quiz.Card], error) { return out(d.Quiz.Cards(ctx)) })

	huma.Register(a, op("get-topic", http.MethodGet, "/api/topics/{slug}", "A topic's skill history, subtopics and mistakes"),
		func(ctx context.Context, in *slugIn) (*body[quiz.Detail], error) {
			return out(d.Quiz.Detail(ctx, in.Slug))
		})

	huma.Register(a, op("get-topic-summary", http.MethodGet, "/api/topics/{slug}/summary", "Compact summary for an AI agent, for example before a mock interview"),
		func(ctx context.Context, in *slugIn) (*body[quiz.TopicSummary], error) {
			return out(d.Quiz.TopicSummary(ctx, in.Slug))
		})

	huma.Register(a, op("generate-questions", http.MethodPost, "/api/topics/{slug}/generate", "Ask Claude for more questions in a topic"),
		func(ctx context.Context, in *slugIn) (*body[chat.Accepted], error) {
			return out(d.Chat.Generate(ctx, in.Slug))
		})

	huma.Register(a, op("start-quiz", http.MethodPost, "/api/quizzes", "Start a quiz"),
		func(ctx context.Context, in *body[StartQuizRequest]) (*body[quiz.Started], error) {
			if in.Body.Kind == quiz.Check {
				return out(d.Quiz.StartCheck(ctx, in.Body.Topic))
			}
			return out(d.Quiz.Start(ctx, in.Body.Topic))
		})

	huma.Register(a, op("next-question", http.MethodGet, "/api/quizzes/{id}/next", "The question at the current position"),
		func(ctx context.Context, in *idIn) (*body[quiz.Shown], error) { return out(d.Quiz.Next(ctx, in.ID)) })

	huma.Register(a, op("answer-question", http.MethodPost, "/api/quizzes/{id}/answers", "Answer the current question"),
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body AnswerRequest
		}) (*body[quiz.Result], error) {
			return out(d.Quiz.Answer(ctx, in.ID, in.Body.Position, in.Body.Chosen))
		})

	huma.Register(a, op("get-quiz", http.MethodGet, "/api/quizzes/{id}", "A quiz's summary"),
		func(ctx context.Context, in *idIn) (*body[quiz.Summary], error) {
			return out(d.Quiz.Summary(ctx, in.ID))
		})

	flag := op("flag-question", http.MethodPost, "/api/questions/{id}/flag", "Take a question out of use")
	flag.DefaultStatus = http.StatusNoContent
	huma.Register(a, flag, func(ctx context.Context, in *idIn) (*struct{}, error) {
		if err := d.Quiz.Flag(ctx, in.ID); err != nil {
			return nil, fail(err)
		}
		return &struct{}{}, nil
	})

	huma.Register(a, op("send-chat", http.MethodPost, "/api/chat", "Send a chat message"),
		func(ctx context.Context, in *body[ChatRequest]) (*body[chat.Accepted], error) {
			return out(d.Chat.Send(ctx, in.Body.Text, in.Body.Page))
		})

	huma.Register(a, op("get-chat", http.MethodGet, "/api/chat", "Chat history and any job in progress"),
		func(ctx context.Context, _ *struct{}) (*body[chat.History], error) { return out(d.Chat.History(ctx)) })

	huma.Register(a, op("get-job", http.MethodGet, "/api/jobs/{id}", "A chat job's progress"),
		func(ctx context.Context, in *idIn) (*body[chat.JobView], error) { return out(d.Chat.Job(ctx, in.ID)) })

	huma.Register(a, op("retry-job", http.MethodPost, "/api/jobs/{id}/retry", "Queue a failed chat job's request again"),
		func(ctx context.Context, in *idIn) (*body[chat.Accepted], error) {
			return out(d.Chat.Retry(ctx, in.ID))
		})

	huma.Register(a, op("list-mistakes", http.MethodGet, "/api/mistakes", "Mistakes, for one topic or all"),
		func(ctx context.Context, in *struct {
			Topic string `query:"topic" doc:"topic slug; empty for all"`
		}) (*body[[]quiz.MistakeView], error) {
			return out(d.Quiz.Mistakes(ctx, in.Topic))
		})

	huma.Register(a, op("add-mistake", http.MethodPost, "/api/mistakes", "Add a mistake, for example from an AI agent after a mock interview"),
		func(ctx context.Context, in *body[quiz.AddMistakeInput]) (*body[quiz.MistakeView], error) {
			return out(d.Quiz.AddMistake(ctx, in.Body))
		})

	huma.Register(a, op("get-settings", http.MethodGet, "/api/settings", "Settings"),
		func(ctx context.Context, _ *struct{}) (*body[store.Settings], error) {
			return out(d.Store.Settings(ctx))
		})

	huma.Register(a, op("put-settings", http.MethodPut, "/api/settings", "Save settings"),
		func(ctx context.Context, in *body[store.Settings]) (*body[store.Settings], error) {
			if err := d.Store.SaveSettings(ctx, in.Body); err != nil {
				return nil, fail(err)
			}
			return out(d.Store.Settings(ctx))
		})

	huma.Register(a, op("get-status", http.MethodGet, "/api/status", "Claude runner status and question counts"),
		func(ctx context.Context, _ *struct{}) (*body[Status], error) {
			s := Status{Auth: "unreachable"}
			hctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			if h, err := d.Runner.Health(hctx); err == nil {
				s.Claude, s.Auth = h.Claude, h.Auth
			}
			tops, err := d.Store.Topics(ctx)
			if err != nil {
				return nil, fail(err)
			}
			s.Topics = len(tops)
			if s.LastError, err = d.Store.LastJobError(ctx); err != nil {
				return nil, fail(err)
			}
			for _, t := range tops {
				n, err := d.Store.CountQuestions(ctx, t.ID)
				if err != nil {
					return nil, fail(err)
				}
				s.Questions += n
			}
			return &body[Status]{Body: s}, nil
		})
}
