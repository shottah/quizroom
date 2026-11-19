import { describe, expect, it } from "vitest";
import { finish, initialJobs, pickUp, track } from "./jobs";

// Final review #1: after a job finished, the chat's cached history still said
// pending_job = that job, so the bubble re-tracked it, saw "done" again, and
// looped forever without ever showing the reply.
describe("chat job tracking", () => {
  it("picks up a job that was still pending before a refresh", () => {
    expect(pickUp(initialJobs, 3).tracking).toBe(3);
  });

  it("never re-tracks a job that already finished", () => {
    const done = finish(track(initialJobs, 7), 7);
    expect(done.tracking).toBeNull();
    expect(pickUp(done, 7).tracking).toBeNull(); // stale history still lists it
    expect(track(done, 7).tracking).toBeNull();
  });

  it("keeps following the current job when history names another", () => {
    expect(pickUp(track(initialJobs, 4), 9).tracking).toBe(4);
  });

  it("follows a new job after one finishes", () => {
    expect(track(finish(track(initialJobs, 1), 1), 2).tracking).toBe(2);
  });

  it("ignores a finish for a job it isn't following", () => {
    expect(finish(track(initialJobs, 5), 6).tracking).toBe(5);
  });
});
