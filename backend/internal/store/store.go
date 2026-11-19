// Package store is Quizroom's Postgres access: the schema, the seed data, and
// every query. Multi-step changes happen inside one transaction each.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"quizroom/internal/mistakes"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ErrNotFound is returned when a row doesn't exist.
var ErrNotFound = errors.New("not found")

// Store wraps the connection pool.
type Store struct{ pool *pgxpool.Pool }

type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Topic struct {
	ID              int64    `json:"id"`
	Slug            string   `json:"slug"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	ConfusionGroups []string `json:"confusion_groups"`
}

type Subtopic struct {
	ID      int64  `json:"id"`
	TopicID int64  `json:"topic_id"`
	Slug    string `json:"slug"`
	Name    string `json:"name"`
}

type Rating struct {
	TopicID int64      `json:"topic_id"`
	Theta   float64    `json:"theta"`
	N       int        `json:"n"`
	RD      *float64   `json:"rd"`    // rating deviation in θ units; nil until backfilled or first answered
	RDAt    *time.Time `json:"rd_at"` // when RD was last updated
}

// HistoryAnswer is one past answer, as the deviation replay needs it.
type HistoryAnswer struct {
	At      time.Time
	P       float64
	Multi   bool
	Options int
	Repeat  bool
}

type Option struct {
	Text        string `json:"text"`
	Correct     bool   `json:"correct"`
	Explanation string `json:"explanation"`
	MistakeID   int64  `json:"mistake_id,omitempty"`
}

type Question struct {
	ID         int64     `json:"id"`
	TopicID    int64     `json:"topic_id"`
	SubtopicID int64     `json:"subtopic_id"`
	Kind       string    `json:"kind"`
	Stem       string    `json:"stem"`
	Code       string    `json:"code"`
	CodeLang   string    `json:"code_lang"`
	Difficulty int       `json:"difficulty"`
	B          float64   `json:"b"`
	N          int       `json:"n"`
	Status     string    `json:"status"`
	Options    []Option  `json:"options"`
	MistakeIDs []int64   `json:"mistake_ids"`
	CreatedAt  time.Time `json:"created_at"`
}

type EligibleQuestion struct {
	Question
	NeverSeen bool `json:"never_seen"`
}

type Mistake struct {
	ID             int64      `json:"id"`
	TopicID        int64      `json:"topic_id"`
	SubtopicID     int64      `json:"subtopic_id"`
	Belief         string     `json:"belief"`
	Correction     string     `json:"correction"`
	Source         string     `json:"source"`
	Status         string     `json:"status"`
	Stage          int        `json:"stage"`
	DueAt          *time.Time `json:"due_at"`
	NeedsQuestions bool       `json:"needs_questions"`
	Hits           int        `json:"hits"`
	CreatedAt      time.Time  `json:"created_at"`
}

// State is the mistake's place in its review schedule.
func (m Mistake) State() mistakes.State {
	s := mistakes.State{Status: mistakes.Status(m.Status), Stage: m.Stage}
	if m.DueAt != nil {
		s.DueAt = *m.DueAt
	}
	return s
}

func dueOf(s mistakes.State) *time.Time {
	if s.DueAt.IsZero() {
		return nil
	}
	t := s.DueAt
	return &t
}

type Quiz struct {
	ID           int64      `json:"id"`
	TopicID      int64      `json:"topic_id"`
	Length       int        `json:"length"`
	Target       float64    `json:"target"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	SkillBefore  float64    `json:"skill_before"`
	MistakeSlots []int      `json:"mistake_slots"`
	Kind         string     `json:"kind"` // "practice" or "check"
	ThetaBefore  *float64   `json:"theta_before"`
}

type Answer struct {
	ID            int64      `json:"id"`
	QuizID        int64      `json:"quiz_id"`
	Position      int        `json:"position"`
	QuestionID    int64      `json:"question_id"`
	Slot          string     `json:"slot"`
	SlotMistakeID *int64     `json:"slot_mistake_id"`
	OptionOrder   []int      `json:"option_order"`
	ShownAt       time.Time  `json:"shown_at"`
	AnsweredAt    *time.Time `json:"answered_at"`
	Chosen        []int      `json:"chosen"`
	Correct       *bool      `json:"correct"`
	Repeat        bool       `json:"repeat"`
	P             float64    `json:"p"`
	ThetaBefore   *float64   `json:"theta_before"`
	ThetaAfter    *float64   `json:"theta_after"`
	BBefore       *float64   `json:"b_before"`
	BAfter        *float64   `json:"b_after"`
	RDBefore      *float64   `json:"rd_before"`
	RDAfter       *float64   `json:"rd_after"`
}

