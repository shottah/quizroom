import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { api, message, unwrap, type S } from "../api/client";
import { useChat, useChatAside, useSetChatPage } from "../chatContext";
import { InlineText, RichText } from "../components/RichText";
import { letter, useGenerate, useStartQuiz, useStatus } from "../hooks";
import { dateText, dueText } from "../lib/due";
import { returnLine } from "../lib/mistakes";
import { eloDelta, eloText, levelChange, skillNote } from "../lib/skill";
import { endedEarly, scoreSentence, sentence } from "../lib/summary";
import { askWhy, cleanStem, letterList, optionTag, verdict, type Kind } from "../lib/verdict";

const PICK_ALL = "Pick all that apply. It only counts as right if you pick every correct option and no wrong ones.";
const SCHEDULE =
  "How mistakes come back: as a new question in your next quiz, then 1, 3, 7 and 21 days after each right answer. Five checks in a row and it's done.";
const SCHEDULE_SEEN = "quizroom.schedule-explained";

function Dots({ position, length }: { position: number; length: number }) {
  return (
    <span className="dots" aria-label={`question ${position} of ${length}`}>
      {Array.from({ length }, (_, i) => (
        <span key={i} className={i + 1 < position ? "dot done" : i + 1 === position ? "dot now" : "dot"} />
      ))}
    </span>
  );
}

type Feedback = S["OptionFeedback"];

function optionClass(o: Feedback, kind: Kind): string {
  if (o.correct && !o.chosen && kind === "multi") return "opt correct missed";
  if (o.correct) return "opt correct";
  if (o.chosen) return "opt chosen-wrong";
  return "opt dim";
}

/** Options after answering, in the order shown, with tags, traps and explanations. */
function AnsweredOptions({ options, kind }: { options: Feedback[]; kind: Kind }) {
  return (
    <ul className="options answered">
      {options.map((o, i) => {
        const tag = optionTag(o, kind);
        return (
          <li key={o.id} className={optionClass(o, kind)}>
            <div className="opt-line">
              <span className="mark" aria-hidden="true">
                {o.correct ? "✓" : o.chosen ? "✗" : ""}
              </span>
              <span className="letter">{letter(i)}</span>
              <span className="opt-text">
                <InlineText text={o.text} />
              </span>
              {tag ? <span className={tag === "you missed this" ? "tag missed" : "tag"}>{tag}</span> : null}
            </div>
            {o.mistake && !o.chosen ? (
              <p className="trap">
                {o.on_list ? "Trap from your list: " : "Trap: "}
                <InlineText text={o.mistake} />
              </p>
            ) : null}
            <RichText text={o.explanation} className="expl" />
          </li>
        );
      })}
    </ul>
  );
}

function MistakeBox({ result, topic }: { result: S["Result"]; topic: string }) {
  const options = result.options ?? [];
  const hits = result.mistakes_hit ?? [];
  const moved = result.moved_on ?? [];
  const [explain] = useState(() => hits.length > 0 && localStorage.getItem(SCHEDULE_SEEN) === null);
  useEffect(() => {
    if (explain) localStorage.setItem(SCHEDULE_SEEN, "1");
  }, [explain]);
  if (hits.length === 0 && moved.length === 0) return null;
  const now = new Date();
  return (
    <div className="mistake-box">
      {hits.map((m) => {
        const i = options.findIndex((o) => o.chosen && o.mistake === m.belief);
        return (
          <div key={m.belief} className="mistake-note hit">
            <p>
              {m.new ? "New on your mistake list: " : `You made a mistake from your list${i >= 0 ? ` (option ${letter(i)})` : ""}: `}
              <InlineText text={sentence(m.belief)} />
            </p>
            <p className="remember">
              Remember: <InlineText text={sentence(m.correction)} />
            </p>
            <p className="small muted">It's due again now: a coming {topic} quiz retests it with a new question.</p>
          </div>
        );
      })}
      {explain ? <p className="small muted">{SCHEDULE}</p> : null}
      {moved.map((m) => (
        <div key={m.belief} className="mistake-note moved">
          {m.done ? (
            <p>
              Done: <InlineText text={sentence(m.belief)} /> You've got this one.
            </p>
          ) : (
            <p>
              Progress: <InlineText text={sentence(m.belief)} /> Next check from {dueText(m.next_due, now)} (check {m.next_check} of 5).
            </p>
          )}
        </div>
      ))}
    </div>
  );
}

