import { letter, plural } from "../hooks";

export type Kind = "single" | "multi";
type Opt = { correct: boolean; chosen: boolean };

/** The one-line verdict on an answer. Options are in the order shown. */
export function verdict(options: Opt[], kind: Kind): string {
  if (kind === "single") {
    if (options.some((o) => o.chosen && o.correct)) return "Right.";
    const answer = options.findIndex((o) => o.correct);
    return `Not quite. The answer is ${letter(answer)}.`;
  }
  const correct = options.filter((o) => o.correct).length;
  const found = options.filter((o) => o.correct && o.chosen).length;
  const wrong = options.filter((o) => !o.correct && o.chosen).length;
  if (found === correct && wrong === 0) return `Right: you found all ${correct} correct options.`;
  if (wrong === 0) return `Not quite. You found ${found} of ${correct} correct options.`;
  if (found === correct) return `Not quite. You picked ${plural(wrong, "wrong option", "wrong options")}.`;
  return `Not quite. You found ${found} of ${correct} correct options and picked ${wrong} wrong.`;
}

/** The small label beside an option after answering. */
export function optionTag(o: Opt, kind: Kind): "your answer" | "you picked" | "you missed this" | "" {
  if (kind === "single") return o.chosen ? "your answer" : "";
  if (o.chosen) return "you picked";
  return o.correct ? "you missed this" : "";
}

/** "A", "A and C", "A, C and D" */
export function letterList(letters: string[]): string {
  if (letters.length <= 1) return letters.join("");
  return `${letters.slice(0, -1).join(", ")} and ${letters[letters.length - 1]}`;
}

/** The question the "Ask Claude why" chip puts in the chat box. */
export function askWhy(options: Opt[], kind: Kind): string {
  const chosen = options.flatMap((o, i) => (o.chosen ? [letter(i)] : []));
  const correct = options.flatMap((o, i) => (o.correct ? [letter(i)] : []));
  const right = options.every((o) => o.chosen === o.correct);
  const are = correct.length === 1 ? "is" : "are";
  if (kind === "single") {
    return right ? `Why is ${correct[0]} right?` : `Why is ${chosen[0]} wrong, and why is ${correct[0]} right?`;
  }
  if (right) return `Why ${are} ${letterList(correct)} right?`;
  return `Why is my answer (${letterList(chosen)}) wrong, and why ${are} ${letterList(correct)} right?`;
}

/** Drops the "(Pick all that apply.)" older pick-all stems end with: the app shows that instruction itself. */
export function cleanStem(stem: string, kind: Kind): string {
  if (kind !== "multi") return stem;
  return stem.replace(/\s*\((?:pick|select|choose) all that apply\.?\)\s*$/i, "");
}
