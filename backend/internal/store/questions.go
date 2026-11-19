package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"quizroom/internal/format"
	"quizroom/internal/mistakes"
	"quizroom/internal/picker"
	"quizroom/internal/rating"
)

const questionCols = `id, topic_id, subtopic_id, kind, stem, code, code_lang, difficulty, b, n_answers, status, options, mistake_ids, created_at`

func scanQuestion(row interface{ Scan(...any) error }, extra ...any) (Question, error) {
	var q Question
	dest := append([]any{&q.ID, &q.TopicID, &q.SubtopicID, &q.Kind, &q.Stem, &q.Code, &q.CodeLang, &q.Difficulty,
		&q.B, &q.N, &q.Status, &q.Options, &q.MistakeIDs, &q.CreatedAt}, extra...)
	err := row.Scan(dest...)
	if q.MistakeIDs == nil {
		q.MistakeIDs = []int64{}
	}
	return q, err
}

// Eligible lists a topic's active questions that may be shown at `now`: not
// shown within picker.RepeatWindow and not already in quiz quizID (0 = none).
func (s *Store) Eligible(ctx context.Context, topicID, quizID int64, now time.Time) ([]EligibleQuestion, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+questionCols+`,
			NOT EXISTS (SELECT 1 FROM answers a WHERE a.question_id = questions.id)
		FROM questions
		WHERE topic_id = $1 AND status = 'active'
		  AND NOT EXISTS (SELECT 1 FROM answers a WHERE a.question_id = questions.id AND a.shown_at > $3)
		  AND NOT EXISTS (SELECT 1 FROM answers a WHERE a.question_id = questions.id AND a.quiz_id = $2)
		ORDER BY id`, topicID, quizID, now.Add(-picker.RepeatWindow))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EligibleQuestion
	for rows.Next() {
		var e EligibleQuestion
		q, err := scanQuestion(rows, &e.NeverSeen)
		if err != nil {
			return nil, err
		}
		e.Question = q
		out = append(out, e)
	}
	return out, rows.Err()
}

// BackAt is when the topic's first resting question (shown within the
// repeat window) can be shown again, or nil when none is resting.
func (s *Store) BackAt(ctx context.Context, topicID int64, now time.Time) (*time.Time, error) {
	var last *time.Time
	err := s.pool.QueryRow(ctx, `SELECT min(last) FROM (
			SELECT max(a.shown_at) AS last FROM answers a JOIN questions q ON q.id = a.question_id
			WHERE q.topic_id = $1 AND q.status = 'active'
			GROUP BY a.question_id
			HAVING max(a.shown_at) > $2) rest`, topicID, now.Add(-picker.RepeatWindow)).Scan(&last)
	if err != nil || last == nil {
		return nil, err
	}
	back := last.Add(picker.RepeatWindow)
	return &back, nil
}

// Question returns one question, or ErrNotFound.
func (s *Store) Question(ctx context.Context, id int64) (Question, error) {
	q, err := scanQuestion(s.pool.QueryRow(ctx, `SELECT `+questionCols+` FROM questions WHERE id = $1`, id))
	return q, notFound(err)
}

