package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"quizroom/internal/mistakes"
)

const quizCols = `id, topic_id, length, target, started_at, finished_at, skill_before, mistake_slots, kind, theta_before`

func scanQuiz(row interface{ Scan(...any) error }) (Quiz, error) {
	var q Quiz
	err := row.Scan(&q.ID, &q.TopicID, &q.Length, &q.Target, &q.StartedAt, &q.FinishedAt, &q.SkillBefore, &q.MistakeSlots,
		&q.Kind, &q.ThetaBefore)
	if q.MistakeSlots == nil {
		q.MistakeSlots = []int{}
	}
	return q, err
}

const answerCols = `id, quiz_id, position, question_id, slot, slot_mistake_id, option_order, shown_at, answered_at,
	chosen, correct, is_repeat, p, theta_before, theta_after, b_before, b_after, rd_before, rd_after`

func scanAnswer(row interface{ Scan(...any) error }) (Answer, error) {
	var a Answer
	err := row.Scan(&a.ID, &a.QuizID, &a.Position, &a.QuestionID, &a.Slot, &a.SlotMistakeID, &a.OptionOrder, &a.ShownAt,
		&a.AnsweredAt, &a.Chosen, &a.Correct, &a.Repeat, &a.P, &a.ThetaBefore, &a.ThetaAfter, &a.BBefore, &a.BAfter,
		&a.RDBefore, &a.RDAfter)
	return a, err
}

// CreateQuiz stores a new quiz and returns its ID.
func (s *Store) CreateQuiz(ctx context.Context, q Quiz) (int64, error) {
	slots := q.MistakeSlots
	if slots == nil {
		slots = []int{}
	}
	kind := q.Kind
	if kind == "" {
		kind = "practice"
	}
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO quizzes (topic_id, length, target, started_at, skill_before, mistake_slots, kind, theta_before)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, q.TopicID, q.Length, q.Target, q.StartedAt, q.SkillBefore, slots,
		kind, q.ThetaBefore).Scan(&id)
	return id, err
}

// Quiz returns one quiz, or ErrNotFound.
func (s *Store) Quiz(ctx context.Context, id int64) (Quiz, error) {
	q, err := scanQuiz(s.pool.QueryRow(ctx, `SELECT `+quizCols+` FROM quizzes WHERE id = $1`, id))
	return q, notFound(err)
}

