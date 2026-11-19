import { plural } from "../hooks";
import type { Bank } from "./bank";

export type Weak = { name: string; answered: number; right: number; expected: number };
export type Step = {
  kind: "signin" | "wait" | "write" | "write-then-quiz" | "check" | "quiz-mistakes" | "weak" | "quiz";
  text: string;
  why: string;
};

/** The one thing to do next in a topic, and why. */
export function nextStep(i: { auth: string; bank: Bank; mistakesDue: number; weak: Weak[]; topic: string; margin: number }): Step {
  const { bank: b, topic } = i;
  const short = b.state === "blocked" || b.state === "offlevel";
  if (short && i.auth === "none") {
    const why =
      b.questions === 0
        ? "There are no questions yet, and Claude isn't signed in to write them."
        : "New questions at your level are needed, and Claude isn't signed in to write them.";
    return { kind: "signin", text: "Sign Claude in", why };
  }
  if (short && b.writing) {
    return { kind: "wait", text: "Wait for Claude", why: `Claude is writing new ${topic} questions. They usually arrive in 1–3 minutes.` };
  }
  if (b.state === "blocked") {
    if (b.questions === 0) {
      return { kind: "write", text: "Write my first 30 questions", why: `${topic} has no questions yet. Claude writes the first 30 in about 5 minutes.` };
    }
    return {
      kind: "write",
      text: "Write 15 questions at your level",
      why: `A quiz needs 5 questions you haven't seen in the last 7 days, and ${topic} has ${b.available}.`,
    };
  }
  if (b.state === "offlevel") {
    return {
      kind: "write-then-quiz",
      text: "Write 15 questions at your level, then take a quiz",
      why: `Only ${b.near_level} of your next ${b.quiz_length} questions would fit your level.`,
    };
  }
  if (i.margin >= 50 && b.available >= 20) {
    return {
      kind: "check",
      text: "Take a skill check",
      why: `Your Elo is a rough estimate (± ${i.margin}) until it's measured. A check takes 10–20 questions.`,
    };
  }
  if (i.mistakesDue > 0) {
    const why =
      i.mistakesDue <= 3
        ? `It retests ${plural(i.mistakesDue, "mistake that's", "mistakes that are")} due.`
        : `${i.mistakesDue} mistakes are due; each quiz retests up to 3.`;
    return { kind: "quiz-mistakes", text: `Take a ${topic} quiz`, why };
  }
  if (i.weak.length > 0) {
    const w = [...i.weak].sort((a, b) => a.right - a.expected - (b.right - b.expected))[0];
    return {
      kind: "weak",
      text: `Ask Claude for 10 ${w.name} questions`,
      why: `${w.name} is your weakest spot: ${w.right} of ${w.answered} right, ${w.expected.toFixed(1)} predicted.`,
    };
  }
  return { kind: "quiz", text: `Take a ${topic} quiz`, why: `${b.quiz_length} questions at your level are ready.` };
}

export type CardLike = { slug: string; mistakes_due: number; bank: Bank };

/** The topic to suggest first on Home: one that can run a quiz, mistakes due first. */
export function nextTopic(cards: CardLike[]): string | null {
  const ready = cards.filter((c) => c.bank.state !== "blocked");
  if (ready.length === 0) return null;
  const due = ready.filter((c) => c.mistakes_due > 0).sort((a, b) => b.mistakes_due - a.mistakes_due);
  if (due.length > 0) return due[0].slug;
  return [...ready].sort((a, b) => b.bank.near_level - a.bank.near_level)[0].slug;
}
