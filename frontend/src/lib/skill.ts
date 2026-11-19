/** 1st, 2nd, 3rd, 4th, 11th, 21st… */
export function ordinal(n: number): string {
  const teen = n % 100 >= 11 && n % 100 <= 13;
  const suffix = teen ? "th" : (["th", "st", "nd", "rd"][n % 10] ?? "th");
  return `${n}${suffix}`;
}

export type SkillMove = { eloBefore: number; eloAfter: number; answersInTopic: number; p: number; correct: boolean };

/** Why one answer moved the skill as much as it did; "" when unremarkable. */
export function skillNote(r: SkillMove, topic: string): string {
  const notes: string[] = [];
  if (Math.abs(r.eloAfter - r.eloBefore) >= 10 && r.answersInTopic <= 30) {
    notes.push(
      `Big swings are normal early in a topic: this was your ${ordinal(r.answersInTopic)} ${topic} answer. Each answer counts for less as you go.`,
    );
  }
  const pct = Math.round(r.p * 100);
  if (!r.correct && r.p >= 0.7) notes.push(`You had a ${pct}% predicted chance on this one, so missing it moved you more.`);
  if (r.correct && r.p <= 0.6) notes.push(`Only a ${pct}% predicted chance, so getting it right moved you more.`);
  return notes.join(" ");
}

const LEVELS = ["Beginner", "Intermediate", "Advanced"];

/** "You're now Intermediate in Docker." when the level changed, otherwise "". */
export function levelChange(before: string, after: string, topic: string): string {
  const a = LEVELS.indexOf(before);
  const b = LEVELS.indexOf(after);
  if (a < 0 || b < 0 || a === b) return "";
  return b > a ? `You're now ${after} in ${topic}.` : `You're back to ${after} in ${topic}.`;
}


const RANGES: Record<string, string> = { Beginner: "0–99", Intermediate: "100–199", Advanced: "200–300" };

/** The skill range a level covers. */
export function levelRange(level: string): string {
  return RANGES[level] ?? "";
}

const AIMS: Record<string, string> = { "0.65": "about 2 right in 3", "0.75": "about 3 right in 4", "0.85": "about 6 right in 7" };

/** What the target success rate means for a 10-question quiz. */
export function aimText(target: number): string {
  const tenths = target * 10;
  const lo = Math.floor(tenths + 1e-9);
  const hi = Math.ceil(tenths - 1e-9);
  const typical = lo === hi ? `${lo}` : `${lo} or ${hi}`;
  const aim = AIMS[target.toFixed(2)] ?? `about ${Math.round(target * 100)}% right`;
  return `${aim}, so expect ${typical} of 10 every time`;
}

/** "+1.2", "−0.8" or "0.0" */
export function signed(n: number): string {
  const r = Math.round(n * 10) / 10;
  if (r === 0) return "0.0";
  return r > 0 ? `+${r.toFixed(1)}` : `−${Math.abs(r).toFixed(1)}`;
}

/** "160 ± 27": an Elo with its margin. */
export function eloText(elo: number, margin: number): string {
  return `${elo} ± ${margin}`;
}

/** "+12", "−8" or "no change", for an Elo moving from before to after. */
export function eloDelta(before: number, after: number): string {
  const d = after - before;
  if (d === 0) return "no change";
  return d > 0 ? `+${d}` : `−${Math.abs(d)}`;
}

/** An Elo counts as measured once its margin is under ±50. */
export function measured(margin: number): boolean {
  return margin < 50;
}
