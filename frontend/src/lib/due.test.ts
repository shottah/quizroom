import { describe, expect, it } from "vitest";
import { dateText, dayLabel, dueText } from "./due";

// Wed 18 Nov 2026, 9:00 pm local time.
const now = new Date(2026, 10, 18, 21, 0);
const at = (...a: [number, number, number, number, number]) => new Date(...a).toISOString();

describe("dueText", () => {
  it("says when nothing is scheduled", () => {
    expect(dueText(null, now)).toBe("not scheduled");
    expect(dueText(undefined, now)).toBe("not scheduled");
  });

  it("says now for a time that has passed or is this minute", () => {
    expect(dueText(at(2026, 10, 18, 20, 59), now)).toBe("now");
    expect(dueText(now.toISOString(), now)).toBe("now");
    expect(dueText(at(2026, 9, 1, 8, 0), now)).toBe("now");
  });

  it("says today for later today, not tomorrow", () => {
    expect(dueText(at(2026, 10, 18, 21, 26), now)).toBe("today, 9:26 pm");
    expect(dueText(at(2026, 10, 18, 23, 59), now)).toBe("today, 11:59 pm");
  });

  it("says tomorrow for any time on the next day", () => {
    expect(dueText(at(2026, 10, 19, 0, 5), now)).toBe("tomorrow, 12:05 am");
    expect(dueText(at(2026, 10, 19, 21, 26), now)).toBe("tomorrow, 9:26 pm");
  });

  it("gives the weekday and date further out", () => {
    expect(dueText(at(2026, 10, 20, 9, 5), now)).toBe("Fri 20 Nov, 9:05 am");
    expect(dueText(at(2026, 10, 26, 12, 0), now)).toBe("Thu 26 Nov, 12:00 pm");
    expect(dueText(at(2026, 11, 9, 21, 26), now)).toBe("Wed 9 Dec, 9:26 pm");
  });

  it("crosses a month end", () => {
    const late = new Date(2026, 10, 30, 22, 0);
    expect(dueText(at(2026, 11, 1, 7, 30), late)).toBe("tomorrow, 7:30 am");
  });
});

describe("dateText", () => {
  it("is the weekday and date", () => {
    expect(dateText(at(2026, 10, 25, 21, 26))).toBe("Wed 25 Nov");
  });
});

describe("dayLabel", () => {
  it("names the day a chat message was sent", () => {
    expect(dayLabel(at(2026, 10, 18, 8, 0), now)).toBe("Today");
    expect(dayLabel(at(2026, 10, 17, 23, 59), now)).toBe("Yesterday");
    expect(dayLabel(at(2026, 10, 12, 9, 0), now)).toBe("Thu 12 Nov");
  });
});
