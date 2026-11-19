package store

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
)

// RestartMessage is the error given to jobs a restart interrupted.
const RestartMessage = "Quizroom restarted while Claude was writing, so nothing was added. Press Retry."

// ErrNotRetryable is a retry of a job that hasn't failed.
var ErrNotRetryable = errors.New("only a failed job can be retried")

// retryShown is how much of the original message a retry's card repeats.
const retryShown = 80

const jobCols = `id, status, request, result, error, created_at`

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	err := row.Scan(&j.ID, &j.Status, &j.Request, &j.Result, &j.Error, &j.CreatedAt)
	return j, err
}

// CreateChatJob queues a job and stores the learner's side of it: display is
// what the chat shows, kind is "message" for typed text or "request" for a
// button press.
func (s *Store) CreateChatJob(ctx context.Context, display, kind string, req JobRequest) (id int64, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		id, err = createJob(ctx, tx, display, kind, req)
		return err
	})
	return id, err
}

func createJob(ctx context.Context, tx pgx.Tx, display, kind string, req JobRequest) (id int64, err error) {
	if err := tx.QueryRow(ctx, `INSERT INTO jobs (status, request) VALUES ('queued', $1) RETURNING id`, req).Scan(&id); err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_messages (role, text, job_id, kind) VALUES ('user', $1, $2, $3)`, display, id, kind)
	return id, err
}

// RetryJob queues a failed job's request again as a new job, with a
// "Retrying: …" card in the chat. A second press while that retry is still
// pending returns it instead of queueing another.
func (s *Store) RetryJob(ctx context.Context, id int64) (newID int64, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		var req JobRequest
		if err := tx.QueryRow(ctx, `SELECT status, request FROM jobs WHERE id = $1 FOR UPDATE`, id).Scan(&status, &req); err != nil {
			return notFound(err)
		}
		if status != "failed" {
			return ErrNotRetryable
		}
		var display string
		err := tx.QueryRow(ctx, `SELECT text FROM chat_messages WHERE job_id = $1 AND role = 'user' ORDER BY id LIMIT 1`, id).Scan(&display)
		if errors.Is(err, pgx.ErrNoRows) {
			display = req.Text
		} else if err != nil {
			return err
		}
		if r := []rune(display); len(r) > retryShown {
			display = string(r[:retryShown]) + "…"
		}
		display = "Retrying: " + display
		err = tx.QueryRow(ctx, `SELECT j.id FROM jobs j JOIN chat_messages m ON m.job_id = j.id AND m.role = 'user'
			WHERE j.status IN ('queued', 'running') AND j.request = $1 AND m.text = $2 ORDER BY j.id LIMIT 1`, req, display).Scan(&newID)
		if err == nil || !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		newID, err = createJob(ctx, tx, display, "request", req)
		return err
	})
	return newID, err
}

// PendingJobForTopic reports whether Claude is writing questions for the
// topic: a queued or running generate job whose page is the topic.
func (s *Store) PendingJobForTopic(ctx context.Context, slug string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM jobs WHERE status IN ('queued', 'running')
		AND (request->>'generate')::boolean AND request->'page'->>'topic' = $1)`, slug).Scan(&ok)
	return ok, err
}

// ClaimJob marks the oldest queued job running and returns it, or nil.
func (s *Store) ClaimJob(ctx context.Context) (*Job, error) {
	j, err := scanJob(s.pool.QueryRow(ctx, `UPDATE jobs SET status = 'running', started_at = now()
		WHERE id = (SELECT id FROM jobs WHERE status = 'queued' ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING `+jobCols))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (s *Store) finish(ctx context.Context, id int64, status string, res *JobResult, errMsg, text string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE jobs SET status = $2, result = $3, error = $4, finished_at = now() WHERE id = $1`,
			id, status, res, errMsg); err != nil {
			return err
		}
		if text == "" {
			return nil
		}
		_, err := tx.Exec(ctx, `INSERT INTO chat_messages (role, text, job_id) VALUES ('assistant', $1, $2)`, text, id)
		return err
	})
}

// FinishJob records a job's result and Claude's message.
func (s *Store) FinishJob(ctx context.Context, id int64, res JobResult, assistantText string) error {
	return s.finish(ctx, id, "done", &res, "", assistantText)
}

// FailJob records why a job failed, and tells the learner in the chat.
func (s *Store) FailJob(ctx context.Context, id int64, msg string) error {
	return s.finish(ctx, id, "failed", nil, msg, msg)
}

// RequeueJob puts a job back in the queue (the runner was busy).
func (s *Store) RequeueJob(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE jobs SET status = 'queued', started_at = NULL WHERE id = $1`, id)
	return err
}

// RecoverJobs fails the jobs a restart left running.
func (s *Store) RecoverJobs(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM jobs WHERE status = 'running'`)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := s.FailJob(ctx, id, RestartMessage); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// Job returns one job, or ErrNotFound.
func (s *Store) Job(ctx context.Context, id int64) (Job, error) {
	j, err := scanJob(s.pool.QueryRow(ctx, `SELECT `+jobCols+` FROM jobs WHERE id = $1`, id))
	return j, notFound(err)
}

// LastJobError is the error of the most recently finished job if it failed,
// otherwise "" (a later success means the problem is gone).
func (s *Store) LastJobError(ctx context.Context) (string, error) {
	var status, msg string
	err := s.pool.QueryRow(ctx, `SELECT status, error FROM jobs WHERE status IN ('done', 'failed')
		ORDER BY finished_at DESC NULLS LAST, id DESC LIMIT 1`).Scan(&status, &msg)
	if errors.Is(err, pgx.ErrNoRows) || status != "failed" {
		return "", nil
	}
	return msg, err
}

// PendingJobWithText is a queued or running job for exactly this message, or nil.
func (s *Store) PendingJobWithText(ctx context.Context, text string) (*int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM jobs WHERE status IN ('queued', 'running') AND request->>'text' = $1
		ORDER BY id LIMIT 1`, text).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// PendingJob is the oldest queued or running job's ID, or nil.
func (s *Store) PendingJob(ctx context.Context) (*int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM jobs WHERE status IN ('queued', 'running') ORDER BY id LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// chatCols reads chat messages, marking Claude's messages about failed jobs.
const chatCols = `m.id, m.role, m.text, m.job_id, m.created_at, m.kind,
	coalesce(m.role = 'assistant' AND j.status = 'failed', false)
	FROM chat_messages m LEFT JOIN jobs j ON j.id = m.job_id`

func (s *Store) chatQuery(ctx context.Context, sql string, args ...any) ([]ChatMessage, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.Role, &m.Text, &m.JobID, &m.CreatedAt, &m.Kind, &m.Failed); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	slices.Reverse(out) // queried newest first; returned oldest first
	return out, rows.Err()
}

// ChatHistory is the last `limit` chat messages, oldest first.
func (s *Store) ChatHistory(ctx context.Context, limit int) ([]ChatMessage, error) {
	return s.chatQuery(ctx, `SELECT `+chatCols+` ORDER BY m.id DESC LIMIT $1`, limit)
}

// ChatHistoryBefore is the last `limit` messages before a job's own message.
func (s *Store) ChatHistoryBefore(ctx context.Context, jobID int64, limit int) ([]ChatMessage, error) {
	return s.chatQuery(ctx, `SELECT `+chatCols+`
		WHERE m.id < coalesce((SELECT min(id) FROM chat_messages WHERE job_id = $1 AND role = 'user'), 9223372036854775807)
		ORDER BY m.id DESC LIMIT $2`, jobID, limit)
}
