import { Fragment } from "react";
import { parseInline, parseRich, type Inline } from "../lib/richtext";

function Parts({ parts }: { parts: Inline[] }) {
  return (
    <>
      {parts.map((p, i) =>
        p.kind === "code" ? (
          <code key={i}>{p.text}</code>
        ) : p.kind === "bold" ? (
          <strong key={i}>{p.text}</strong>
        ) : (
          <Fragment key={i}>{p.text}</Fragment>
        ),
      )}
    </>
  );
}

/** Claude's text as React elements: never raw HTML. */
export function RichText({ text, className = "" }: { text: string; className?: string }) {
  return (
    <div className={`rich ${className}`}>
      {parseRich(text).map((b, i) =>
        b.kind === "code" ? (
          <pre key={i} className="code">
            <code data-lang={b.lang}>{b.text}</code>
          </pre>
        ) : (
          <p key={i}>
            <Parts parts={b.parts} />
          </p>
        ),
      )}
    </div>
  );
}

export function InlineText({ text }: { text: string }) {
  return <Parts parts={parseInline(text)} />;
}
