import { bankLegend, bankParts, type Bank } from "../lib/bank";

const RESTING =
  "You've seen these in the last 7 days. Quizroom doesn't repeat a question within 7 days, so a quiz tests the idea, not your memory of the question.";

const NAMES = { near: "At your level", off: "Off your level", rest: "Resting" };

/** The question bank as one thin bar: at your level, off your level, resting. */
export function BankMeter({ bank, label = true }: { bank: Bank; label?: boolean }) {
  const parts = bankParts(bank);
  if (parts.length === 0) return null;
  const total = parts.reduce((n, p) => n + p.count, 0);
  return (
    <div className="bank">
      {label ? <div className="bank-label small muted">Question bank</div> : null}
      <div className="bank-bar" role="img" aria-label={`Question bank: ${bankLegend(bank)}`}>
        {parts.map((p) => (
          <span
            key={p.key}
            className={`seg seg-${p.key}`}
            style={{ flexGrow: p.count }}
            title={`${NAMES[p.key]}: ${p.text} (${Math.round((p.count / total) * 100)}%)`}
          />
        ))}
      </div>
      <ul className="bank-legend small">
        {parts.map((p) => (
          <li key={p.key}>
            <span className={`swatch seg-${p.key}`} aria-hidden="true" />
            {p.text}
            {p.key === "rest" ? (
              <span className="tip" title={RESTING} aria-label={RESTING} tabIndex={0}>
                ?
              </span>
            ) : null}
          </li>
        ))}
      </ul>
    </div>
  );
}
