import { describe, expect, it } from "vitest";
import { aimText, eloDelta, eloText, levelChange, levelRange, measured, ordinal, signed, skillNote } from "./skill";

describe("skillNote", () => {
  it("explains big early swings", () => {
    expect(skillNote({ eloBefore: 80, eloAfter: 53, answersInTopic: 8, p: 0.5, correct: false }, "Kubernetes")).toBe(
      "Big swings are normal early in a topic: this was your 8th Kubernetes answer. Each answer counts for less as you go.",
    );
  });

  it("explains a miss you were expected to get", () => {
    expect(skillNote({ eloBefore: 133, eloAfter: 128, answersInTopic: 120, p: 0.82, correct: false }, "Docker")).toBe(
      "You had a 82% predicted chance on this one, so missing it moved you more.",
    );
  });

  it("explains a hard one you got right", () => {
    expect(skillNote({ eloBefore: 133, eloAfter: 137, answersInTopic: 120, p: 0.55, correct: true }, "Docker")).toBe(
      "Only a 55% predicted chance, so getting it right moved you more.",
    );
  });

  it("joins both notes", () => {
    expect(skillNote({ eloBefore: 33, eloAfter: 17, answersInTopic: 3, p: 0.9, correct: false }, "Docker")).toBe(
      "Big swings are normal early in a topic: this was your 3rd Docker answer. Each answer counts for less as you go. You had a 90% predicted chance on this one, so missing it moved you more.",
    );
  });

  it("says nothing about an ordinary answer", () => {
    expect(skillNote({ eloBefore: 133, eloAfter: 135, answersInTopic: 200, p: 0.75, correct: true }, "Docker")).toBe("");
  });
});

describe("ordinal", () => {
  it("handles the teens", () => {
    expect([1, 2, 3, 4, 11, 12, 13, 21, 22, 23, 101, 111].map(ordinal)).toEqual([
      "1st", "2nd", "3rd", "4th", "11th", "12th", "13th", "21st", "22nd", "23rd", "101st", "111th",
    ]);
  });
});

describe("levelChange", () => {
  it("announces a level up and down, and stays quiet otherwise", () => {
    expect(levelChange("Beginner", "Intermediate", "Docker")).toBe("You're now Intermediate in Docker.");
    expect(levelChange("Intermediate", "Beginner", "Docker")).toBe("You're back to Beginner in Docker.");
    expect(levelChange("Beginner", "Beginner", "Docker")).toBe("");
  });
});


describe("levelRange", () => {
  it("gives each level's skill range", () => {
    expect(levelRange("Beginner")).toBe("0–99");
    expect(levelRange("Intermediate")).toBe("100–199");
    expect(levelRange("Advanced")).toBe("200–300");
  });
});

describe("aimText", () => {
  it("turns the target into a typical score", () => {
    expect(aimText(0.75)).toBe("about 3 right in 4, so expect 7 or 8 of 10 every time");
    expect(aimText(0.65)).toBe("about 2 right in 3, so expect 6 or 7 of 10 every time");
    expect(aimText(0.85)).toBe("about 6 right in 7, so expect 8 or 9 of 10 every time");
  });
});

describe("signed", () => {
  it("signs a gap to one decimal with a real minus", () => {
    expect(signed(1.24)).toBe("+1.2");
    expect(signed(-0.76)).toBe("−0.8");
    expect(signed(0.02)).toBe("0.0");
    expect(signed(-0.04)).toBe("0.0");
  });
});

describe("Elo wording", () => {
  it("shows the number with its margin", () => {
    expect(eloText(17, 100)).toBe("17 ± 100");
    expect(eloText(160, 27)).toBe("160 ± 27");
  });

  it("signs a change in whole points", () => {
    expect(eloDelta(148, 160)).toBe("+12");
    expect(eloDelta(154, 146)).toBe("−8");
    expect(eloDelta(150, 150)).toBe("no change");
  });

  it("calls a margin under 50 measured", () => {
    expect(measured(49)).toBe(true);
    expect(measured(50)).toBe(false);
  });

  it("explains big swings from 10 points", () => {
    const base = { answersInTopic: 8, p: 0.75, correct: true };
    expect(skillNote({ ...base, eloBefore: 100, eloAfter: 109 }, "Docker")).toBe("");
    expect(skillNote({ ...base, eloBefore: 100, eloAfter: 110 }, "Docker")).toContain("Big swings are normal early in a topic");
  });
});