// PageContext is where the learner was when they sent a chat message.
type PageContext struct {
	Page       string `json:"page" enum:"home,topic,quiz,feedback,settings" doc:"the page the chat was opened on"`
	Topic      string `json:"topic" doc:"topic slug, or empty"`
	QuestionID int64  `json:"question_id" doc:"the question on screen, or 0"`
	Chosen     []int  `json:"chosen" doc:"the option IDs the learner chose on that question"`
	Order      []int  `json:"order,omitempty" doc:"the option IDs in the order they were shown, so A, B, C… mean what the learner saw"`
}

type JobRequest struct {
	Text     string      `json:"text"`
	Page     PageContext `json:"page"`
	Followup string      `json:"followup"`
	// Generate marks a "write questions" button press, so the topic's
	// question bank can say Claude is writing.
	Generate bool `json:"generate,omitempty"`
}

type JobResult struct {
	Added   int      `json:"added"`
	Dropped []string `json:"dropped"`
	CostUSD float64  `json:"cost_usd"`
	Reply   string   `json:"reply"`
}

type Job struct {
	ID        int64      `json:"id"`
	Status    string     `json:"status"`
	Request   JobRequest `json:"request"`
	Result    *JobResult `json:"result"`
	Error     string     `json:"error"`
	CreatedAt time.Time  `json:"created_at"`
}

type ChatMessage struct {
	ID        int64     `json:"id"`
	Role      string    `json:"role"`
	Text      string    `json:"text"`
	JobID     *int64    `json:"job_id"`
	CreatedAt time.Time `json:"created_at"`
	Kind      string    `json:"kind"`   // "message" (typed) or "request" (a button press)
	Failed    bool      `json:"failed"` // Claude's message about a job that failed
}

type Settings struct {
	Target float64 `json:"target" enum:"0.65,0.75,0.85" doc:"the predicted chance of success questions are aimed at"`
	Model  string  `json:"model" enum:"sonnet,opus" doc:"the Claude model that writes questions"`
}

type SkillPoint struct {
	At     time.Time `json:"at"`
	Theta  float64   `json:"theta"`
	QuizID int64     `json:"quiz_id"`
	Kind   string    `json:"kind"`
}

// MistakeEvent is a hit or a clear one answer caused.
type MistakeEvent struct {
	MistakeID int64
	Kind      string // "hit" or "clear"
	Position  int    // the answer's position in its quiz
}

// QuizStat is one quiz's totals so far.
type QuizStat struct {
	ID          int64
	StartedAt   time.Time
	FinishedAt  *time.Time
	Length      int
	SkillBefore float64
	ThetaBefore *float64
	Kind        string
	Answered    int
	Right       int
	Expected    float64
	LastTheta   *float64 // the rating after the quiz's latest answer
	Hits        int      // mistakes that came back
	Clears      int      // mistakes that moved on
}

type SubtopicStat struct {
	SubtopicID int64   `json:"subtopic_id"`
	Slug       string  `json:"slug"`
	Name       string  `json:"name"`
	Answered   int     `json:"answered"`
	Right      int     `json:"right"`
	Expected   float64 `json:"expected"`
}

// Open connects, retrying every second until ctx ends, then migrates and seeds.
func Open(ctx context.Context, url string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("bad database URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	for {
		err = pool.Ping(ctx)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, fmt.Errorf("database not reachable: %w", err)
		case <-time.After(time.Second):
		}
	}
	s := &Store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := s.seed(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("seed: %w", err)
	}
	return s, nil
}

// Close closes the pool.
func (s *Store) Close() { s.pool.Close() }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		prefix, _, _ := strings.Cut(e.Name(), "_")
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return fmt.Errorf("migration %s: name must start with a number", e.Name())
		}
		var done bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sql, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version)
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", e.Name(), err)
		}
	}
	return nil
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