function FeedbackView({
  result,
  kind,
  topic,
  onFlag,
  flagged,
  onNext,
}: {
  result: S["Result"];
  kind: Kind;
  topic: string;
  onFlag: () => void;
  flagged: boolean;
  onNext: () => void;
}) {
  const { prefill } = useChat();
  const nextRef = useRef<HTMLButtonElement>(null);
  const options = result.options ?? [];
  // Keep Enter, Enter working: focus Next without scrolling (the card scrolls
  // to its top), and lift the chat button above the sticky bar meanwhile.
  useEffect(() => {
    nextRef.current?.focus({ preventScroll: true });
    document.body.classList.add("feedback-open");
    return () => document.body.classList.remove("feedback-open");
  }, []);
  const avoided = options.filter((o) => o.mistake && o.on_list && !o.chosen).length;
  const move = { eloBefore: result.elo_before, eloAfter: result.elo_after, answersInTopic: result.answers_in_topic };
  const note = skillNote({ ...move, p: result.p, correct: result.correct }, topic);
  const level = levelChange(result.level_before, result.level, topic);
  return (
    <div className="feedback">
      <p className={result.correct ? "verdict right" : "verdict wrong"}>{verdict(options, kind)}</p>
      <MistakeBox result={result} topic={topic} />
      {result.correct && avoided > 0 ? <p className="avoided">You avoided {avoided} of your mistakes.</p> : null}
      <AnsweredOptions options={options} kind={kind} />
      <div className="skill-line">
        <p>
          <strong>
            Elo {result.elo_before} → {result.elo_after} ({eloDelta(result.elo_before, result.elo_after)})
          </strong>
          {note ? ` ${note}` : ""}
        </p>
        {level ? <p className="level-change">{level}</p> : null}
      </div>
      <div className="feedback-tools">
        <button className="chip" onClick={() => prefill(askWhy(options, kind))}>
          Ask Claude why
        </button>
        {flagged ? (
          <p className="small muted">Removed. You won't see it again, and any mistake it recorded was undone.</p>
        ) : (
          <button className="link quiet" onClick={onFlag}>
            Wrong answer key or unclear? Remove this question
          </button>
        )}
      </div>
      <div className="sticky-bar">
        <span className={result.correct ? "bar-verdict right" : "bar-verdict wrong"}>{result.correct ? "Right" : "Not quite"}</span>
        <span className="skill-change">
          Elo {result.elo_before} → {result.elo_after}
        </span>
        <button className="primary" ref={nextRef} onClick={onNext}>
          {result.finished ? "See results" : "Next"}
        </button>
      </div>
    </div>
  );
}

