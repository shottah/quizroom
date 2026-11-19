import { plural } from "../hooks";

/** The toast shown when a chat job finishes while the chat is closed. */
export function toastText(job: { status: string; added: number }, topicName: string): string {
  if (job.status === "failed") return "Claude couldn't finish. Open the chat to retry.";
  if (job.added === 0) return "Claude replied.";
  const noun = topicName ? `${topicName} question` : "question";
  return `${plural(job.added, noun, `${noun}s`)} added.`;
}
