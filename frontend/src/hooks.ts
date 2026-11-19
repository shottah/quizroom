import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "./api/client";
import { useChat } from "./chatContext";
import { go } from "./router";

/** Starts a practice quiz, or a skill check, and opens it. */
export function useStartQuiz(kind: "practice" | "check" = "practice") {
  return useMutation({
    mutationFn: async (slug: string) => unwrap(await api.POST("/api/quizzes", { body: { topic: slug, kind } })),
    onSuccess: (s) => go(`#/quiz/${s.quiz_id}`),
  });
}

/** "Ask Claude for more" / "Write my first 30 questions": opens the chat and follows the job. */
export function useGenerate() {
  const { setOpen, trackJob, pendingJob } = useChat();
  const qc = useQueryClient();
  const m = useMutation({
    mutationFn: async (slug: string) =>
      unwrap(await api.POST("/api/topics/{slug}/generate", { params: { path: { slug } } })),
    onSuccess: (a) => {
      trackJob(a.job_id);
      setOpen(true);
      void qc.invalidateQueries({ queryKey: ["chat"] });
    },
  });
  return { ...m, busy: m.isPending || pendingJob !== null };
}

/** Claude's sign-in state and the stack's health; shared with Settings. */
export function useStatus() {
  return useQuery({ queryKey: ["status"], queryFn: async () => unwrap(await api.GET("/api/status")), staleTime: 30_000 });
}

export function useSettings() {
  return useQuery({ queryKey: ["settings"], queryFn: async () => unwrap(await api.GET("/api/settings")) });
}

/** A, B, C… for an option's position on screen. */
export const letter = (i: number) => String.fromCharCode(65 + i);


export function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}
