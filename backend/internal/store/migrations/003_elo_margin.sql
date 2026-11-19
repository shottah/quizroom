-- Elo margins: a rating deviation per topic (the margin), the deviation around
-- each answer, and quiz kinds (practice or a skill check).
ALTER TABLE ratings ADD COLUMN rd DOUBLE PRECISION, ADD COLUMN rd_at TIMESTAMPTZ;
ALTER TABLE answers ADD COLUMN rd_before DOUBLE PRECISION, ADD COLUMN rd_after DOUBLE PRECISION;
ALTER TABLE quizzes ADD COLUMN kind TEXT NOT NULL DEFAULT 'practice' CHECK (kind IN ('practice', 'check')),
  ADD COLUMN theta_before DOUBLE PRECISION;
