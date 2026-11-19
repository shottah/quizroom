package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"quizroom/internal/mistakes"
)

const mistakeCols = `id, topic_id, subtopic_id, belief, correction, source, status, stage, due_at, needs_questions, created_at,
	(SELECT count(*) FROM mistake_events e WHERE e.mistake_id = mistakes.id AND e.kind = 'hit')`

func scanMistake(row interface{ Scan(...any) error }) (Mistake, error) {
	var m Mistake
	err := row.Scan(&m.ID, &m.TopicID, &m.SubtopicID, &m.Belief, &m.Correction, &m.Source, &m.Status, &m.Stage,
		&m.DueAt, &m.NeedsQuestions, &m.CreatedAt, &m.Hits)
	return m, err
}

func queryMistakes(ctx context.Context, q querier, sql string, args ...any) ([]Mistake, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Mistake{}
	for rows.Next() {
		m, err := scanMistake(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DueMistakes are a topic's active mistakes due by `at`, most overdue first.
func (s *Store) DueMistakes(ctx context.Context, topicID int64, at time.Time) ([]Mistake, error) {
	return queryMistakes(ctx, s.pool, `SELECT `+mistakeCols+` FROM mistakes
		WHERE topic_id = $1 AND status = 'active' AND due_at <= $2 ORDER BY due_at, id`, topicID, at)
}

// Mistakes lists a topic's mistakes (topicID 0 for all): active first, then by due date.
func (s *Store) Mistakes(ctx context.Context, topicID int64) ([]Mistake, error) {
	return queryMistakes(ctx, s.pool, `SELECT `+mistakeCols+` FROM mistakes
		WHERE $1 = 0 OR topic_id = $1
		ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'watching' THEN 1 ELSE 2 END, due_at NULLS LAST, id`, topicID)
}

// Mistake returns one mistake, or ErrNotFound.
func (s *Store) Mistake(ctx context.Context, id int64) (Mistake, error) {
	m, err := scanMistake(s.pool.QueryRow(ctx, `SELECT `+mistakeCols+` FROM mistakes WHERE id = $1`, id))
	return m, notFound(err)
}

// SetNeedsQuestions marks a mistake that had no unseen question for its slot.
func (s *Store) SetNeedsQuestions(ctx context.Context, id int64, v bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE mistakes SET needs_questions = $2 WHERE id = $1`, id, v)
	return err
}

func saveMistakeState(ctx context.Context, q querier, id int64, st mistakes.State) error {
	_, err := q.Exec(ctx, `UPDATE mistakes SET status = $2, stage = $3, due_at = $4,
		needs_questions = needs_questions AND $2 <> 'resolved' WHERE id = $1`,
		id, string(st.Status), st.Stage, dueOf(st))
	return err
}

func addEvent(ctx context.Context, q querier, mistakeID int64, answerID *int64, kind mistakes.EventKind, at time.Time) error {
	_, err := q.Exec(ctx, `INSERT INTO mistake_events (mistake_id, answer_id, kind, happened_at) VALUES ($1, $2, $3, $4)`,
		mistakeID, answerID, string(kind), at)
	return err
}

// AddMistake records a mistake the learner made outside a quiz (after a mock
// interview, or described in the chat). An existing belief (case-insensitive)
// in the topic gets a hit instead, and created is false.
func (s *Store) AddMistake(ctx context.Context, topicSlug, subtopicSlug, belief, correction string, src mistakes.Source, at time.Time) (m Mistake, created bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var topicID, subID int64
		if err := tx.QueryRow(ctx, `SELECT t.id, s.id FROM topics t JOIN subtopics s ON s.topic_id = t.id
			WHERE t.slug = $1 AND s.slug = $2`, topicSlug, subtopicSlug).Scan(&topicID, &subID); err != nil {
			return notFound(err)
		}
		var id int64
		err := tx.QueryRow(ctx, `SELECT id FROM mistakes WHERE topic_id = $1 AND lower(belief) = lower($2) FOR UPDATE`, topicID, belief).Scan(&id)
		switch {
		case err == nil:
			if err := saveMistakeState(ctx, tx, id, mistakes.Hit(at)); err != nil {
				return err
			}
			if err := addEvent(ctx, tx, id, nil, mistakes.HitEvent, at); err != nil {
				return err
			}
		case errors.Is(err, pgx.ErrNoRows):
			st := mistakes.Initial(src, at)
			if err := tx.QueryRow(ctx, `INSERT INTO mistakes (topic_id, subtopic_id, belief, correction, source, status, stage, due_at, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
				topicID, subID, belief, correction, string(src), string(st.Status), st.Stage, dueOf(st), at).Scan(&id); err != nil {
				return err
			}
			created = true
		default:
			return err
		}
		m, err = scanMistake(tx.QueryRow(ctx, `SELECT `+mistakeCols+` FROM mistakes WHERE id = $1`, id))
		return err
	})
	return m, created, err
}
