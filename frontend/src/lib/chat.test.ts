import { describe, expect, it } from "vitest";
import { toastText } from "./chat";

describe("toastText", () => {
  it("says what a finished job did", () => {
    expect(toastText({ status: "done", added: 15 }, "Kubernetes")).toBe("15 Kubernetes questions added.");
    expect(toastText({ status: "done", added: 1 }, "")).toBe("1 question added.");
    expect(toastText({ status: "done", added: 0 }, "Docker")).toBe("Claude replied.");
    expect(toastText({ status: "failed", added: 0 }, "Docker")).toBe("Claude couldn't finish. Open the chat to retry.");
  });
});
