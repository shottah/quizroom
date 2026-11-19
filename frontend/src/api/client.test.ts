import { describe, expect, it } from "vitest";
import { ApiError, isDown, visibleError } from "./client";

describe("isDown", () => {
  it("is a network error or a 5xx, not a 4xx or no error", () => {
    expect(isDown(new TypeError("Failed to fetch"))).toBe(true);
    expect(isDown(new ApiError(502, "Bad Gateway"))).toBe(true);
    expect(isDown(new ApiError(404, "not found"))).toBe(false);
    expect(isDown(new ApiError(409, "No quiz right now"))).toBe(false);
    expect(isDown(null)).toBe(false);
  });
});

describe("visibleError", () => {
  it("shows a page's own error unless the global banner already explains it", () => {
    expect(visibleError(new ApiError(502, "Bad Gateway"), false)).toBe("Bad Gateway");
    expect(visibleError(new ApiError(500, "something went wrong"), false)).toBe("something went wrong");
    expect(visibleError(new ApiError(502, "Bad Gateway"), true)).toBeNull();
    expect(visibleError(new ApiError(404, "not found"), true)).toBe("not found");
    expect(visibleError(null, false)).toBeNull();
  });
});