// RecentStems are a topic's newest question stems, newest first.
func (s *Store) RecentStems(ctx context.Context, topicID int64, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT stem FROM questions WHERE topic_id = $1 ORDER BY id DESC LIMIT $2`, topicID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var stem string
		if err := rows.Scan(&stem); err != nil {
			return nil, err
		}
		out = append(out, stem)
	}
	return out, rows.Err()
}

// FlagQuestion takes a question out of use and undoes the mistake hits and
// clears its answers caused, replaying each affected mistake's other events.
// Rating changes are kept.
func (s *Store) FlagQuestion(ctx context.Context, id int64) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE questions SET status = 'flagged' WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		rows, err := tx.Query(ctx, `DELETE FROM mistake_events e USING answers a
			WHERE e.answer_id = a.id AND a.question_id = $1 RETURNING e.mistake_id`, id)
		if err != nil {
			return err
		}
		affected := map[int64]bool{}
		for rows.Next() {
			var m int64
			if err := rows.Scan(&m); err != nil {
				rows.Close()
				return err
			}
			affected[m] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for m := range affected {
			if err := replayMistake(ctx, tx, m); err != nil {
				return err
			}
		}
		return nil
	})
}

func replayMistake(ctx context.Context, tx pgx.Tx, id int64) error {
	var source string
	var created time.Time
	if err := tx.QueryRow(ctx, `SELECT source, created_at FROM mistakes WHERE id = $1 FOR UPDATE`, id).Scan(&source, &created); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT kind, happened_at FROM mistake_events WHERE mistake_id = $1 ORDER BY happened_at, id`, id)
	if err != nil {
		return err
	}
	var events []mistakes.Event
	for rows.Next() {
		var e mistakes.Event
		var kind string
		if err := rows.Scan(&kind, &e.At); err != nil {
			rows.Close()
			return err
		}
		e.Kind = mistakes.EventKind(kind)
		events = append(events, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	return saveMistakeState(ctx, tx, id, mistakes.Replay(mistakes.Initial(mistakes.Source(source), created), events))
}

func head(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// ApplyReply stores a checked reply in one transaction: the new topic (with
// its subtopics and rating), the new mistakes, and the questions. A question
// whose stem already exists in its topic is dropped as a duplicate.
func (s *Store) ApplyReply(ctx context.Context, jobID int64, c format.Clean, at time.Time) (added []Question, dropped []format.Dropped, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		added, dropped = nil, nil
		if t := c.Topic; t != nil {
			groups := t.ConfusionGroups
			if groups == nil {
				groups = []string{}
			}
			var id int64
			if err := tx.QueryRow(ctx, `INSERT INTO topics (slug, name, description, confusion_groups) VALUES ($1, $2, $3, $4) RETURNING id`,
				t.Slug, t.Name, t.Description, groups).Scan(&id); err != nil {
				return err
			}
			for i, sub := range t.Subtopics {
				if _, err := tx.Exec(ctx, `INSERT INTO subtopics (topic_id, slug, name, position) VALUES ($1, $2, $3, $4)`, id, sub.Slug, sub.Name, i); err != nil {
					return err
				}
			}
			if _, err := ratingRow(ctx, tx, id, false); err != nil {
				return err
			}
		}

		type key struct{ topic, sub string }
		ids := map[key][2]int64{} // → topic ID, subtopic ID
		rows, err := tx.Query(ctx, `SELECT t.slug, s.slug, t.id, s.id FROM topics t JOIN subtopics s ON s.topic_id = t.id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var k key
			var v [2]int64
			if err := rows.Scan(&k.topic, &k.sub, &v[0], &v[1]); err != nil {
				rows.Close()
				return err
			}
			ids[k] = v
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		newIDs := map[string]int64{}
		for _, m := range c.Mistakes {
			v, ok := ids[key{m.Topic, m.Subtopic}]
			if !ok {
				dropped = append(dropped, format.Dropped{What: "mistake: " + head(m.Belief, 60), Reason: format.ReasonUnknown})
				continue
			}
			src := mistakes.Quiz
			if m.FromUser {
				src = mistakes.Chat
			}
			st := mistakes.Initial(src, at)
			var id int64
			err := tx.QueryRow(ctx, `INSERT INTO mistakes (topic_id, subtopic_id, belief, correction, source, status, stage, due_at, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT (topic_id, lower(belief)) DO NOTHING RETURNING id`,
				v[0], v[1], m.Belief, m.Correction, string(src), string(st.Status), st.Stage, dueOf(st), at).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				if err := tx.QueryRow(ctx, `SELECT id FROM mistakes WHERE topic_id = $1 AND lower(belief) = lower($2)`, v[0], m.Belief).Scan(&id); err != nil {
					return err
				}
				if m.FromUser {
					if err := saveMistakeState(ctx, tx, id, mistakes.Hit(at)); err != nil {
						return err
					}
					if err := addEvent(ctx, tx, id, nil, mistakes.HitEvent, at); err != nil {
						return err
					}
				}
			} else if err != nil {
				return err
			}
			newIDs[m.Key] = id
		}

		var job *int64
		if jobID != 0 {
			job = &jobID
		}
		for _, q := range c.Questions {
			v, ok := ids[key{q.Topic, q.Subtopic}]
			if !ok {
				dropped = append(dropped, format.Dropped{What: "question: " + head(q.Stem, 60), Reason: format.ReasonUnknown})
				continue
			}
			opts := make([]Option, len(q.Options))
			tags := []int64{}
			seen := map[int64]bool{}
			for i, o := range q.Options {
				var mid int64
				switch {
				case strings.HasPrefix(o.Mistake, "m"):
					mid, _ = strconv.ParseInt(o.Mistake[1:], 10, 64)
				case strings.HasPrefix(o.Mistake, "n"):
					mid = newIDs[o.Mistake]
				}
				opts[i] = Option{Text: o.Text, Correct: o.Correct, Explanation: o.Explanation, MistakeID: mid}
				if mid != 0 && !seen[mid] {
					seen[mid] = true
					tags = append(tags, mid)
				}
			}
			row := tx.QueryRow(ctx, `INSERT INTO questions (topic_id, subtopic_id, kind, stem, stem_key, code, code_lang, difficulty, b, options, mistake_ids, job_id, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
				ON CONFLICT (topic_id, stem_key) DO NOTHING RETURNING `+questionCols,
				v[0], v[1], string(q.Kind), q.Stem, format.StemKey(q.Stem), q.Code, q.CodeLang, q.Difficulty,
				rating.BFromDifficulty(q.Difficulty), opts, tags, job, at)
			stored, err := scanQuestion(row)
			if errors.Is(err, pgx.ErrNoRows) {
				dropped = append(dropped, format.Dropped{What: "question: " + head(q.Stem, 60), Reason: format.ReasonDuplicate})
				continue
			}
			if err != nil {
				return err
			}
			added = append(added, stored)
		}
		var tagged []int64
		for _, q := range added {
			tagged = append(tagged, q.MistakeIDs...)
		}
		if len(tagged) > 0 {
			if _, err := tx.Exec(ctx, `UPDATE mistakes SET needs_questions = false WHERE id = ANY($1)`, tagged); err != nil {
				return err
			}
		}
		return nil
	})
	return added, dropped, err
}
