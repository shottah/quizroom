import { describe, expect, it } from "vitest";
import { parseRich } from "./richtext";

describe("parseRich", () => {
  it("keeps HTML as literal text", () => {
    expect(parseRich("<script>alert(1)</script>")).toEqual([
      { kind: "p", parts: [{ kind: "text", text: "<script>alert(1)</script>" }] },
    ]);
  });

  it("turns a fenced block into code with its language", () => {
    expect(parseRich("Look:\n```yaml\nkind: Pod\n```")).toEqual([
      { kind: "p", parts: [{ kind: "text", text: "Look:" }] },
      { kind: "code", lang: "yaml", text: "kind: Pod" },
    ]);
  });

  it("finds inline code and bold", () => {
    expect(parseRich("run `kubectl get pods`, **not** etcd")).toEqual([
      {
        kind: "p",
        parts: [
          { kind: "text", text: "run " },
          { kind: "code", text: "kubectl get pods" },
          { kind: "text", text: ", " },
          { kind: "bold", text: "not" },
          { kind: "text", text: " etcd" },
        ],
      },
    ]);
  });

  it("runs an unclosed fence to the end", () => {
    expect(parseRich("```\nFROM alpine\nRUN apk add curl")).toEqual([
      { kind: "code", lang: "", text: "FROM alpine\nRUN apk add curl" },
    ]);
  });

  it("splits paragraphs on blank lines", () => {
    expect(parseRich("one\n\ntwo")).toHaveLength(2);
  });
});
