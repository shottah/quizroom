import { useQuery } from "@tanstack/react-query";
import { api, isDown, message, unwrap, visibleError, type S } from "../api/client";
import { homePage, useSetChatPage } from "../chatContext";
import { BankMeter } from "../components/BankMeter";
import { Sparkline } from "../components/Sparkline";
import { plural, useGenerate, useSettings, useStartQuiz, useStatus } from "../hooks";
import { bankNote } from "../lib/bank";
import { nextStep, nextTopic } from "../lib/next";
import { aimText, eloDelta, levelRange, measured } from "../lib/skill";

const WRITE_HINT = "About 2 min · uses your Claude plan";

function SignedOut() {
  return (
    <p className="note">
      Claude isn't signed in, so it can't write questions. <a href="#/settings">How to fix →</a>
    </p>
  );
}

function mistakesLine(due: number): string {
  if (due === 0) return "";
  if (due <= 3) return `Next quiz retests ${plural(due, "mistake", "mistakes")}.`;
  return `${due} mistakes waiting · your next quiz retests up to 3.`;
}

function TopicCard({ card, recommended, signedOut }: { card: S["Card"]; recommended: boolean; signedOut: boolean }) {
  const start = useStartQuiz();
  const check = useStartQuiz("check");
  const gen = useGenerate();
  const b = card.bank;
  const main = recommended ? "primary" : "secondary";
  const busyTitle = gen.busy ? "Claude is busy with your last request." : undefined;
  const startQuiz = () => start.mutate(card.slug);

  const write = (label: string) =>
    signedOut ? (
      <SignedOut />
    ) : b.writing ? null : (
      <div className="action-row">
        <button className={main} disabled={gen.busy} title={busyTitle} onClick={() => gen.mutate(card.slug)}>
          {label}
        </button>
        <span className="hint small">{WRITE_HINT}</span>
      </div>
    );

  if (card.empty) {
    return (
      <article className={`card topic-card${recommended ? " recommended" : ""}`}>
        <header className="card-head">
          <a className="card-title" href={`#/topic/${card.slug}`}>
            {card.name}
          </a>
        </header>
        {b.writing ? (
          <p className="note">Claude is writing your first {card.name} questions… usually about 5 minutes.</p>
        ) : (
          <>
            <p>No questions yet. Claude writes your first 30, from easy to Intermediate, in about 5 minutes. Uses your Claude plan.</p>
            {signedOut ? (
              <SignedOut />
            ) : (
              <button className={main} disabled={gen.busy} title={busyTitle} onClick={() => gen.mutate(card.slug)}>
                Write my first 30 questions
              </button>
            )}
          </>
        )}
        {gen.error ? <p className="error">{message(gen.error)}</p> : null}
      </article>
    );
  }

  const note = bankNote(b, card.name);
  const len = b.quiz_length;
  let actions;
  if (card.open_quiz) {
    const q = card.open_quiz;
    actions = (
      <div className="action-row">
        <a className={`button ${main}`} href={`#/quiz/${q.id}`}>
          Resume quiz · {q.answered} of {q.length} done
        </a>
        {b.state !== "blocked" ? (
          <button className="link" disabled={start.isPending} onClick={startQuiz}>
            Start a new quiz instead
          </button>
        ) : null}
      </div>
    );
  } else if (b.state === "plenty" || b.state === "low") {
    actions = (
      <div className="action-row">
        <button className={main} disabled={start.isPending} onClick={startQuiz}>
          Start quiz · {len} questions
        </button>
        {b.state === "low" && !signedOut && !b.writing ? (
          <button className="link" disabled={gen.busy} title={busyTitle} onClick={() => gen.mutate(card.slug)}>
            Top up: write 15 more
          </button>
        ) : null}
      </div>
    );
  } else if (b.state === "offlevel") {
    actions = (
      <>
        {write("Write 15 at my level")}
        <button className="link start-anyway" disabled={start.isPending} onClick={startQuiz}>
          Start quiz anyway
        </button>
      </>
    );
  } else {
    actions = (
      <>
        {write("Write 15 at my level")}
        <button className="secondary" disabled title="No quiz right now: see the note above">
          Start quiz
        </button>
      </>
    );
  }

  const last = card.last_quiz;
  return (
    <article className={`card topic-card${recommended ? " recommended" : ""}`}>
      <header className="card-head">
        <a className="card-title" href={`#/topic/${card.slug}`}>
          {card.name}
        </a>
        <a className="small" href={`#/topic/${card.slug}`}>
          See progress →
        </a>
      </header>
      <div className="skill-row">
        <span className="elo-label">Elo</span>
        <span className="skill-num">{card.elo}</span>
        <span className="skill-of" title="About a 2 in 3 chance your real level is within this margin">
          ± {card.margin}
        </span>
        <span className={`badge level-${card.level.toLowerCase()}`} title={`${card.level}: Elo ${levelRange(card.level)}`}>
          {card.level}
        </span>
      </div>
      {last ? (
        <p className="small muted">
          Last quiz {last.elo_before} → {last.elo_after} ({eloDelta(last.elo_before, last.elo_after)})
        </p>
      ) : null}
      {!measured(card.margin) ? (
        <p className="small muted">
          A rough estimate until it's measured.{" "}
          {b.available >= 20 ? (
            <button className="link" disabled={check.isPending} onClick={() => check.mutate(card.slug)}>
              Take a skill check
            </button>
          ) : null}
        </p>
      ) : null}
      {(card.spark ?? []).length >= 3 ? <Sparkline values={(card.spark ?? []).map((elo) => 1 + elo * 0.03)} /> : null}
      {card.mistakes_due > 0 ? <p className="small">{mistakesLine(card.mistakes_due)}</p> : null}
      <BankMeter bank={b} />
      {note.text ? <p className={`bank-note tone-${note.tone}`}>{note.text}</p> : null}
      {actions}
      {start.error ? <p className="error">{message(start.error)}</p> : null}
      {check.error ? <p className="error">{message(check.error)}</p> : null}
      {gen.error ? <p className="error">{message(gen.error)}</p> : null}
    </article>
  );
}

