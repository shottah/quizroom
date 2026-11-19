import { describe, expect, it } from "vitest";
import { chartPath, quizSeries } from "./chart";

describe("chartPath", () => {
  it("draws nothing for fewer than two points", () => {
    expect(chartPath([], 100, 90)).toBe("");
    expect(chartPath([{ t: 0, skill: 3 }], 100, 90)).toBe("");
  });

  it("puts skill 1 at the bottom and 10 at the top", () => {
    expect(chartPath([{ t: 0, skill: 1 }, { t: 10, skill: 10 }], 100, 90)).toBe("M0,90 L100,0");
  });
});

describe("quizSeries", () => {
  it("is the start plus the skill at the end of each quiz, in order", () => {
    const h = [
      { skill: 1.9, quiz_id: 4 },
      { skill: 2.3, quiz_id: 4 },
      { skill: 2.1, quiz_id: 7 },
      { skill: 2.8, quiz_id: 7 },
    ];
    expect(quizSeries(h)).toEqual([
      { label: "start", skill: 17, quizId: 0 },
      { label: "1", skill: 2.3, quizId: 4 },
      { label: "2", skill: 2.8, quizId: 7 },
    ]);
  });

  it("is empty with no answers", () => {
    expect(quizSeries([])).toEqual([]);
  });
});
