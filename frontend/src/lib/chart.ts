// SVG path for skill over time: x spans the first to the last point, and y
// puts skill 1 at the bottom (h) and skill 10 at the top (0).

export type ChartPoint = { t: number; skill: number };

const fmt = (n: number) => String(Number(n.toFixed(1)));

export function skillY(skill: number, h: number): number {
  return (h * (10 - skill)) / 9;
}

export function chartPath(points: ChartPoint[], w: number, h: number): string {
  if (points.length < 2) return "";
  const t0 = points[0].t;
  const span = points[points.length - 1].t - t0 || 1;
  return points
    .map((p, i) => `${i === 0 ? "M" : "L"}${fmt(((p.t - t0) / span) * w)},${fmt(skillY(p.skill, h))}`)
    .join(" ");
}

export type QuizPoint = { label: string; skill: number; quizId: number };

/** Skill quiz by quiz: the starting 1.5, then the skill after each quiz's last answer. */
export function quizSeries(history: { skill: number; quiz_id: number }[], start = 17): QuizPoint[] {
  if (history.length === 0) return [];
  const out: QuizPoint[] = [{ label: "start", skill: start, quizId: 0 }];
  for (const p of history) {
    const last = out[out.length - 1];
    if (last.quizId === p.quiz_id) last.skill = p.skill;
    else out.push({ label: String(out.length), skill: p.skill, quizId: p.quiz_id });
  }
  return out;
}
