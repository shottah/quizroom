import type { S } from "../api/client";
import { plural } from "../hooks";
import { dueText } from "../lib/due";
import { checkDots, groupMistakes } from "../lib/mistakes";
import { sentence } from "../lib/summary";
import { InlineText } from "./RichText";

const SCHEDULE =
  "A mistake comes back as a new question in your next quiz, then 1, 3, 7 and 21 days after each right answer. Getting one wrong again starts it over. Five checks in a row and it's done.";

function Row({ m, subName, now }: { m: S["MistakeView"]; subName: (slug: string) => string; now: Date }) {
  return (
    <li className="mistake">
      <p className="correction">
        <span className="ok" aria-hidden="true">
          ✓
        </span>{" "}
        Remember: <InlineText text={sentence(m.correction)} />
      </p>
      <p className="belief">
        <span className="no" aria-hidden="true">
          ✗
        </span>{" "}
        You believed: <InlineText text={sentence(m.belief)} />
      </p>
      <p className="muted small meta">
        {subName(m.subtopic)} ·{" "}
        <span className="check-dots" title={`check ${Math.min(m.stage + 1, 5)} of 5 next`}>
          {checkDots(m.stage, m.status)}
        </span>
        {m.hits > 0 ? ` · you made it ${plural(m.hits, "time", "times")}` : ""}
        {m.source === "mock" ? " · from your mock interview" : ""}
        {m.status === "active" && m.due_at && Date.parse(m.due_at) > now.getTime() ? ` · back from ${dueText(m.due_at, now)}` : ""}
      </p>
      {m.status === "active" && m.needs_questions && !(m.due_at && Date.parse(m.due_at) <= now.getTime()) ? (
        <p className="small needs">No fitting question yet; your next Write 15 will include one.</p>
      ) : null}
    </li>
  );
}

/** A topic's mistakes, grouped by when they come back. */
export function MistakeList({ mistakes, subtopics }: { mistakes: S["MistakeView"][]; subtopics: S["SubtopicView"][] }) {
  const now = new Date();
  const g = groupMistakes(mistakes, now);
  const subName = (slug: string) => subtopics.find((s) => s.slug === slug)?.name ?? slug;
  const group = (title: string, items: S["MistakeView"][]) =>
    items.length > 0 ? (
      <div className="mistake-group">
        <h3>
          {title} <span className="muted">({items.length})</span>
        </h3>
        <ul className="mistakes">
          {items.map((m) => (
            <Row key={m.id} m={m} subName={subName} now={now} />
          ))}
        </ul>
      </div>
    ) : null;
  const none = g.next.length + g.waiting.length + g.later.length + g.done.length === 0;
  return (
    <>
      {none ? <p className="muted">No mistakes on your list yet. Wrong answers that match a known trap add one.</p> : null}
      {group(g.next.length > 3 ? "Due now · your next quiz retests up to 3" : "Comes back in your next quiz", g.next)}
      {g.waiting.length > 0 ? (
        <>
          {group("Due, waiting for a question", g.waiting)}
          <p className="small muted">These are due, but no unseen question fits them yet, so a quiz can't retest them. Your next Write 15 includes questions on them.</p>
        </>
      ) : null}
      {group("Comes back later", g.later)}
      {g.done.length > 0 ? (
        <details>
          <summary>Done ({g.done.length})</summary>
          <ul className="mistakes">
            {g.done.map((m) => (
              <Row key={m.id} m={m} subName={subName} now={now} />
            ))}
          </ul>
        </details>
      ) : null}
      {g.traps.length > 0 ? (
        <details>
          <summary>Traps to watch for ({g.traps.length})</summary>
          <p className="small muted">Wrong options Claude built from common mistakes. They join your list only if you pick one.</p>
          <ul className="traps">
            {g.traps.map((m) => (
              <li key={m.id}>
                <InlineText text={sentence(m.belief)} /> <span className="muted small">({subName(m.subtopic)})</span>
              </li>
            ))}
          </ul>
        </details>
      ) : null}
      <p className="small muted">{SCHEDULE}</p>
    </>
  );
}
