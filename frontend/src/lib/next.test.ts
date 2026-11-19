import { describe, expect, it } from "vitest";
import type { Bank } from "./bank";
import type { CardLike } from "./next";
import { nextStep, nextTopic } from "./next";

const bank = (b: Partial<Bank>): Bank => ({
  questions: 60, available: 40, near_level: 25, harder: 10, easier: 5, resting: 20, back_at: null,
  state: "plenty", writing: false, quiz_length: 10, ...b,
});
const step = (i: Partial<Parameters<typeof nextStep>[0]>) =>
  nextStep({ auth: "subscription", bank: bank({}), mistakesDue: 0, weak: [], topic: "Docker", margin: 27, ...i });

describe("nextStep", () => {
  it("asks to sign Claude in when questions are needed and it can't write them", () => {
    expect(step({ auth: "none", bank: bank({ state: "blocked", available: 2 }) })).toEqual({
      kind: "signin",
      text: "Sign Claude in",
      why: "New questions at your level are needed, and Claude isn't signed in to write them.",
    });
    expect(step({ auth: "none", bank: bank({ state: "offlevel", near_level: 2 }) }).kind).toBe("signin");
    expect(step({ auth: "none" }).kind).toBe("quiz");
    expect(step({ auth: "none", bank: bank({ state: "blocked", questions: 0, available: 0 }) }).why).toBe(
      "There are no questions yet, and Claude isn't signed in to write them.",
    );
  });

  it("waits while Claude writes for a bank that needs it", () => {
    expect(step({ bank: bank({ state: "blocked", available: 2, writing: true }) }).kind).toBe("wait");
    expect(step({ bank: bank({ writing: true }) }).kind).toBe("quiz");
  });

  it("writes when blocked, and the first 30 for an empty topic", () => {
    const s = step({ bank: bank({ state: "blocked", available: 3, quiz_length: 0 }) });
    expect(s).toEqual({
      kind: "write",
      text: "Write 15 questions at your level",
      why: "A quiz needs 5 questions you haven't seen in the last 7 days, and Docker has 3.",
    });
    const empty = step({ bank: bank({ state: "blocked", questions: 0, available: 0, near_level: 0, resting: 0, quiz_length: 0 }) });
    expect(empty.kind).toBe("write");
    expect(empty.text).toBe("Write my first 30 questions");
  });

  it("writes then quizzes when off level", () => {
    expect(step({ bank: bank({ state: "offlevel", near_level: 2 }) })).toEqual({
      kind: "write-then-quiz",
      text: "Write 15 questions at your level, then take a quiz",
      why: "Only 2 of your next 10 questions would fit your level.",
    });
  });

  it("quizzes when mistakes are due", () => {
    expect(step({ mistakesDue: 1, weak: [{ name: "Networking", answered: 6, right: 2, expected: 4.5 }] })).toEqual({
      kind: "quiz-mistakes",
      text: "Take a Docker quiz",
      why: "It retests 1 mistake that's due.",
    });
    expect(step({ mistakesDue: 5 }).why).toBe("5 mistakes are due; each quiz retests up to 3.");
  });

  it("points at the weakest spot", () => {
    expect(
      step({
        weak: [
          { name: "Storage", answered: 5, right: 3, expected: 3.4 },
          { name: "Networking", answered: 6, right: 2, expected: 4.5 },
        ],
      }),
    ).toEqual({
      kind: "weak",
      text: "Ask Claude for 10 Networking questions",
      why: "Networking is your weakest spot: 2 of 6 right, 4.5 predicted.",
    });
  });

  it("otherwise quizzes", () => {
    expect(step({})).toEqual({ kind: "quiz", text: "Take a Docker quiz", why: "10 questions at your level are ready." });
  });
});

describe("nextStep, skill checks", () => {
  it("suggests a check while the Elo is unmeasured and a check can run", () => {
    expect(step({ margin: 100, bank: bank({ available: 30 }) })).toEqual({
      kind: "check",
      text: "Take a skill check",
      why: "Your Elo is a rough estimate (± 100) until it's measured. A check takes 10–20 questions.",
    });
    expect(step({ margin: 100, bank: bank({ available: 19 }) }).kind).toBe("quiz");
    expect(step({ margin: 49, bank: bank({ available: 30 }) }).kind).toBe("quiz");
  });

  it("comes after the write steps and before mistakes", () => {
    expect(step({ margin: 100, bank: bank({ state: "offlevel", near_level: 2, available: 30 }) }).kind).toBe("write-then-quiz");
    expect(step({ margin: 100, mistakesDue: 2, bank: bank({ available: 30 }) }).kind).toBe("check");
  });
});

describe("nextTopic", () => {
  const card = (slug: string, b: Partial<Bank>, mistakes_due = 0): CardLike => ({ slug, mistakes_due, bank: bank(b) });

  it("prefers a topic with mistakes due that can run a quiz", () => {
    expect(nextTopic([card("a", { near_level: 30 }), card("b", { near_level: 2 }, 2), card("c", { state: "blocked" }, 5)])).toBe("b");
  });

  it("otherwise the most questions at your level", () => {
    expect(nextTopic([card("a", { near_level: 12 }), card("b", { near_level: 30 })])).toBe("b");
  });

  it("is null when nothing can run", () => {
    expect(nextTopic([card("a", { state: "blocked" }), card("b", { state: "blocked" })])).toBeNull();
    expect(nextTopic([])).toBeNull();
  });
});
