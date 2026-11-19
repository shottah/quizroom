import { describe, expect, it } from "vitest";
import { askWhy, cleanStem, optionTag, verdict } from "./verdict";

const o = (correct: boolean, chosen: boolean) => ({ correct, chosen });

describe("verdict", () => {
  it("single, right", () => {
    expect(verdict([o(false, false), o(true, true), o(false, false), o(false, false)], "single")).toBe("Right.");
  });

  it("single, wrong: names the right letter as shown", () => {
    expect(verdict([o(false, true), o(false, false), o(true, false), o(false, false)], "single")).toBe(
      "Not quite. The answer is C.",
    );
  });

  it("pick-all, every correct option and nothing else", () => {
    expect(verdict([o(true, true), o(false, false), o(true, true), o(true, true), o(false, false)], "multi")).toBe(
      "Right: you found all 3 correct options.",
    );
  });

  it("pick-all, some missed", () => {
    expect(verdict([o(true, true), o(false, false), o(true, true), o(true, false), o(false, false)], "multi")).toBe(
      "Not quite. You found 2 of 3 correct options.",
    );
  });

  it("pick-all, all found plus a wrong pick", () => {
    expect(verdict([o(true, true), o(false, true), o(true, true), o(true, true), o(false, false)], "multi")).toBe(
      "Not quite. You picked 1 wrong option.",
    );
  });

  it("pick-all, some missed and wrong picks", () => {
    expect(verdict([o(true, true), o(false, true), o(true, true), o(true, false), o(false, false)], "multi")).toBe(
      "Not quite. You found 2 of 3 correct options and picked 1 wrong.",
    );
  });

  it("pick-all, nothing correct", () => {
    expect(verdict([o(true, false), o(false, true), o(true, false), o(false, true)], "multi")).toBe(
      "Not quite. You found 0 of 2 correct options and picked 2 wrong.",
    );
  });
});

describe("optionTag", () => {
  it("single: only the chosen option is tagged", () => {
    expect(optionTag(o(false, true), "single")).toBe("your answer");
    expect(optionTag(o(true, true), "single")).toBe("your answer");
    expect(optionTag(o(true, false), "single")).toBe("");
  });

  it("pick-all: picks and missed correct options", () => {
    expect(optionTag(o(true, true), "multi")).toBe("you picked");
    expect(optionTag(o(false, true), "multi")).toBe("you picked");
    expect(optionTag(o(true, false), "multi")).toBe("you missed this");
    expect(optionTag(o(false, false), "multi")).toBe("");
  });
});

describe("askWhy", () => {
  it("single: asks about the pick and the answer", () => {
    expect(askWhy([o(false, true), o(false, false), o(true, false), o(false, false)], "single")).toBe(
      "Why is A wrong, and why is C right?",
    );
    expect(askWhy([o(false, false), o(true, true), o(false, false), o(false, false)], "single")).toBe("Why is B right?");
  });

  it("pick-all: lists the letters", () => {
    expect(askWhy([o(true, true), o(false, true), o(true, false), o(true, false), o(false, false)], "multi")).toBe(
      "Why is my answer (A and B) wrong, and why are A, C and D right?",
    );
    expect(askWhy([o(true, true), o(false, false), o(true, true), o(false, false)], "multi")).toBe("Why are A and C right?");
  });
});

describe("cleanStem", () => {
  it("drops a trailing pick-all instruction from pick-all stems", () => {
    expect(cleanStem("Which are true? (Pick all that apply.)", "multi")).toBe("Which are true?");
    expect(cleanStem("Which are true? (select all that apply)", "multi")).toBe("Which are true?");
  });

  it("leaves everything else alone", () => {
    expect(cleanStem("Which is true? (Pick all that apply.)", "single")).toBe("Which is true? (Pick all that apply.)");
    expect(cleanStem("Which are true?", "multi")).toBe("Which are true?");
    expect(cleanStem("Does (pick all that apply) mean anything here?", "multi")).toBe("Does (pick all that apply) mean anything here?");
  });
});
