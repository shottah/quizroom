import { quizSeries } from "../lib/chart";

const W = 640;
const H = 230;
const PAD = { left: 34, right: 22, top: 10, bottom: 30 };
const PLOT_W = W - PAD.left - PAD.right;
const PLOT_H = H - PAD.top - PAD.bottom;

const BANDS = [
  { name: "Advanced", range: "200–300", from: 200, to: 300 },
  { name: "Intermediate", range: "100–199", from: 100, to: 200 },
  { name: "Beginner", range: "0–99", from: 0, to: 100 },
];

const y = (elo: number) => PAD.top + (PLOT_H * (300 - Math.min(300, Math.max(0, elo)))) / 300;

type Check = { quiz_id: number; elo: number; margin: number };

/** Elo after each quiz on the 0–300 scale, with the level bands; skill checks carry their ± margin. */
export function SkillChart({ history, checks }: { history: { elo: number; quiz_id: number }[]; checks: Check[] }) {
  const pts = quizSeries(history.map((p) => ({ skill: p.elo, quiz_id: p.quiz_id })));
  if (pts.length < 2) {
    return <p className="muted">Answer a few questions to see your progress here.</p>;
  }
  const byQuiz = new Map(checks.map((c) => [c.quiz_id, c]));
  const x = (i: number) => PAD.left + (PLOT_W * i) / (pts.length - 1);
  const every = Math.ceil(pts.length / 12);
  const path = pts.map((p, i) => `${i === 0 ? "M" : "L"}${x(i).toFixed(1)},${y(p.skill).toFixed(1)}`).join(" ");
  const last = pts[pts.length - 1];
  return (
    <figure className="chart">
      <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-label={`Elo after each quiz, from ${pts[0].skill} to ${last.skill}`}>
        {BANDS.map((b) => (
          <g key={b.name}>
            <rect x={PAD.left} y={y(b.to)} width={PLOT_W} height={y(b.from) - y(b.to)} className={`band band-${b.name.toLowerCase()}`} />
            <text x={PAD.left + 8} y={y(b.to) + 15} className="band-label">
              {b.name} {b.range}
            </text>
          </g>
        ))}
        {[0, 100, 200, 300].map((t) => (
          <text key={t} x={PAD.left - 8} y={y(t) + 4} className="axis-label" textAnchor="end">
            {t}
          </text>
        ))}
        {pts.map((p, i) =>
          i % every === 0 || i === pts.length - 1 ? (
            <text key={p.label + i} x={x(i)} y={H - 10} className="axis-label" textAnchor="middle">
              {p.label}
            </text>
          ) : null,
        )}
        <path d={path} className="line" />
        {pts.map((p, i) => {
          const c = byQuiz.get(p.quizId);
          if (c) {
            const cx = x(i);
            const cy = y(p.skill);
            return (
              <g key={i} className="point check-point">
                <title>{`Skill check ${p.label}: Elo ${c.elo} ± ${c.margin}`}</title>
                <line x1={cx} x2={cx} y1={y(c.elo + c.margin)} y2={y(c.elo - c.margin)} className="err-bar" />
                <line x1={cx - 5} x2={cx + 5} y1={y(c.elo + c.margin)} y2={y(c.elo + c.margin)} className="err-bar" />
                <line x1={cx - 5} x2={cx + 5} y1={y(c.elo - c.margin)} y2={y(c.elo - c.margin)} className="err-bar" />
                <circle cx={cx} cy={cy} r={11} className="hit" />
                <path d={`M${cx},${cy - 6} L${cx + 6},${cy} L${cx},${cy + 6} L${cx - 6},${cy} Z`} className="diamond" />
              </g>
            );
          }
          return (
            <g key={i} className="point">
              <title>{i === 0 ? `Start: Elo ${p.skill}` : `After quiz ${p.label}: Elo ${p.skill}`}</title>
              <circle cx={x(i)} cy={y(p.skill)} r={11} className="hit" />
              <circle cx={x(i)} cy={y(p.skill)} r={4} className="dot" />
            </g>
          );
        })}
        <text x={x(pts.length - 1) - 8} y={y(last.skill) - 10} className="value-label" textAnchor="end">
          {last.skill}
        </text>
      </svg>
      <figcaption className="small muted">
        Elo after each quiz. ◆ marks a skill check, with its ± margin. Hover a point for its value.
      </figcaption>
    </figure>
  );
}
