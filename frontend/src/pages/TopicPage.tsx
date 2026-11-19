import { useQuery } from "@tanstack/react-query";
import { api, isDown, message, unwrap, visibleError, type S } from "../api/client";
import { useChat, useSetChatPage } from "../chatContext";
import { BankMeter } from "../components/BankMeter";
import { MistakeList } from "../components/MistakeList";
import { SkillChart } from "../components/SkillChart";
import { plural, useGenerate, useStartQuiz, useStatus } from "../hooks";
import { bankNote } from "../lib/bank";
import { dateText } from "../lib/due";
import { nextStep, type Step } from "../lib/next";
import { eloDelta, eloText, levelRange, measured, signed } from "../lib/skill";
import { recentReading } from "../lib/summary";

const START_ELO = 17;
const WRITE_HINT = "About 2 min · uses your Claude plan";

function SkillHelp() {
  return (
    <details>
      <summary>What do Elo, the margin and levels mean?</summary>
      <div className="help">
        <p>
          Elo is your level in this topic on a 0–300 scale. At Elo 160 you'd get about 3 in 4 single-answer questions pitched
          at 160 right. It moves after every answer: up when you beat the prediction, down when you miss one you were expected to get.
        </p>
        <p>
          The ± margin is how sure Quizroom is: there's about a 2 in 3 chance your real level is within it. It narrows as you answer,
          widens while you're away, and a skill check narrows it fastest. It never goes below ± 27, because your number always wobbles
          a little around your real level.
        </p>
        <p>Levels are names for ranges:</p>
        <ul>
          <li>Beginner, 0–99: which component does what.</li>
          <li>Intermediate, 100–199: predict what a manifest, Dockerfile or command does.</li>
          <li>Advanced, 200–300: troubleshoot from symptoms across components, at senior or SRE interview depth.</li>
        </ul>
        <p>Levels never change which questions you get.</p>
      </div>
    </details>
  );
}

