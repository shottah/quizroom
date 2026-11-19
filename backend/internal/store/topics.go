package store

import (
	"context"
	"fmt"

	"quizroom/internal/format"
	"quizroom/internal/rating"
)

const topicCols = `id, slug, name, description, confusion_groups`

func scanTopic(row interface{ Scan(...any) error }) (Topic, error) {
	var t Topic
	err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.Description, &t.ConfusionGroups)
	return t, err
}

// Topics lists every topic, oldest first.
func (s *Store) Topics(ctx context.Context) ([]Topic, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+topicCols+` FROM topics ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Topic
	for rows.Next() {
		t, err := scanTopic(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TopicBySlug returns ErrNotFound for an unknown slug.
func (s *Store) TopicBySlug(ctx context.Context, slug string) (Topic, error) {
	t, err := scanTopic(s.pool.QueryRow(ctx, `SELECT `+topicCols+` FROM topics WHERE slug = $1`, slug))
	return t, notFound(err)
}

// Subtopics lists a topic's subtopics in their seeded order.
func (s *Store) Subtopics(ctx context.Context, topicID int64) ([]Subtopic, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, topic_id, slug, name FROM subtopics WHERE topic_id = $1 ORDER BY position, id`, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subtopic
	for rows.Next() {
		var sub Subtopic
		if err := rows.Scan(&sub.ID, &sub.TopicID, &sub.Slug, &sub.Name); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

func ratingRow(ctx context.Context, q querier, topicID int64, forUpdate bool) (Rating, error) {
	if _, err := q.Exec(ctx, `INSERT INTO ratings (topic_id, theta) VALUES ($1, $2) ON CONFLICT (topic_id) DO NOTHING`,
		topicID, rating.ThetaFromSkill(rating.StartSkill)); err != nil {
		return Rating{}, err
	}
	sql := `SELECT topic_id, theta, n_answers, rd, rd_at FROM ratings WHERE topic_id = $1`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	var r Rating
	err := q.QueryRow(ctx, sql, topicID).Scan(&r.TopicID, &r.Theta, &r.N, &r.RD, &r.RDAt)
	return r, err
}

// Rating is the learner's rating in a topic, created at the start skill if missing.
func (s *Store) Rating(ctx context.Context, topicID int64) (Rating, error) {
	return ratingRow(ctx, s.pool, topicID, false)
}

// CountQuestions counts a topic's active questions.
func (s *Store) CountQuestions(ctx context.Context, topicID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM questions WHERE topic_id = $1 AND status = 'active'`, topicID).Scan(&n)
	return n, err
}

// Known is what Claude's replies are checked against.
func (s *Store) Known(ctx context.Context) (format.Known, error) {
	k := format.Known{Subtopics: map[string]map[string]bool{}, Mistakes: map[string]bool{}, StemKeys: map[string]map[string]bool{}}
	rows, err := s.pool.Query(ctx, `SELECT t.slug, s.slug FROM topics t LEFT JOIN subtopics s ON s.topic_id = t.id`)
	if err != nil {
		return k, err
	}
	for rows.Next() {
		var t string
		var sub *string
		if err := rows.Scan(&t, &sub); err != nil {
			rows.Close()
			return k, err
		}
		if k.Subtopics[t] == nil {
			k.Subtopics[t] = map[string]bool{}
		}
		if sub != nil {
			k.Subtopics[t][*sub] = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return k, err
	}

	rows, err = s.pool.Query(ctx, `SELECT id FROM mistakes`)
	if err != nil {
		return k, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return k, err
		}
		k.Mistakes[fmt.Sprintf("m%d", id)] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return k, err
	}

	rows, err = s.pool.Query(ctx, `SELECT t.slug, q.stem_key FROM questions q JOIN topics t ON t.id = q.topic_id`)
	if err != nil {
		return k, err
	}
	defer rows.Close()
	for rows.Next() {
		var t, key string
		if err := rows.Scan(&t, &key); err != nil {
			return k, err
		}
		if k.StemKeys[t] == nil {
			k.StemKeys[t] = map[string]bool{}
		}
		k.StemKeys[t][key] = true
	}
	return k, rows.Err()
}

// SkillHistory is the learner's rating after each answer in a topic.
func (s *Store) SkillHistory(ctx context.Context, topicID int64) ([]SkillPoint, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.answered_at, a.theta_after, a.quiz_id, z.kind FROM answers a JOIN quizzes z ON z.id = a.quiz_id
		WHERE z.topic_id = $1 AND a.answered_at IS NOT NULL ORDER BY a.answered_at, a.id`, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SkillPoint{}
	for rows.Next() {
		var p SkillPoint
		if err := rows.Scan(&p.At, &p.Theta, &p.QuizID, &p.Kind); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SubtopicStats is how the learner did against expectation in each subtopic.
func (s *Store) SubtopicStats(ctx context.Context, topicID int64) ([]SubtopicStat, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id, s.slug, s.name,
			count(a.id) FILTER (WHERE a.answered_at IS NOT NULL),
			count(a.id) FILTER (WHERE a.correct),
			coalesce(sum(a.p) FILTER (WHERE a.answered_at IS NOT NULL), 0)
		FROM subtopics s
		LEFT JOIN questions q ON q.subtopic_id = s.id
		LEFT JOIN answers a ON a.question_id = q.id
		WHERE s.topic_id = $1
		GROUP BY s.id ORDER BY s.position, s.id`, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubtopicStat
	for rows.Next() {
		var st SubtopicStat
		if err := rows.Scan(&st.SubtopicID, &st.Slug, &st.Name, &st.Answered, &st.Right, &st.Expected); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// RecentStats covers the learner's last `last` answers in a topic.
func (s *Store) RecentStats(ctx context.Context, topicID int64, last int) (answered, right int, expected float64, err error) {
	err = s.pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE correct), coalesce(sum(p), 0) FROM (
			SELECT a.correct, a.p FROM answers a JOIN quizzes z ON z.id = a.quiz_id
			WHERE z.topic_id = $1 AND a.answered_at IS NOT NULL ORDER BY a.answered_at DESC, a.id DESC LIMIT $2) r`,
		topicID, last).Scan(&answered, &right, &expected)
	return
}
