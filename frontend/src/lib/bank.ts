import type { components } from "../api/schema";
import { dateText, dueText } from "./due";

export type Bank = components["schemas"]["Bank"];

export type BankPart = { key: "near" | "off" | "rest"; count: number; text: string };

/** The meter's parts in order: at your level, off your level, resting. */
export function bankParts(b: Bank): BankPart[] {
  const parts: BankPart[] = [];
  if (b.near_level > 0) parts.push({ key: "near", count: b.near_level, text: `${b.near_level} at your level` });
  const off = [b.harder > 0 ? `${b.harder} harder` : "", b.easier > 0 ? `${b.easier} easier` : ""].filter(Boolean);
  if (off.length > 0) parts.push({ key: "off", count: b.harder + b.easier, text: off.join(" · ") });
  if (b.resting > 0) {
    const text = b.back_at ? `${b.resting} resting, first back ${dateText(b.back_at)}` : `${b.resting} resting`;
    parts.push({ key: "rest", count: b.resting, text });
  }
  return parts;
}

/** "1 at your level · 14 harder · 30 resting, first back Wed 25 Nov" */
export function bankLegend(b: Bank): string {
  return bankParts(b)
    .map((p) => p.text)
    .join(" · ");
}

export type Note = { tone: "none" | "grey" | "amber"; text: string };

/** What the bank means for the next quiz, if anything needs saying. */
export function bankNote(b: Bank, topic: string, now: Date = new Date()): Note {
  if (b.questions === 0) return { tone: "none", text: "" };
  if (b.writing) return { tone: "grey", text: `Claude is writing new ${topic} questions…` };
  switch (b.state) {
    case "plenty":
      return { tone: "none", text: "" };
    case "low":
      return { tone: "grey", text: "Enough for your next quiz." };
    case "offlevel": {
      const len = b.quiz_length;
      const harder = b.harder >= b.easier;
      const effect = `${harder ? "harder" : "easier"} than usual, so expect a ${harder ? "lower" : "higher"} score.`;
      const text =
        b.near_level === 0
          ? `None of your next ${len} questions would fit your level. All ${len} would be ${effect}`
          : `Only ${b.near_level} of your next ${len} questions would fit your level. The other ${len - b.near_level} would be ${effect}`;
      return { tone: "amber", text };
    }
    case "blocked": {
      const back = b.back_at ? ` More come back ${dueText(b.back_at, now)}.` : "";
      return {
        tone: "amber",
        text: `No quiz right now: a quiz needs 5 questions you haven't seen in the last 7 days, and ${topic} has ${b.available}.${back}`,
      };
    }
  }
}
