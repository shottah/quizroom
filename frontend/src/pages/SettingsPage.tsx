import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, message, unwrap, type S } from "../api/client";
import { useSetChatPage } from "../chatContext";
import { InlineText } from "../components/RichText";
import { useSettings, useStatus } from "../hooks";

const TARGETS: { value: S["Settings"]["target"]; label: string }[] = [
  { value: 0.65, label: "Harder: about 2 in 3 right (65%)" },
  { value: 0.75, label: "Standard: about 3 in 4 right (75%) · recommended" },
  { value: 0.85, label: "Easier: about 6 in 7 right (85%)" },
];

const MODELS: { value: S["Settings"]["model"]; label: string }[] = [
  { value: "sonnet", label: "Sonnet: faster, lighter on your plan" },
  { value: "opus", label: "Opus: strongest questions, uses more of your plan (15 questions took about 2 min)" },
];

function StatusBanner({ status }: { status: S["Status"] }) {
  const last = status.last_error ? (
    <p className="small">
      Last Claude error: <InlineText text={status.last_error} />
    </p>
  ) : null;
  switch (status.auth) {
    case "subscription":
      return (
        <div className="status-banner ok">
          <p>
            <strong>Claude: ready</strong> (your subscription).
          </p>
          {last}
        </div>
      );
    case "api_key":
      return (
        <div className="status-banner ok">
          <p>
            <strong>Claude: ready</strong> (API key; calls bill your API account).
          </p>
          {last}
        </div>
      );
    case "none":
      return (
        <div className="status-banner warn">
          <p>
            <strong>Claude: not signed in.</strong> Chat and question writing won't work until you sign in.
          </p>
          <ol>
            <li>
              In a terminal, open the folder you cloned Quizroom into.
            </li>
            <li>
              Run <code>make token</code> and paste the long token it prints (it starts with <code>sk-ant-</code>), or run <code>make api-key</code> to use an API key instead.
            </li>
            <li>It restarts the runner for you. Then reload this page.</li>
          </ol>
          {last}
        </div>
      );
    default:
      return (
        <div className="status-banner error">
          <p>
            <strong>Claude: can't reach the Claude runner.</strong> Run <code>make doctor</code> in your Quizroom folder to
            see what's down.
          </p>
          {last}
        </div>
      );
  }
}

// The AGPL asks anyone who runs a changed copy for others to offer them its
// source; keep this link pointing at yours.
const SOURCE_URL = "https://github.com/shottah/quizroom";

function HowItWorks({ target }: { target: number }) {
  const pct = Math.round(target * 100);
  return (
    <details className="card how">
      <summary>How Quizroom works</summary>
      <ol>
        <li>
          <strong>Elo.</strong> Each topic has an Elo from 0 to 300, with a ± margin for how sure Quizroom is (about a 2 in 3
          chance your real level is inside it). It moves after every answer, and a skill check measures it fastest.
        </li>
        <li>
          <strong>At your level.</strong> Quizzes pick questions you have about a {pct}% chance of getting right, so your score stays
          steady while the questions get harder as your skill grows.
        </li>
        <li>
          <strong>Predicted.</strong> Every answer has a predicted chance. Beating the prediction raises your skill; missing one you
          were expected to get lowers it. Levels are names for ranges: Beginner 0–99, Intermediate 100–199, Advanced 200–300.
        </li>
        <li>
          <strong>Resting.</strong> A question rests for 7 days after you see it, so a quiz tests the idea, not your memory of the
          question. When a topic runs short, press Write 15 at my level and Claude writes 15 more at your level.
        </li>
        <li>
          <strong>Mistakes.</strong> A wrong answer that matches a known trap goes on your mistake list. It comes back as a new
          question in your next quiz, then 1, 3, 7 and 21 days after each right answer. Five checks in a row and it's done.
        </li>
      </ol>
    </details>
  );
}

export function SettingsPage() {
  useSetChatPage({ page: "settings", topic: "", question_id: 0, chosen: [] });
  const qc = useQueryClient();
  const status = useStatus();
  const settings = useSettings();
  const save = useMutation({
    mutationFn: async (s: S["Settings"]) => unwrap(await api.PUT("/api/settings", { body: s })),
    onSuccess: (s) => {
      qc.setQueryData(["settings"], s);
      void qc.invalidateQueries({ queryKey: ["topics"] });
    },
  });
  const s = settings.data;
  return (
    <main className="page narrow">
      <header className="page-head">
        <h1>Settings</h1>
      </header>
      <section className="card">
        <h2>Claude</h2>
        {status.isPending ? <p className="muted">Checking…</p> : null}
        {status.data ? (
          <>
            <StatusBanner status={status.data} />
            <details>
              <summary>Details</summary>
              <dl className="facts">
                <dt>Claude Code CLI</dt>
                <dd>{status.data.claude || "unknown"}</dd>
                <dt>Questions</dt>
                <dd>
                  {status.data.questions} in {status.data.topics} topics
                </dd>
              </dl>
            </details>
          </>
        ) : null}
        {status.error ? <p className="error">{message(status.error)}</p> : null}
      </section>
      {s ? (
        <section className="card">
          <h2>Quizzes</h2>
          <fieldset>
            <legend>How hard should quizzes be?</legend>
            {TARGETS.map((t) => (
              <label key={t.value} className="radio">
                <input type="radio" name="target" checked={s.target === t.value} onChange={() => save.mutate({ ...s, target: t.value })} />
                {t.label}
              </label>
            ))}
            <p className="small muted">This changes which questions you get, not how skill is measured.</p>
          </fieldset>
          <fieldset>
            <legend>Claude model for writing questions</legend>
            {MODELS.map((m) => (
              <label key={m.value} className="radio">
                <input type="radio" name="model" checked={s.model === m.value} onChange={() => save.mutate({ ...s, model: m.value })} />
                {m.label}
              </label>
            ))}
          </fieldset>
          {save.error ? <p className="error">{message(save.error)}</p> : null}
          {save.isSuccess ? <p className="muted small">Saved. Difficulty applies from your next quiz; the model from Claude's next request.</p> : null}
        </section>
      ) : null}
      <HowItWorks target={s?.target ?? 0.75} />
      <p className="small muted">
        Quizroom is free software under the <a href="https://www.gnu.org/licenses/agpl-3.0.html">GNU AGPL v3 or later</a> ·{" "}
        <a href={SOURCE_URL}>Source code</a>
      </p>
    </main>
  );
}
