import { describe, expect, it } from "vitest";
import { checkDots, groupMistakes, returnLine, type MistakeLike } from "./mistakes";

const now = new Date(2026, 10, 18, 21, 0);
const m = (id: number, status: string, due: Date | null, stage = 0): MistakeLike => ({
  id, status, stage, due_at: due ? due.toISOString() : null,
});

describe("groupMistakes", () => {
  it("splits by when each comes back", () => {
    const g = groupMistakes(
      [
        m(1, "active", new Date(2026, 10, 18, 20, 0)),
        m(2, "active", new Date(2026, 10, 25, 9, 0), 3),
        m(3, "active", new Date(2026, 10, 19, 9, 0), 1),
        m(4, "resolved", null, 4),
        m(5, "watching", new Date(2026, 10, 1)),
        m(6, "active", new Date(2026, 10, 18, 21, 0)),
      ],
      now,
    );
    expect(g.next.map((x) => x.id)).toEqual([1, 6]);
    expect(g.later.map((x) => x.id)).toEqual([3, 2]);
    expect(g.done.map((x) => x.id)).toEqual([4]);
    expect(g.traps.map((x) => x.id)).toEqual([5]);
  });
});

describe("checkDots", () => {
  it("fills one dot per check passed, all five when done", () => {
    expect(checkDots(0, "active")).toBe("○○○○○");
    expect(checkDots(2, "active")).toBe("●●○○○");
    expect(checkDots(4, "resolved")).toBe("●●●●●");
  });
});

describe("groupMistakes, waiting for a question", () => {
  it("keeps due mistakes with no fitting question out of 'next quiz'", () => {
    const g = groupMistakes([{ ...m(1, "active", new Date(2026, 10, 18, 20, 0)), needs_questions: true }, m(2, "active", new Date(2026, 10, 18, 20, 0))], now);
    expect(g.next.map((x) => x.id)).toEqual([2]);
    expect(g.waiting.map((x) => x.id)).toEqual([1]);
  });
});

describe("returnLine", () => {
  const note = (done: boolean, due: Date | null, next_check = 1) => ({ done, next_due: due ? due.toISOString() : null, next_check });
  it("says where a mistake stands now, not where it stood", () => {
    expect(returnLine(note(true, null, 0), now)).toBe("Done: you've got this one.");
    expect(returnLine(note(false, new Date(2026, 10, 18, 20, 0)), now)).toBe("Due now: a coming quiz retests it with a new question.");
    expect(returnLine(note(false, new Date(2026, 10, 19, 21, 26), 2), now)).toBe("Next check from tomorrow, 9:26 pm (check 2 of 5).");
  });
});