function QuestionView({ quizId, shown }: { quizId: number; shown: S["Shown"] }) {
  const qc = useQueryClient();
  const q = shown.question!;
  const [chosen, setChosen] = useState<number[]>([]);
  const [result, setResult] = useState<S["Result"] | null>(null);
  const cardRef = useRef<HTMLElement>(null);
  const multi = q.kind === "multi";

  useSetChatPage(
    result
      ? { page: "feedback", topic: q.topic_slug, question_id: q.id, chosen, order: (q.options ?? []).map((o) => o.id) }
      : { page: "quiz", topic: q.topic_slug, question_id: 0, chosen: [] },
  );

  const isCheck = shown.kind === "check";
  const picked = letterList((q.options ?? []).flatMap((o, i) => (chosen.includes(o.id) ? [letter(i)] : [])));
  useChatAside(
    result ? `${q.topic} › ${q.subtopic} · your answer ${picked}` : "",
    isCheck
      ? "Ask Claude anything. It can't see this check's questions."
      : result
        ? "Ask about this question. Claude sees it and your answer…"
        : "Answer first, then ask Claude about this question.",
  );

  useEffect(() => {
    if (result) cardRef.current?.scrollIntoView({ block: "start", behavior: "smooth" });
  }, [result]);

  const answer = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/api/quizzes/{id}/answers", { params: { path: { id: quizId } }, body: { position: shown.position, chosen } })),
    // A skill check shows no feedback: the next question loads straight away.
    onSuccess: (r) => (isCheck ? void qc.invalidateQueries({ queryKey: ["next", quizId] }) : setResult(r)),
  });
  const flag = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/questions/{id}/flag", { params: { path: { id: q.id } } })),
  });

  const toggle = (id: number) =>
    setChosen((c) => (multi ? (c.includes(id) ? c.filter((x) => x !== id) : [...c, id]) : [id]));

  return (
    <article className="card quiz-card" ref={cardRef}>
      <header className="quiz-head">
        <span className="crumb">
          {q.topic} › {q.subtopic}
        </span>
        <span className="muted small">
          {isCheck ? `Skill check · question ${shown.position} · up to ${shown.length}` : `question ${shown.position} of ${shown.length}`}
        </span>
        <Dots position={shown.position} length={shown.length} />
      </header>
      <p className="sr-only" role="status" aria-live="polite">
        {result ? verdict(result.options ?? [], multi ? "multi" : "single") : ""}
      </p>
      {shown.thin && !result && !isCheck ? (
        <p className="note">
          This one is outside your usual range because {q.topic} is short of questions at your level. Your skill takes that into
          account.
        </p>
      ) : null}
      <RichText text={cleanStem(q.stem, multi ? "multi" : "single")} className="stem" />
      {q.code ? (
        <pre className="code">
          <code data-lang={q.code_lang}>{q.code}</code>
        </pre>
      ) : null}
      {result ? (
        <FeedbackView
          result={result}
          kind={multi ? "multi" : "single"}
          topic={q.topic}
          flagged={flag.isSuccess}
          onFlag={() => flag.mutate()}
          onNext={() => void qc.invalidateQueries({ queryKey: ["next", quizId] })}
        />
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (chosen.length > 0) answer.mutate();
          }}
        >
          {multi ? <p className="pill">{PICK_ALL}</p> : null}
          <ul className={multi ? "options multi" : "options"}>
            {(q.options ?? []).map((o, i) => (
              <li key={o.id}>
                <label className={chosen.includes(o.id) ? "opt pick selected" : "opt pick"}>
                  <input
                    type={multi ? "checkbox" : "radio"}
                    name="option"
                    checked={chosen.includes(o.id)}
                    onChange={() => toggle(o.id)}
                  />
                  <span className="letter">{letter(i)}</span>
                  <span className="opt-text">
                    <InlineText text={o.text} />
                  </span>
                </label>
              </li>
            ))}
          </ul>
          {answer.error ? <p className="error">{message(answer.error)}</p> : null}
          <div className="actions">
            <p className="small muted leave">
              <a href="#/">{isCheck ? "Leave check" : "Leave quiz"}</a> · Your answers so far are saved. Resume from Topics.
            </p>
            <button className="primary" type="submit" disabled={chosen.length === 0 || answer.isPending}>
              {isCheck ? "Answer and continue" : "Check answer"}
            </button>
          </div>
        </form>
      )}
      {flag.error ? <p className="error">{message(flag.error)}</p> : null}
    </article>
  );
}

