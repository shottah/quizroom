import { describe, expect, it } from "vitest";
import type { Bank } from "./bank";
import { bankLegend, bankNote, bankParts } from "./bank";

const now = new Date(2026, 10, 18, 21, 0);
const bank = (b: Partial<Bank>): Bank => ({
  questions: 45, available: 15, near_level: 1, harder: 14, easier: 0, resting: 30,
  back_at: new Date(2026, 10, 25, 9, 26).toISOString(), state: "offlevel", writing: false, quiz_length: 10, ...b,
});

describe("bankLegend", () => {
  it("lists the non-zero parts", () => {
    expect(bankLegend(bank({}))).toBe("1 at your level · 14 harder · 30 resting, first back Wed 25 Nov");
    expect(bankLegend(bank({ near_level: 25, harder: 0, easier: 3, resting: 0, back_at: null }))).toBe(
      "25 at your level · 3 easier",
    );
  });

  it("is empty for an empty bank", () => {
    expect(bankLegend(bank({ questions: 0, available: 0, near_level: 0, harder: 0, resting: 0, back_at: null }))).toBe("");
  });
});

describe("bankNote", () => {
  it("is silent when there's plenty", () => {
    expect(bankNote(bank({ state: "plenty", near_level: 25 }), "Docker", now)).toEqual({ tone: "none", text: "" });
  });

  it("is a grey reassurance when low", () => {
    expect(bankNote(bank({ state: "low", near_level: 12 }), "Docker", now)).toEqual({ tone: "grey", text: "Enough for your next quiz." });
  });

  it("explains an off-level quiz with numbers and its effect", () => {
    expect(bankNote(bank({}), "Docker", now)).toEqual({
      tone: "amber",
      text: "Only 1 of your next 10 questions would fit your level. The other 9 would be harder than usual, so expect a lower score.",
    });
    expect(bankNote(bank({ near_level: 3, harder: 1, easier: 6, quiz_length: 7, available: 10 }), "Docker", now).text).toBe(
      "Only 3 of your next 7 questions would fit your level. The other 4 would be easier than usual, so expect a higher score.",
    );
    expect(bankNote(bank({ near_level: 0, harder: 15 }), "Docker", now).text).toBe(
      "None of your next 10 questions would fit your level. All 10 would be harder than usual, so expect a lower score.",
    );
  });

  it("says why a quiz can't start and when that changes", () => {
    expect(bankNote(bank({ state: "blocked", available: 4, quiz_length: 0 }), "Docker", now)).toEqual({
      tone: "amber",
      text: "No quiz right now: a quiz needs 5 questions you haven't seen in the last 7 days, and Docker has 4. More come back Wed 25 Nov, 9:26 am.",
    });
  });

  it("is grey while Claude writes, whatever the state", () => {
    expect(bankNote(bank({ writing: true }), "Docker", now)).toEqual({ tone: "grey", text: "Claude is writing new Docker questions…" });
  });

  it("leaves an empty topic to the empty-card copy", () => {
    expect(bankNote(bank({ state: "blocked", questions: 0, available: 0, resting: 0 }), "Docker", now)).toEqual({ tone: "none", text: "" });
  });
});

describe("bankParts", () => {
  it("splits the bank into the meter's parts in order, skipping empty ones", () => {
    const parts = bankParts(bank({ easier: 2 }));
    expect(parts.map((p) => [p.key, p.count])).toEqual([["near", 1], ["off", 16], ["rest", 30]]);
    expect(parts[1].text).toBe("14 harder · 2 easier");
    expect(bankParts(bank({ near_level: 0, harder: 0, resting: 0 }))).toEqual([]);
  });
});
