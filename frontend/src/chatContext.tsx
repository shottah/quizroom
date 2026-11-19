import { createContext, useCallback, useContext, useEffect, useMemo, useReducer, useState, type ReactNode } from "react";
import type { S } from "./api/client";
import { finish, initialJobs, pickUp, track, type Jobs } from "./lib/jobs";

export type ChatPage = S["PageContext"];

export const homePage: ChatPage = { page: "home", topic: "", question_id: 0, chosen: [] };

type ChatState = {
  page: ChatPage;
  setPage: (p: ChatPage) => void;
  open: boolean;
  setOpen: (open: boolean) => void;
  pendingJob: number | null;
  /** Follow a job the learner just started. */
  trackJob: (id: number) => void;
  /** Follow a job the server reports as pending (ignored once it has finished). */
  pickUpJob: (id: number | null | undefined) => void;
  finishJob: (id: number) => void;
  /** The chat box's text, kept here so other screens can fill it in. */
  draft: string;
  setDraft: (text: string) => void;
  /** Opens the chat with text ready to edit and send. */
  prefill: (text: string) => void;
  /** Counts prefills, so the chat box can take focus each time. */
  prefillTick: number;
  /** What the screen adds to the chat box: an "About: …" chip and a placeholder. */
  aside: ChatAside;
  setAside: (a: ChatAside) => void;
};

export type ChatAside = { about: string; placeholder: string };
const noAside: ChatAside = { about: "", placeholder: "" };

type Action = { type: "track" | "finish"; id: number } | { type: "pickUp"; id: number | null | undefined };

function reduce(s: Jobs, a: Action): Jobs {
  if (a.type === "track") return track(s, a.id);
  if (a.type === "finish") return finish(s, a.id);
  return pickUp(s, a.id);
}

const ChatContext = createContext<ChatState | null>(null);

export function ChatPageProvider({ children }: { children: ReactNode }) {
  const [page, setPage] = useState<ChatPage>(homePage);
  const [open, setOpen] = useState(false);
  const [jobs, dispatch] = useReducer(reduce, initialJobs);
  const trackJob = useCallback((id: number) => dispatch({ type: "track", id }), []);
  const pickUpJob = useCallback((id: number | null | undefined) => dispatch({ type: "pickUp", id }), []);
  const finishJob = useCallback((id: number) => dispatch({ type: "finish", id }), []);
  const [draft, setDraft] = useState("");
  const [aside, setAside] = useState<ChatAside>(noAside);
  const [prefillTick, setPrefillTick] = useState(0);
  const prefill = useCallback((text: string) => {
    setDraft(text);
    setOpen(true);
    setPrefillTick((n) => n + 1);
  }, []);
  const value = useMemo<ChatState>(
    () => ({ page, setPage, open, setOpen, pendingJob: jobs.tracking, trackJob, pickUpJob, finishJob, draft, setDraft, prefill, prefillTick, aside, setAside }),
    [page, open, jobs.tracking, trackJob, pickUpJob, finishJob, draft, prefill, prefillTick, aside],
  );
  return <ChatContext.Provider value={value}>{children}</ChatContext.Provider>;
}

export function useChat(): ChatState {
  const ctx = useContext(ChatContext);
  if (!ctx) throw new Error("useChat outside ChatPageProvider");
  return ctx;
}

/** Tells the chat what the learner is looking at. */
export function useSetChatPage(p: ChatPage): void {
  const { setPage } = useChat();
  const key = JSON.stringify(p);
  useEffect(() => {
    setPage(JSON.parse(key) as ChatPage);
  }, [key, setPage]);
}

export function useChatPage(): ChatPage {
  return useChat().page;
}

/** Sets the chat's "About: …" chip and placeholder while a screen is shown. */
export function useChatAside(about: string, placeholder: string): void {
  const { setAside } = useChat();
  useEffect(() => {
    setAside({ about, placeholder });
    return () => setAside(noAside);
  }, [about, placeholder, setAside]);
}
