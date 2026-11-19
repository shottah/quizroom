import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Fragment, useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { api, message, unwrap, type S } from "../api/client";
import { useChat } from "../chatContext";
import { useStatus } from "../hooks";
import { toastText } from "../lib/chat";
import { dayLabel } from "../lib/due";
import { RichText } from "./RichText";

const DEFAULT_PLACEHOLDER = "Ask Claude to write questions, explain something, or note a mistake…";

const CHIPS = [
  { text: "Write 10 questions on Kubernetes Services", hint: "Claude adds them to your question bank in 1–3 minutes." },
  { text: "Why is my answer wrong?", hint: "Ask on a question's feedback: Claude sees the question and your answer." },
  { text: "I always mix up CMD and ENTRYPOINT", hint: "Claude notes it as a mistake; your next Write 15 includes questions on it." },
];

function elapsed(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

const dayKey = (iso: string) => new Date(iso).toDateString();

type Toast = { text: string; topic: string; topicName: string };

export function ChatBubble() {
  const { open, setOpen, page, pendingJob, trackJob, pickUpJob, finishJob, draft: text, setDraft: setText, aside, prefillTick } =
    useChat();
  const qc = useQueryClient();
  const status = useStatus();
  const auth = status.data?.auth ?? "";
  const cantWrite = auth === "none" || auth === "unreachable";
  const [startedAt, setStartedAt] = useState<number | null>(null);
  const [now, setNow] = useState(Date.now());
  const [toast, setToast] = useState<Toast | null>(null);
  const openRef = useRef(open);
  openRef.current = open;
  const listRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const fabRef = useRef<HTMLButtonElement>(null);
  const wasOpen = useRef(open);

  // Opening (or a prefill) puts the cursor in the chat box; closing returns
  // focus to the chat button.
  useEffect(() => {
    if (open) inputRef.current?.focus();
    else if (wasOpen.current) fabRef.current?.focus();
    wasOpen.current = open;
  }, [open, prefillTick]);

  const history = useQuery({ queryKey: ["chat"], queryFn: async () => unwrap(await api.GET("/api/chat")) });

  // Pick up a job that was still running before a page refresh. Finished jobs
  // are never picked up again, even if cached history still lists them.
  useEffect(() => {
    pickUpJob(history.data?.pending_job);
  }, [history.data?.pending_job, pickUpJob]);

  useEffect(() => {
    setStartedAt(pendingJob === null ? null : Date.now());
  }, [pendingJob]);

  useEffect(() => {
    if (startedAt === null) return;
    const t = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, [startedAt]);

  const job = useQuery({
    queryKey: ["job", pendingJob],
    enabled: pendingJob !== null,
    refetchInterval: 2000,
    queryFn: async () => unwrap(await api.GET("/api/jobs/{id}", { params: { path: { id: pendingJob ?? 0 } } })),
  });

  useEffect(() => {
    const j = job.data;
    if (!j || (j.status !== "done" && j.status !== "failed")) return;
    finishJob(j.id);
    qc.setQueryData<S["History"]>(["chat"], (h) => (h ? { ...h, pending_job: null } : h));
    void qc.invalidateQueries({ queryKey: ["chat"] });
    void qc.invalidateQueries({ queryKey: ["topics"] });
    void qc.invalidateQueries({ queryKey: ["topic"] });
    void qc.invalidateQueries({ queryKey: ["status"] });
    if (!openRef.current) {
      const cards = qc.getQueryData<S["Card"][]>(["topics"]) ?? [];
      const name = cards.find((c) => c.slug === j.topic)?.name ?? "";
      setToast({ text: toastText(j, name), topic: j.status === "done" && j.added > 0 ? j.topic : "", topicName: name });
    }
  }, [job.data, finishJob, qc]);

  useEffect(() => {
    if (!toast) return;
    const t = window.setTimeout(() => setToast(null), 10_000);
    return () => window.clearTimeout(t);
  }, [toast]);

  // Wide screens dock the open chat as a column instead of covering the page.
  useEffect(() => {
    document.body.classList.toggle("chat-open", open);
    return () => document.body.classList.remove("chat-open");
  }, [open]);

  const afterQueue = (a: S["Accepted"]) => {
    trackJob(a.job_id);
    void qc.invalidateQueries({ queryKey: ["chat"] });
  };
  const send = useMutation({
    mutationFn: async (t: string) => unwrap(await api.POST("/api/chat", { body: { text: t, page } })),
    onSuccess: (a) => {
      setText("");
      afterQueue(a);
    },
  });
  const retry = useMutation({
    mutationFn: async (id: number) => unwrap(await api.POST("/api/jobs/{id}/retry", { params: { path: { id } } })),
    onSuccess: afterQueue,
  });

  const messages = history.data?.messages ?? [];
  useEffect(() => {
    listRef.current?.scrollTo({ top: listRef.current.scrollHeight });
  }, [messages.length, open, pendingJob]);

  const busy = pendingJob !== null || send.isPending || retry.isPending;
  const timer = startedAt !== null ? elapsed(now - startedAt) : "";
  const submit = (e?: FormEvent) => {
    e?.preventDefault();
    if (text.trim() && !busy && !cantWrite) send.mutate(text.trim());
  };
  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) submit(e);
  };
  const fix = (
    <a href="#/settings" onClick={() => setOpen(false)}>
      How to fix →
    </a>
  );

  if (!open) {
    return (
      <>
        {toast ? (
          <div className="toast" role="status">
            <span>{toast.text}</span>
            <button
              className="link"
              onClick={() => {
                setToast(null);
                setOpen(true);
              }}
            >
              Open chat
            </button>
            {toast.topic ? (
              <a href={`#/topic/${toast.topic}`} onClick={() => setToast(null)}>
                See {toast.topicName || "topic"} →
              </a>
            ) : null}
            <button className="icon" aria-label="Dismiss" onClick={() => setToast(null)}>
              ×
            </button>
          </div>
        ) : null}
        <button className="chat-fab" ref={fabRef} onClick={() => setOpen(true)} aria-label="Open the chat">
          <span aria-hidden="true">✦</span> {busy ? `Claude is writing… ${timer}` : "Ask Claude"}
        </button>
      </>
    );
  }

  const last = messages.length - 1;
  return (
    <section
      className="chat-panel"
      aria-label="Chat with Claude"
      onKeyDown={(e) => {
        if (e.key === "Escape") setOpen(false);
      }}
    >
      <header className="chat-head">
        <div>
          <strong>Claude</strong>
          <span className="muted">: writes questions, explains answers, notes your mistakes</span>
        </div>
        <button className="icon" onClick={() => setOpen(false)} aria-label="Close the chat">
          ×
        </button>
      </header>
      {cantWrite ? (
        <p className="chat-banner">
          {auth === "none"
            ? "Claude isn't signed in, so chat and question writing won't work. "
            : "Can't reach Claude's runner, so chat and question writing won't work. "}
          {fix}
        </p>
      ) : null}
      <div className="chat-list" ref={listRef}>
        {messages.length === 0 ? (
          <div className="chat-empty">
            <p className="muted">Try one:</p>
            {CHIPS.map((c) => (
              <div key={c.text} className="chat-try">
                <button className="chip" onClick={() => setText(c.text)}>
                  {c.text}
                </button>
                <p className="small muted">{c.hint}</p>
              </div>
            ))}
            <p className="small muted">Each message uses your Claude plan. One at a time.</p>
          </div>
        ) : null}
        {messages.map((m, i) => (
          <Fragment key={i}>
            {i === 0 || dayKey(messages[i - 1].at) !== dayKey(m.at) ? <div className="chat-day">{dayLabel(m.at, new Date())}</div> : null}
            {m.kind === "request" ? (
              <div className="msg-system">{m.text}</div>
            ) : m.failed ? (
              <div className="msg msg-failed">
                <RichText text={m.text} />
                {i === last && m.job_id !== null ? (
                  <button className="secondary retry" disabled={busy} onClick={() => retry.mutate(m.job_id ?? 0)}>
                    Retry
                  </button>
                ) : null}
              </div>
            ) : (
              <div className={`msg msg-${m.role}`}>
                <RichText text={m.text} />
              </div>
            )}
          </Fragment>
        ))}
        {pendingJob !== null ? (
          <div className="msg msg-assistant msg-pending">
            Claude is writing… {timer} · usually 1–3 min. You can keep using Quizroom.
          </div>
        ) : null}
      </div>
      {send.error ? <p className="error chat-error">{message(send.error)}</p> : null}
      {retry.error ? <p className="error chat-error">{message(retry.error)}</p> : null}
      {aside.about ? <p className="chat-about">About: {aside.about}</p> : null}
      <form className="chat-form" onSubmit={submit}>
        <textarea
          ref={inputRef}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={onKey}
          placeholder={aside.placeholder || DEFAULT_PLACEHOLDER}
          rows={2}
          maxLength={4000}
        />
        <button
          className="primary"
          type="submit"
          disabled={busy || cantWrite || !text.trim()}
          title={cantWrite ? "Claude isn't available: see the note above" : busy ? "Claude is busy with your last request." : undefined}
        >
          Send
        </button>
      </form>
      <p className="chat-hint small muted">{busy ? "Claude is busy with your last request." : "Enter to send · Shift+Enter for a new line"}</p>
    </section>
  );
}