function ReviewStrip({ items }: { items: S["ReviewItem"][] }) {
  const firstWrong = items.find((r) => !r.correct)?.position ?? null;
  const [open, setOpen] = useState<number | null>(firstWrong);
  const [removed, setRemoved] = useState<number[]>([]);
  const flag = useMutation({
    mutationFn: async (id: number) => unwrap(await api.POST("/api/questions/{id}/flag", { params: { path: { id } } })),
    onSuccess: (_, id) => setRemoved((r) => [...r, id]),
  });
  const item = items.find((r) => r.position === open);
  if (items.length === 0) return null;
  return (
    <section className="review">
      <h3>Review your answers</h3>
      <div className="strip" role="tablist" aria-label="Questions in this quiz">
        {items.map((r) => (
          <button
            key={r.position}
            role="tab"
            aria-selected={r.position === open}
            className={`strip-cell ${r.correct ? "right" : "wrong"}${r.position === open ? " open" : ""}`}
            onClick={() => setOpen(r.position === open ? null : r.position)}
            title={`Question ${r.position}: ${r.correct ? "right" : "wrong"}`}
          >
            <span aria-hidden="true">{r.correct ? "✓" : "✗"}</span> {r.position}
          </button>
        ))}
      </div>
      {item ? (
        <div className="review-item">
          <p className="crumb">
            Question {item.position} · {item.subtopic}
          </p>
          <RichText text={cleanStem(item.stem, item.kind)} className="stem" />
          {item.code ? (
            <pre className="code">
              <code data-lang={item.code_lang}>{item.code}</code>
            </pre>
          ) : null}
          <p className={item.correct ? "verdict right" : "verdict wrong"}>{verdict(item.options ?? [], item.kind)}</p>
          <AnsweredOptions options={item.options ?? []} kind={item.kind} />
          {removed.includes(item.question_id) ? (
            <p className="small muted">Removed. You won't see it again, and any mistake it recorded was undone.</p>
          ) : (
            <button className="link quiet" disabled={flag.isPending} onClick={() => flag.mutate(item.question_id)}>
              Wrong answer key or unclear? Remove this question
            </button>
          )}
          {flag.error ? <p className="error">{message(flag.error)}</p> : null}
        </div>
      ) : (
        <p className="small muted">Pick a question to see it again with every explanation.</p>
      )}
    </section>
  );
}

/** What a skill check measured, and how it compares with the last one. */
function CheckResultView({ s }: { s: S["Summary"] }) {
  const c = s.check;
  if (!c) return <p className="muted">This check ended before it could measure anything.</p>;
  const missed = c.missed_in ?? [];
  return (
    <div className="check-result">
      <div className="skill-row">
        <span className="elo-label">Elo</span>
        <span className="skill-num">{c.elo}</span>
        <span className="skill-of">± {c.margin}</span>
        <span className={`badge level-${c.level.toLowerCase()}`}>{c.level}</span>
      </div>
      {c.previous ? (
        <p>
          {eloDelta(c.previous.elo, c.elo)} since your check on {dateText(c.previous.at)} ({eloText(c.previous.elo, c.previous.margin)}).
        </p>
      ) : (
        <p>Your first skill check in {s.topic}.</p>
      )}
      <p className="muted small">
        {s.right} of {s.answered} right. There's about a 2 in 3 chance your real Elo is within ± {c.margin}.
      </p>
      <p>{missed.length > 0 ? `Missed in: ${missed.map((m) => `${m.subtopic} (${m.count})`).join(", ")}.` : "No misses."}</p>
    </div>
  );
}