// QuizAnswers lists a quiz's shown questions by position.
func (s *Store) QuizAnswers(ctx context.Context, quizID int64) ([]Answer, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+answerCols+` FROM answers WHERE quiz_id = $1 ORDER BY position`, quizID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Answer{}
	for rows.Next() {
		a, err := scanAnswer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// FinishQuiz marks a quiz finished, once.
func (s *Store) FinishQuiz(ctx context.Context, id int64, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE quizzes SET finished_at = $2 WHERE id = $1 AND finished_at IS NULL`, id, at)
	return err
}

// InsertShown records a question being shown. If that position was already
// shown (a double Next), the existing row is returned unchanged.
func (s *Store) InsertShown(ctx context.Context, a Answer) (Answer, error) {
	row := s.pool.QueryRow(ctx, `INSERT INTO answers (quiz_id, position, question_id, slot, slot_mistake_id, option_order, shown_at, is_repeat, p)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT (quiz_id, position) DO NOTHING RETURNING `+answerCols,
		a.QuizID, a.Position, a.QuestionID, a.Slot, a.SlotMistakeID, a.OptionOrder, a.ShownAt, a.Repeat, a.P)
	got, err := scanAnswer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return scanAnswer(s.pool.QueryRow(ctx, `SELECT `+answerCols+` FROM answers WHERE quiz_id = $1 AND position = $2`, a.QuizID, a.Position))
	}
	return got, err
}

// AnswerState is everything an answer's update depends on.
type AnswerState struct {
	Answer   Answer
	Quiz     Quiz
	Question Question
	Rating   Rating
	Mistakes map[int64]Mistake // the mistakes tagged on the question's options
}

// MistakeChange is a mistake's new state and the event that caused it.
type MistakeChange struct {
	ID    int64
	State mistakes.State
	Event mistakes.EventKind
}

// AnswerUpdate is what compute decides for one answer.
type AnswerUpdate struct {
	Chosen     []int
	Correct    bool
	ThetaAfter float64
	BAfter     float64
	RDBefore   float64
	RDAfter    float64
	Mistakes   []MistakeChange
	Finished   bool
}

func loadAnswerState(ctx context.Context, q querier, quizID int64, position int, lock bool) (AnswerState, error) {
	forUpdate := ""
	if lock {
		forUpdate = " FOR UPDATE"
	}
	var st AnswerState
	a, err := scanAnswer(q.QueryRow(ctx, `SELECT `+answerCols+` FROM answers WHERE quiz_id = $1 AND position = $2`+forUpdate, quizID, position))
	if err != nil {
		return st, notFound(err)
	}
	st.Answer = a
	if st.Quiz, err = scanQuiz(q.QueryRow(ctx, `SELECT `+quizCols+` FROM quizzes WHERE id = $1`, quizID)); err != nil {
		return st, err
	}
	if st.Question, err = scanQuestion(q.QueryRow(ctx, `SELECT `+questionCols+` FROM questions WHERE id = $1`+forUpdate, a.QuestionID)); err != nil {
		return st, err
	}
	if st.Rating, err = ratingRow(ctx, q, st.Quiz.TopicID, lock); err != nil {
		return st, err
	}
	ms, err := queryMistakes(ctx, q, `SELECT `+mistakeCols+` FROM mistakes WHERE id = ANY($1) ORDER BY id`+forUpdate, st.Question.MistakeIDs)
	if err != nil {
		return st, err
	}
	st.Mistakes = map[int64]Mistake{}
	for _, m := range ms {
		st.Mistakes[m.ID] = m
	}
	return st, nil
}

// ApplyAnswer applies one answer atomically. It locks the answer, rating,
// question and tagged mistakes, and calls compute with their current state.
// If the position was already answered, it returns that state with
// already=true and doesn't call compute, so a double submit changes nothing.
func (s *Store) ApplyAnswer(ctx context.Context, quizID int64, position int, at time.Time,
	compute func(AnswerState) (AnswerUpdate, error)) (after AnswerState, already bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		st, err := loadAnswerState(ctx, tx, quizID, position, true)
		if err != nil {
			return err
		}
		if st.Answer.AnsweredAt != nil {
			already = true
			after = st
			return nil
		}
		upd, err := compute(st)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE answers SET answered_at = $2, chosen = $3, correct = $4,
				theta_before = $5, theta_after = $6, b_before = $7, b_after = $8, rd_before = $9, rd_after = $10 WHERE id = $1`,
			st.Answer.ID, at, upd.Chosen, upd.Correct, st.Rating.Theta, upd.ThetaAfter, st.Question.B, upd.BAfter,
			upd.RDBefore, upd.RDAfter); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE ratings SET theta = $2, n_answers = n_answers + 1, updated_at = $3, rd = $4, rd_at = $3
				WHERE topic_id = $1`, st.Quiz.TopicID, upd.ThetaAfter, at, upd.RDAfter); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE questions SET b = $2, n_answers = n_answers + 1 WHERE id = $1`, st.Question.ID, upd.BAfter); err != nil {
			return err
		}
		answerID := st.Answer.ID
		for _, m := range upd.Mistakes {
			if err := saveMistakeState(ctx, tx, m.ID, m.State); err != nil {
				return err
			}
			if err := addEvent(ctx, tx, m.ID, &answerID, m.Event, at); err != nil {
				return err
			}
		}
		if upd.Finished {
			if _, err := tx.Exec(ctx, `UPDATE quizzes SET finished_at = $2 WHERE id = $1 AND finished_at IS NULL`, quizID, at); err != nil {
				return err
			}
		}
		after, err = loadAnswerState(ctx, tx, quizID, position, false)
		return err
	})
	return after, already, err
}

