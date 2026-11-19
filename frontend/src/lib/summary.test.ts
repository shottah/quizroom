import { describe, expect, it } from "vitest";
import { endedEarly, recentReading, scoreSentence, sentence } from "./summary";

describe("scoreSentence", () => {
  it("has nothing to say with no answers", () => {
    expect(scoreSentence(0, 0, 0, 17, 17)).toBe("No answers yet.");
  });

  it("within 1 of the prediction and the skill barely moved", () => {
    expect(scoreSentence(10, 7, 7.6, 70, 72)).toBe(
      "Right on the prediction. The questions fit you, so your Elo barely moved.",
    );
    expect(scoreSentence(10, 8, 7.0, 70, 71)).toBe(
      "Right on the prediction. The questions fit you, so your Elo barely moved.",
    );
  });

  it("within 1 of the prediction but the skill moved", () => {
    expect(scoreSentence(10, 7, 7.5, 53, 37)).toBe(
      "Close to the prediction, so the questions fit you. Your Elo went down 16.",
    );
  });

  it("above the prediction", () => {
    expect(scoreSentence(10, 9, 6.7, 33, 60)).toBe("2.3 more right than predicted, so your Elo went up 27.");
  });

  it("below the prediction", () => {
    expect(scoreSentence(10, 4, 7.2, 80, 53)).toBe(
      "3.2 fewer right than predicted, so your Elo went down 27. That's how it finds your level.",
    );
  });

  it("never prints NaN for one answer", () => {
    expect(scoreSentence(1, 0, 0.8, 17, 7)).not.toMatch(/NaN|Infinity/);
  });
});

describe("sentence", () => {
  it("ends text with a full stop unless it already has one", () => {
    expect(sentence("The kubelet schedules DaemonSet pods")).toBe("The kubelet schedules DaemonSet pods.");
    expect(sentence("Look in etcd to debug.")).toBe("Look in etcd to debug.");
    expect(sentence("Is it `ports:`?")).toBe("Is it `ports:`?");
    expect(sentence("  spaced  ")).toBe("spaced.");
    expect(sentence("")).toBe("");
  });
});

describe("recentReading", () => {
  it("reads recent answers against the prediction", () => {
    expect(recentReading(0, 0, 0)).toBe("No answers yet.");
    expect(recentReading(40, 29, 24.8)).toBe("You're beating the prediction, so your Elo is still catching up.");
    expect(recentReading(40, 20, 24.8)).toBe("You're below the prediction, so your Elo is coming down to meet your level.");
    expect(recentReading(40, 25, 24.8)).toBe("You're on the prediction, so the questions fit you.");
  });
});

describe("scoreSentence with off-level questions", () => {
  it("doesn't claim the questions fit when most were off level", () => {
    expect(scoreSentence(10, 5, 5.1, 70, 63, 8)).toBe(
      "Close to the prediction. Most questions were outside your usual range, and the prediction allowed for that. Your Elo went down 7.",
    );
    expect(scoreSentence(10, 5, 5.1, 70, 63, 2)).toBe("Close to the prediction, so the questions fit you. Your Elo went down 7.");
  });
});

describe("endedEarly", () => {
  it("says why a quiz or check ended before its length", () => {
    expect(endedEarly("practice", 7, 10, 40, "Docker")).toBe("This quiz ended after 7 questions because no more Docker questions were available.");
    expect(endedEarly("check", 10, 20, 30, "Docker")).toBe("The check stopped after 10 questions: enough to measure you within ± 30.");
    expect(endedEarly("check", 6, 20, 60, "Docker")).toBe("The check stopped after 6 questions because no more Docker questions were available.");
    expect(endedEarly("check", 20, 20, 30, "Docker")).toBe("");
    expect(endedEarly("practice", 10, 10, 30, "Docker")).toBe("");
  });
});