function QuizSummary({ quizId }: { quizId: number }) {
  const sum = useQuery({
    queryKey: ["quiz", quizId],
    queryFn: async () => unwrap(await api.GET("/api/quizzes/{id}", { params: { path: { id: quizId } } })),
  });
  useSetChatPage({ page: "quiz", topic: sum.data?.topic_slug ?? "", question_id: 0, chosen: [] });
  const start = useStartQuiz();
  const gen = useGenerate();
  const status = useStatus();
  if (sum.isPending) return <p className="muted">Loading…</p>;
  if (sum.error) return <p className="error">{message(sum.error)}</p>;
  const s = sum.data;
  const now = new Date();
  const cameBack = s.came_back ?? [];
  const movedOn = s.moved_on ?? [];
  const short = s.bank_state === "offlevel" || s.bank_state === "blocked";
  const signedOut = status.data?.auth === "none";
  const level = levelChange(s.level_before, s.level, s.topic);
  return (
    <article className="card summary-card">
      <h2>
        {s.topic} {s.kind === "check" ? "skill check" : "quiz"} complete
      </h2>
      {s.kind === "check" ? <CheckResultView s={s} /> : null}
      {s.kind === "check" ? null : (
      <div className="tiles">
        <div className="tile">
          <div className="tile-big">
            {s.right} of {s.answered}
          </div>
          <div className="tile-small">predicted {s.expected.toFixed(1)}</div>
        </div>
        <div className="tile">
          <div className="tile-big">
            Elo {s.elo_before} → {s.elo_after}
          </div>
          <div className="tile-small">
            {eloDelta(s.elo_before, s.elo_after)} · {s.level}
          </div>
        </div>
        <div className="tile">
          <div className="tile-big">{cameBack.length} came back</div>
          <div className="tile-small">{movedOn.length} moved on</div>
        </div>
      </div>
      )}
      {s.kind === "check" ? null : <p>{scoreSentence(s.answered, s.right, s.expected, s.elo_before, s.elo_after, s.off_level)}</p>}
      {level ? <p className="level-change">{level}</p> : null}
      {s.off_level > 0 ? (
        <p className="muted">
          {s.off_level} of these {s.answered} questions were outside your usual range because {s.topic} was short of questions at your
          level.
        </p>
      ) : null}
      {endedEarly(s.kind, s.answered, s.length, s.margin_after, s.topic) ? (
        <p className="muted">{endedEarly(s.kind, s.answered, s.length, s.margin_after, s.topic)}</p>
      ) : null}
      {cameBack.length > 0 ? (
        <section className="mistake-list">
          <h3>Came back</h3>
          <ul>
            {cameBack.map((m) => (
              <li key={m.belief}>
                <InlineText text={sentence(m.belief)} />
                <span className="remember">
                  {" "}
                  Remember: <InlineText text={sentence(m.correction)} />
                </span>
                <span className="muted small"> {returnLine(m, now)}</span>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
      {movedOn.length > 0 ? (
        <section className="mistake-list">
          <h3>Moved on</h3>
          <ul>
            {movedOn.map((m) => (
              <li key={m.belief}>
                <InlineText text={sentence(m.belief)} />
                <span className="muted small"> {returnLine(m, now)}</span>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
      <ReviewStrip items={s.review ?? []} />
      <div className="actions summary-actions">
        <a className="button secondary" href="#/">
          Back to topics
        </a>
        <a className="button secondary" href={`#/topic/${s.topic_slug}`}>
          See {s.topic} progress
        </a>
        {s.bank_state === "offlevel" ? (
          <button className="secondary" disabled={start.isPending} onClick={() => start.mutate(s.topic_slug)}>
            {s.kind === "check" ? "Start a practice quiz anyway" : "Start another anyway"}
          </button>
        ) : null}
        {short ? (
          signedOut ? (
            <a className="button primary" href="#/settings">
              Sign Claude in to write more
            </a>
          ) : (
            <button className="primary" disabled={gen.busy} onClick={() => gen.mutate(s.topic_slug)}>
              Write 15 at my level
            </button>
          )
        ) : (
          <button className="primary" disabled={start.isPending} onClick={() => start.mutate(s.topic_slug)}>
            {s.kind === "check" ? `Start a ${s.topic} quiz` : `Start another ${s.topic} quiz`}
          </button>
        )}
      </div>
      {start.error ? <p className="error">{message(start.error)}</p> : null}
      {gen.error ? <p className="error">{message(gen.error)}</p> : null}
    </article>
  );
}

export function QuizPage({ id }: { id: number }) {
  const next = useQuery({
    queryKey: ["next", id],
    queryFn: async () => unwrap(await api.GET("/api/quizzes/{id}/next", { params: { path: { id } } })),
  });
  return (
    <main className="page narrow">
      {next.isPending ? <p className="muted">Loading…</p> : null}
      {next.error ? <p className="error">{message(next.error)}</p> : null}
      {next.data?.done ? <QuizSummary quizId={id} /> : null}
      {next.data && !next.data.done && next.data.question ? (
        <QuestionView key={`${next.data.position}-${next.data.question.id}`} quizId={id} shown={next.data} />
      ) : null}
    </main>
  );
}