// QuizMistakeCounts counts the hits and clears a quiz's answers caused.
func (s *Store) QuizMistakeCounts(ctx context.Context, quizID int64) (hits, clears int, err error) {
	err = s.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE e.kind = 'hit'), count(*) FILTER (WHERE e.kind = 'clear')
		FROM mistake_events e JOIN answers a ON a.id = e.answer_id WHERE a.quiz_id = $1`, quizID).Scan(&hits, &clears)
	return
}

// TopicQuizzes are a topic's latest quizzes with their totals, newest first.
func (s *Store) TopicQuizzes(ctx context.Context, topicID int64, limit int) ([]QuizStat, error) {
	rows, err := s.pool.Query(ctx, `SELECT z.id, z.started_at, z.finished_at, z.length, z.skill_before, z.theta_before, z.kind,
			count(a.id) FILTER (WHERE a.answered_at IS NOT NULL),
			count(a.id) FILTER (WHERE a.correct),
			coalesce(sum(a.p) FILTER (WHERE a.answered_at IS NOT NULL), 0),
			(SELECT l.theta_after FROM answers l WHERE l.quiz_id = z.id AND l.answered_at IS NOT NULL
				ORDER BY l.answered_at DESC, l.id DESC LIMIT 1),
			(SELECT count(*) FROM mistake_events e JOIN answers ea ON ea.id = e.answer_id WHERE ea.quiz_id = z.id AND e.kind = 'hit'),
			(SELECT count(*) FROM mistake_events e JOIN answers ea ON ea.id = e.answer_id WHERE ea.quiz_id = z.id AND e.kind = 'clear')
		FROM quizzes z LEFT JOIN answers a ON a.quiz_id = z.id
		WHERE z.topic_id = $1
		GROUP BY z.id ORDER BY z.started_at DESC, z.id DESC LIMIT $2`, topicID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QuizStat{}
	for rows.Next() {
		var q QuizStat
		if err := rows.Scan(&q.ID, &q.StartedAt, &q.FinishedAt, &q.Length, &q.SkillBefore, &q.ThetaBefore, &q.Kind, &q.Answered, &q.Right, &q.Expected,
			&q.LastTheta, &q.Hits, &q.Clears); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// AnswerEvents are the hits and clears one answer caused.
func (s *Store) AnswerEvents(ctx context.Context, answerID int64) ([]MistakeEvent, error) {
	return s.mistakeEvents(ctx, `e.answer_id = $1`, answerID)
}

// QuizEvents are the hits and clears a quiz's answers caused, in answer order.
func (s *Store) QuizEvents(ctx context.Context, quizID int64) ([]MistakeEvent, error) {
	return s.mistakeEvents(ctx, `a.quiz_id = $1`, quizID)
}

func (s *Store) mistakeEvents(ctx context.Context, where string, id int64) ([]MistakeEvent, error) {
	rows, err := s.pool.Query(ctx, `SELECT e.mistake_id, e.kind, a.position
		FROM mistake_events e JOIN answers a ON a.id = e.answer_id WHERE `+where+` ORDER BY a.position, e.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MistakeEvent{}
	for rows.Next() {
		var e MistakeEvent
		if err := rows.Scan(&e.MistakeID, &e.Kind, &e.Position); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// TopicAnswerHistory is every answer in a topic, oldest first, for replaying
// the rating deviation.
func (s *Store) TopicAnswerHistory(ctx context.Context, topicID int64) ([]HistoryAnswer, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.answered_at, a.p, q.kind = 'multi', jsonb_array_length(q.options), a.is_repeat
		FROM answers a JOIN quizzes z ON z.id = a.quiz_id JOIN questions q ON q.id = a.question_id
		WHERE z.topic_id = $1 AND a.answered_at IS NOT NULL ORDER BY a.answered_at, a.id`, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HistoryAnswer{}
	for rows.Next() {
		var h HistoryAnswer
		if err := rows.Scan(&h.At, &h.P, &h.Multi, &h.Options, &h.Repeat); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// RatingsWithoutRD are the topics whose rating has no deviation yet.
func (s *Store) RatingsWithoutRD(ctx context.Context) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT topic_id FROM ratings WHERE rd IS NULL ORDER BY topic_id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// SetRD stores a topic rating's deviation and when it was last updated; nil
// clears it.
func (s *Store) SetRD(ctx context.Context, topicID int64, rd *float64, at *time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE ratings SET rd = $2, rd_at = $3 WHERE topic_id = $1`, topicID, rd, at)
	return err
}

// CheckStat is a finished skill check's result: the rating and deviation
// after its last answer.
type CheckStat struct {
	QuizID     int64
	FinishedAt time.Time
	Answered   int
	Theta      *float64
	RD         *float64
}

// TopicChecks are a topic's finished skill checks, newest first.
func (s *Store) TopicChecks(ctx context.Context, topicID int64, limit int) ([]CheckStat, error) {
	rows, err := s.pool.Query(ctx, `SELECT z.id, z.finished_at,
			(SELECT count(*) FROM answers a WHERE a.quiz_id = z.id AND a.answered_at IS NOT NULL),
			l.theta_after, l.rd_after
		FROM quizzes z
		LEFT JOIN LATERAL (SELECT theta_after, rd_after FROM answers a WHERE a.quiz_id = z.id AND a.answered_at IS NOT NULL
			ORDER BY a.answered_at DESC, a.id DESC LIMIT 1) l ON true
		WHERE z.topic_id = $1 AND z.kind = 'check' AND z.finished_at IS NOT NULL
		ORDER BY z.finished_at DESC, z.id DESC LIMIT $2`, topicID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CheckStat{}
	for rows.Next() {
		var c CheckStat
		if err := rows.Scan(&c.QuizID, &c.FinishedAt, &c.Answered, &c.Theta, &c.RD); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