export function TopicPage({ slug }: { slug: string }) {
  useSetChatPage({ page: "topic", topic: slug, question_id: 0, chosen: [] });
  const detail = useQuery({
    queryKey: ["topic", slug],
    queryFn: async () => unwrap(await api.GET("/api/topics/{slug}", { params: { path: { slug } } })),
  });
  const status = useStatus();
  const start = useStartQuiz();
  const check = useStartQuiz("check");
  const gen = useGenerate();
  const { prefill } = useChat();
  if (detail.isPending)
    return (
      <main className="page">
        <p className="muted">Loading…</p>
      </main>
    );
  if (detail.error)
    return (
      <main className="page">
        {visibleError(detail.error, isDown(status.error)) ? <p className="error">{visibleError(detail.error, isDown(status.error))}</p> : null}
      </main>
    );

  const d = detail.data;
  const b = d.bank;
  const now = new Date();
  const mistakes = d.mistakes ?? [];
  const subtopics = d.subtopics ?? [];
  const quizzes = d.quizzes ?? [];
  const weak = subtopics.filter((s) => s.weak).sort((x, y) => x.right - x.expected - (y.right - y.expected));
  const due = mistakes.filter((m) => m.status === "active" && m.due_at !== null && Date.parse(m.due_at) <= now.getTime()).length;
  const lastDone = quizzes.find((q) => q.finished);
  const open = quizzes[0] && !quizzes[0].finished ? quizzes[0] : null;
  const yours = (sub: string) => mistakes.filter((m) => m.subtopic === sub && m.status !== "watching").length;
  const askFor = (name: string) => prefill(`Write 10 questions on ${name} in ${d.name}, around my level.`);
  const step = nextStep({ auth: status.data?.auth ?? "", bank: b, mistakesDue: due, weak, topic: d.name, margin: d.margin });
  const checks = d.checks ?? [];
  const note = bankNote(b, d.name);
  const tried = subtopics.filter((s) => s.answered > 0);
  const untried = subtopics.filter((s) => s.answered === 0);

  const writeButton = (
    <button className="primary" disabled={gen.busy} title={gen.busy ? "Claude is busy with your last request." : undefined} onClick={() => gen.mutate(slug)}>
      {b.questions === 0 ? "Write my first 30 questions" : "Write 15 at my level"}
    </button>
  );
  const action = (s: Step) => {
    switch (s.kind) {
      case "signin":
        return (
          <a className="button primary" href="#/settings">
            How to sign Claude in
          </a>
        );
      case "wait":
        return <p className="note">You can keep using Quizroom while Claude writes.</p>;
      case "write":
      case "write-then-quiz":
        return (
          <div className="action-row">
            {writeButton}
            <span className="hint small">{WRITE_HINT}</span>
            {s.kind === "write-then-quiz" ? (
              <button className="link" disabled={start.isPending} onClick={() => start.mutate(slug)}>
                Start quiz anyway
              </button>
            ) : null}
          </div>
        );
      case "check":
        return (
          <div className="action-row">
            <button className="primary" disabled={check.isPending} onClick={() => check.mutate(slug)}>
              Take a skill check
            </button>
            <span className="hint small">10–20 questions · answers shown at the end</span>
            <button className="link" disabled={start.isPending} onClick={() => start.mutate(slug)}>
              or start a quiz
            </button>
          </div>
        );
      case "weak":
        return (
          <div className="action-row">
            <button className="chip" onClick={() => askFor(weak[0].name)}>
              {s.text}
            </button>
            <button className="link" disabled={start.isPending} onClick={() => start.mutate(slug)}>
              or start a quiz
            </button>
          </div>
        );
      default:
        return (
          <button className="primary" disabled={start.isPending} onClick={() => start.mutate(slug)}>
            Start quiz · {b.quiz_length} questions
          </button>
        );
    }
  };

  return (
    <main className="page">
      <header className="page-head">
        <h1>{d.name}</h1>
        <p className="muted">{d.description}</p>
      </header>

      <div className="topic-top">
        <section className="card stand">
          <h2>Where you stand</h2>
          <div className="skill-row">
            <span className="elo-label">Elo</span>
            <span className="skill-num">{d.elo}</span>
            <span className="skill-of">± {d.margin}</span>
            <span className={`badge level-${d.level.toLowerCase()}`}>{d.level}</span>
            <span className="muted small">({levelRange(d.level)})</span>
          </div>
          {lastDone ? (
            <p>
              Last quiz {lastDone.elo_before} → {lastDone.elo_after} ({eloDelta(lastDone.elo_before, lastDone.elo_after)})
              · since you started {START_ELO} → {d.elo} ({eloDelta(START_ELO, d.elo)})
            </p>
          ) : (
            <p className="muted">No finished quizzes yet. Everyone starts at Elo {START_ELO}.</p>
          )}
          {d.recent.answered > 0 ? (
            <p>
              Last {plural(d.recent.answered, "answer", "answers")}: {d.recent.right} right, {d.recent.expected.toFixed(1)} predicted.{" "}
              {recentReading(d.recent.answered, d.recent.right, d.recent.expected)}
            </p>
          ) : null}
          <SkillHelp />
        </section>

        <section className="card next-card">
          <h2>Do this next</h2>
          {open ? (
            <>
              <p className="next-text">Finish your quiz</p>
              <p className="muted">
                You've answered {open.answered} of {open.length}. Your answers so far are saved.
              </p>
              <a className="button primary" href={`#/quiz/${open.id}`}>
                Resume quiz · {open.answered} of {open.length} done
              </a>
            </>
          ) : (
            <>
              <p className="next-text">{step.text}</p>
              <p className="muted">{step.why}</p>
              {action(step)}
            </>
          )}
          {start.error ? <p className="error">{message(start.error)}</p> : null}
          {gen.error ? <p className="error">{message(gen.error)}</p> : null}
        </section>
      </div>

      <section className="card">
        <h2>Elo over time</h2>
        <SkillChart history={d.history ?? []} checks={checks} />
      </section>

      <section className="card">
        <h2>Weak spots</h2>
        {weak.length === 0 ? (
          <p className="muted">
            No weak spots yet. A subtopic shows here once you've answered 5 of its questions and scored below prediction.
          </p>
        ) : (
          <ul className="weak-list">
            {weak.map((w) => (
              <li key={w.slug}>
                <p>
                  <strong>{w.name}</strong>: {w.right} of {w.answered} right · predicted {w.expected.toFixed(1)} ·{" "}
                  {plural(yours(w.slug), "mistake", "mistakes")} on your list
                </p>
                <button className="chip" onClick={() => askFor(w.name)}>
                  Ask Claude for 10 {w.name} questions
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="card">
        <h2>Mistakes</h2>
        <MistakeList mistakes={mistakes} subtopics={subtopics} />
      </section>

      <section className="card">
        <h2>Subtopics</h2>
        {tried.length === 0 ? (
          <p className="muted">You haven't answered any {d.name} questions yet.</p>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Subtopic</th>
                <th>Answered</th>
                <th title="Right answers minus the predicted number">Right vs predicted</th>
                <th>Your mistakes</th>
              </tr>
            </thead>
            <tbody>
              {tried.map((s) => {
                const gap = s.right - s.expected;
                return (
                  <tr key={s.slug} className={s.weak ? "weak" : ""}>
                    <td>
                      {s.name} {s.weak ? <span className="badge weak-badge">weak spot</span> : null}
                    </td>
                    <td>{s.answered}</td>
                    <td className={gap > 0.05 ? "gap-up" : gap < -0.05 ? "gap-down" : ""}>{signed(gap)}</td>
                    <td>{yours(s.slug)}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
        {untried.length > 0 ? <p className="small muted">Not tried yet: {untried.map((s) => s.name).join(", ")}.</p> : null}
      </section>

      <section className="card">
        <h2>Question bank</h2>
        {b.questions === 0 ? (
          <p className="muted">No questions yet. Claude writes your first 30, from easy to Intermediate, in about 5 minutes.</p>
        ) : (
          <>
            <BankMeter bank={b} label={false} />
            {note.text ? <p className={`bank-note tone-${note.tone}`}>{note.text}</p> : null}
            <p className="small muted">
              {plural(b.questions, "question", "questions")} in all. Each quiz picks from the ones you haven't seen in the last 7 days,
              aiming at your level.
            </p>
          </>
        )}
      </section>

      <section className="card">
        <h2>Skill checks</h2>
        {checks.length === 0 ? (
          <p className="muted">
            No skill checks yet. A check measures your Elo in 10–20 questions and narrows the margin.
            {!measured(d.margin) ? " Yours is still a rough estimate." : ""}
          </p>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Date</th>
                <th>Elo</th>
                <th>Questions</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {checks.map((c) => (
                <tr key={c.quiz_id}>
                  <td>{dateText(c.at)}</td>
                  <td>{eloText(c.elo, c.margin)}</td>
                  <td>{c.answered}</td>
                  <td>
                    <a href={`#/quiz/${c.quiz_id}`}>Result</a>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {b.available >= 20 && step.kind !== "check" ? (
          <button className="secondary" disabled={check.isPending} onClick={() => check.mutate(slug)}>
            Take a skill check
          </button>
        ) : null}
        {check.error ? <p className="error">{message(check.error)}</p> : null}
      </section>

      <section className="card">
        <h2>Recent quizzes</h2>
        {quizzes.length === 0 ? (
          <p className="muted">No quizzes yet.</p>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Started</th>
                <th>Score</th>
                <th>Elo</th>
                <th>Mistakes</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {quizzes.map((q: S["QuizRow"]) => (
                <tr key={q.id}>
                  <td>
                    {dateText(q.started_at)}
                    {q.kind === "check" ? <span className="badge kind-badge">skill check</span> : null}
                  </td>
                  <td>
                    {q.right}/{q.answered} · predicted {q.expected.toFixed(1)}
                  </td>
                  <td>
                    {q.elo_before} → {q.elo_after}
                  </td>
                  <td>
                    {q.came_back} came back, {q.moved_on} moved on
                  </td>
                  <td>
                    <a href={`#/quiz/${q.id}`}>{q.finished ? "Review" : "Resume"}</a>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </main>
  );
}
