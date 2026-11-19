// The typed API client. Every type comes from schema.d.ts, which `make types`
// generates from the backend's OpenAPI description: never hand-write one.
import createClient from "openapi-fetch";
import type { components, paths } from "./schema";

export type S = components["schemas"];

export const api = createClient<paths>({ baseUrl: "" });

export class ApiError extends Error {
  readonly status: number;
  constructor(status: number, detail: string) {
    super(detail);
    this.status = status;
  }
}

type Res<T> = { data?: T; error?: unknown; response: Response };

/** Returns the response data, or throws ApiError with the problem's detail. */
export function unwrap<T>(r: Res<T>): T {
  if (r.error !== undefined || !r.response.ok) {
    const e = (r.error ?? {}) as { detail?: string; title?: string };
    throw new ApiError(r.response.status, e.detail || e.title || r.response.statusText || "Request failed");
  }
  return r.data as T;
}

export function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** The stack is down: a network error or a 5xx. The global banner says so, so pages stay quiet. */
export function isDown(err: unknown): boolean {
  if (err === null || err === undefined) return false;
  return !(err instanceof ApiError && err.status < 500);
}

/** A page's own error to show, or null when the global "can't reach Quizroom" banner already explains it. */
export function visibleError(err: unknown, stackDown: boolean): string | null {
  if (err === null || err === undefined) return null;
  if (stackDown && isDown(err)) return null;
  return message(err);
}
