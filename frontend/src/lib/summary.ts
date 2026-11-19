/** How a quiz went against the prediction, and what that did to the Elo. */
export function scoreSentence(
  answered: number,
  right: number,
  expected: number,
  eloBefore: number,
  eloAfter: number,
  offLevel = 0,
): string {
  if (answered === 0) return "No answers yet.";
  const gap = right - expected;
  const delta = eloAfter - eloBefore;
  const still = Math.abs(delta) < 3;
  const moved = `your Elo went ${delta > 0 ? "up" : "down"} ${Math.abs(delta)}`;
  const Moved = `${moved[0].toUpperCase()}${moved.slice(1)}`;
  if (Math.abs(gap) <= 1) {
    if (offLevel * 2 > answered) {
      const after = still ? "Your Elo barely moved." : `${Moved}.`;
      return `Close to the prediction. Most questions were outside your usual range, and the prediction allowed for that. ${after}`;
    }
    return still
      ? "Right on the prediction. The questions fit you, so your Elo barely moved."
      : `Close to the prediction, so the questions fit you. ${Moved}.`;
  }
  const effect = still ? "your Elo barely moved" : moved;
  if (gap > 0) return `${gap.toFixed(1)} more right than predicted, so ${effect}.`;
  return `${(-gap).toFixed(1)} fewer right than predicted, so ${effect}. That's how it finds your level.`;
}

/** Text as a sentence: trimmed, ending in . ! or ? so the next one can follow. */
export function sentence(text: string): string {
  const t = text.trim();
  if (t === "" || /[.!?]$/.test(t)) return t;
  return `${t}.`;
}

/** One line on how recent answers compare with the prediction. */
export function recentReading(answered: number, right: number, expected: number): string {
  if (answered === 0) return "No answers yet.";
  const gap = right - expected;
  if (gap > 1) return "You're beating the prediction, so your Elo is still catching up.";
  if (gap < -1) return "You're below the prediction, so your Elo is coming down to meet your level.";
  return "You're on the prediction, so the questions fit you.";
}

/** Why a quiz or check ended before its length, or "" when it didn't. */
export function endedEarly(kind: string, answered: number, length: number, margin: number, topic: string): string {
  if (answered >= length) return "";
  if (kind === "check" && answered >= 10 && margin <= 30) {
    return `The check stopped after ${answered} questions: enough to measure you within ± ${margin}.`;
  }
  const what = kind === "check" ? "The check stopped" : "This quiz ended";
  return `${what} after ${answered} questions because no more ${topic} questions were available.`;
}
