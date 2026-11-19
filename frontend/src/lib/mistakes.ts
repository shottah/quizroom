// Groups a topic's mistakes by when they come back.

import { dueText } from "./due";

export type MistakeLike = { id: number; status: string; stage: number; due_at: string | null; needs_questions?: boolean };

export type MistakeGroups<T> = { next: T[]; waiting: T[]; later: T[]; done: T[]; traps: T[] };

/** next: due, with a question to retest it; waiting: due, but no fitting question yet; later: due after now,
 * soonest first; done; traps: Claude's, never made. */
export function groupMistakes<T extends MistakeLike>(ms: T[], now: Date): MistakeGroups<T> {
  const g: MistakeGroups<T> = { next: [], waiting: [], later: [], done: [], traps: [] };
  const due = (x: T) => (x.due_at ? Date.parse(x.due_at) : 0);
  for (const x of ms) {
    if (x.status === "resolved") g.done.push(x);
    else if (x.status === "watching") g.traps.push(x);
    else if (due(x) <= now.getTime()) (x.needs_questions ? g.waiting : g.next).push(x);
    else g.later.push(x);
  }
  g.later.sort((a, b) => due(a) - due(b));
  return g;
}

/** ●●○○○: one filled dot per check passed, of five. */
export function checkDots(stage: number, status: string): string {
  const passed = status === "resolved" ? 5 : Math.max(0, Math.min(5, stage));
  return "●".repeat(passed) + "○".repeat(5 - passed);
}

/** Where a mistake stands now: done, due now, or its next check. */
export function returnLine(n: { done: boolean; next_due: string | null; next_check: number }, now: Date): string {
  if (n.done) return "Done: you've got this one.";
  if (!n.next_due) return "";
  if (Date.parse(n.next_due) <= now.getTime()) return "Due now: a coming quiz retests it with a new question.";
  return `Next check from ${dueText(n.next_due, now)} (check ${n.next_check} of 5).`;
}
