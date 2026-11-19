-- Quizroom schema. See quizroom.md ("Data") and the build plan (Task 7).

CREATE TABLE topics (
  id               BIGSERIAL PRIMARY KEY,
  slug             TEXT NOT NULL UNIQUE,
  name             TEXT NOT NULL,
  description      TEXT NOT NULL DEFAULT '',
  confusion_groups JSONB NOT NULL DEFAULT '[]',
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE subtopics (
  id       BIGSERIAL PRIMARY KEY,
  topic_id BIGINT NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  slug     TEXT NOT NULL,
  name     TEXT NOT NULL,
  position INT NOT NULL DEFAULT 0,
  UNIQUE (topic_id, slug)
);

CREATE TABLE ratings (
  topic_id   BIGINT PRIMARY KEY REFERENCES topics(id) ON DELETE CASCADE,
  theta      DOUBLE PRECISION NOT NULL,
  n_answers  INT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE mistakes (
  id              BIGSERIAL PRIMARY KEY,
  topic_id        BIGINT NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  subtopic_id     BIGINT NOT NULL REFERENCES subtopics(id),
  belief          TEXT NOT NULL,
  correction      TEXT NOT NULL,
  source          TEXT NOT NULL CHECK (source IN ('seed', 'quiz', 'chat', 'mock')),
  status          TEXT NOT NULL CHECK (status IN ('watching', 'active', 'resolved')),
  stage           INT NOT NULL DEFAULT 0,
  due_at          TIMESTAMPTZ,
  needs_questions BOOLEAN NOT NULL DEFAULT false,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mistakes_topic_belief ON mistakes (topic_id, lower(belief));

CREATE TABLE questions (
  id          BIGSERIAL PRIMARY KEY,
  topic_id    BIGINT NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  subtopic_id BIGINT NOT NULL REFERENCES subtopics(id),
  kind        TEXT NOT NULL CHECK (kind IN ('single', 'multi')),
  stem        TEXT NOT NULL,
  stem_key    TEXT NOT NULL,
  code        TEXT NOT NULL DEFAULT '',
  code_lang   TEXT NOT NULL DEFAULT '',
  difficulty  INT NOT NULL CHECK (difficulty BETWEEN 1 AND 10),
  b           DOUBLE PRECISION NOT NULL,
  n_answers   INT NOT NULL DEFAULT 0,
  status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'flagged')),
  options     JSONB NOT NULL,
  mistake_ids BIGINT[] NOT NULL DEFAULT '{}',
  job_id      BIGINT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (topic_id, stem_key)
);

CREATE TABLE quizzes (
  id            BIGSERIAL PRIMARY KEY,
  topic_id      BIGINT NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  length        INT NOT NULL,
  target        DOUBLE PRECISION NOT NULL,
  started_at    TIMESTAMPTZ NOT NULL,
  finished_at   TIMESTAMPTZ,
  skill_before  DOUBLE PRECISION NOT NULL,
  mistake_slots INT[] NOT NULL DEFAULT '{}'
);

CREATE TABLE answers (
  id              BIGSERIAL PRIMARY KEY,
  quiz_id         BIGINT NOT NULL REFERENCES quizzes(id) ON DELETE CASCADE,
  position        INT NOT NULL,
  question_id     BIGINT NOT NULL REFERENCES questions(id),
  slot            TEXT NOT NULL CHECK (slot IN ('rating', 'mistake')),
  slot_mistake_id BIGINT REFERENCES mistakes(id),
  option_order    INT[] NOT NULL,
  shown_at        TIMESTAMPTZ NOT NULL,
  answered_at     TIMESTAMPTZ,
  chosen          INT[],
  correct         BOOLEAN,
  is_repeat       BOOLEAN NOT NULL,
  p               DOUBLE PRECISION NOT NULL,
  theta_before    DOUBLE PRECISION,
  theta_after     DOUBLE PRECISION,
  b_before        DOUBLE PRECISION,
  b_after         DOUBLE PRECISION,
  UNIQUE (quiz_id, position)
);
CREATE INDEX answers_question_shown ON answers (question_id, shown_at);

CREATE TABLE mistake_events (
  id          BIGSERIAL PRIMARY KEY,
  mistake_id  BIGINT NOT NULL REFERENCES mistakes(id) ON DELETE CASCADE,
  answer_id   BIGINT REFERENCES answers(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL CHECK (kind IN ('hit', 'clear')),
  happened_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX mistake_events_mistake ON mistake_events (mistake_id);

CREATE TABLE jobs (
  id          BIGSERIAL PRIMARY KEY,
  status      TEXT NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed')),
  request     JSONB NOT NULL,
  result      JSONB,
  error       TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at  TIMESTAMPTZ,
  finished_at TIMESTAMPTZ
);

CREATE TABLE chat_messages (
  id         BIGSERIAL PRIMARY KEY,
  role       TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
  text       TEXT NOT NULL,
  job_id     BIGINT REFERENCES jobs(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value JSONB NOT NULL
);
