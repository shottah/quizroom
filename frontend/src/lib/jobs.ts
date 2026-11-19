// Which chat job the bubble is following. Finished jobs are remembered so a
// stale "pending_job" in cached history can never re-start tracking one.

export type Jobs = { tracking: number | null; finished: number[] };

export const initialJobs: Jobs = { tracking: null, finished: [] };

/** Follow a job the learner just started (unless it already finished). */
export function track(s: Jobs, id: number): Jobs {
  if (s.finished.includes(id)) return s;
  return { ...s, tracking: id };
}

/** Follow a job the server says is pending, if nothing is followed yet. */
export function pickUp(s: Jobs, pending: number | null | undefined): Jobs {
  if (pending == null || s.tracking !== null || s.finished.includes(pending)) return s;
  return { ...s, tracking: pending };
}

/** A job reached done or failed. */
export function finish(s: Jobs, id: number): Jobs {
  return {
    tracking: s.tracking === id ? null : s.tracking,
    finished: [...s.finished.filter((x) => x !== id), id].slice(-50),
  };
}
