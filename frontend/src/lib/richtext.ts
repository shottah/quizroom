// A tiny, safe markdown subset for Claude's text: paragraphs, fenced code
// blocks, `inline code` and **bold**. Everything else stays literal text, and
// the RichText component renders it as React elements, never as raw HTML.

export type Inline = { kind: "text" | "code" | "bold"; text: string };
export type Block = { kind: "p"; parts: Inline[] } | { kind: "code"; lang: string; text: string };

const FENCE = /^```\s*([\w+-]*)\s*$/;
const CLOSE = /^```\s*$/;
const INLINE = /`([^`]+)`|\*\*([^*]+)\*\*/g;

export function parseInline(s: string): Inline[] {
  const out: Inline[] = [];
  let last = 0;
  for (const m of s.matchAll(INLINE)) {
    const at = m.index ?? 0;
    if (at > last) out.push({ kind: "text", text: s.slice(last, at) });
    if (m[1] !== undefined) out.push({ kind: "code", text: m[1] });
    else out.push({ kind: "bold", text: m[2] });
    last = at + m[0].length;
  }
  if (last < s.length) out.push({ kind: "text", text: s.slice(last) });
  return out;
}

export function parseRich(src: string): Block[] {
  const blocks: Block[] = [];
  const lines = src.replace(/\r\n/g, "\n").split("\n");
  let para: string[] = [];
  const flush = () => {
    if (para.length > 0) {
      blocks.push({ kind: "p", parts: parseInline(para.join("\n")) });
      para = [];
    }
  };
  for (let i = 0; i < lines.length; i++) {
    const fence = lines[i].match(FENCE);
    if (fence) {
      flush();
      const body: string[] = [];
      for (i++; i < lines.length && !CLOSE.test(lines[i]); i++) body.push(lines[i]);
      blocks.push({ kind: "code", lang: fence[1] ?? "", text: body.join("\n") });
      continue;
    }
    if (lines[i].trim() === "") flush();
    else para.push(lines[i]);
  }
  flush();
  return blocks;
}