function nextUp(cards: S["Card"][], auth: string): { slug: string | null; line: string } {
  const open = cards.find((c) => c.open_quiz);
  if (open?.open_quiz) {
    return { slug: open.slug, line: `Next up: finish your ${open.name} quiz (${open.open_quiz.answered} of ${open.open_quiz.length} done).` };
  }
  const slug = nextTopic(cards) ?? cards[0]?.slug ?? null;
  const card = cards.find((c) => c.slug === slug);
  if (!card) return { slug: null, line: "" };
  const step = nextStep({ auth, bank: card.bank, mistakesDue: card.mistakes_due, weak: [], topic: card.name, margin: card.margin });
  return { slug, line: `Next up in ${card.name}: ${step.text}. ${step.why}` };
}

export function Home() {
  useSetChatPage(homePage);
  const topics = useQuery({ queryKey: ["topics"], queryFn: async () => unwrap(await api.GET("/api/topics")) });
  const status = useStatus();
  const settings = useSettings();
  const auth = status.data?.auth ?? "";
  const cards = topics.data ?? [];
  const next = nextUp(cards, auth);
  return (
    <main className="page">
      <header className="page-head">
        <h1>Your topics</h1>
        <p className="muted">
          Elo (0–300) is your level in each topic, with a ± margin for how sure Quizroom is. Quizzes aim for{" "}
          {aimText(settings.data?.target ?? 0.75)}. Your Elo is what shows progress.
        </p>
        {next.line ? <p className="next-up">{next.line}</p> : null}
      </header>
      {topics.isPending ? <p className="muted">Loading…</p> : null}
      {visibleError(topics.error, isDown(status.error)) ? <p className="error">{visibleError(topics.error, isDown(status.error))}</p> : null}
      <div className="cards">
        {cards.map((c) => (
          <TopicCard key={c.slug} card={c} recommended={c.slug === next.slug} signedOut={auth === "none"} />
        ))}
      </div>
    </main>
  );
}
