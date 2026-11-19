// Exact, local-time wording for when something happens next.

const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** "9:26 pm" */
export function clockText(d: Date): string {
  const h = d.getHours();
  return `${h % 12 === 0 ? 12 : h % 12}:${String(d.getMinutes()).padStart(2, "0")} ${h < 12 ? "am" : "pm"}`;
}

/** "Wed 25 Nov" */
export function dateText(iso: string): string {
  const d = new Date(iso);
  return `${DAYS[d.getDay()]} ${d.getDate()} ${MONTHS[d.getMonth()]}`;
}

const dayStart = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();

/** "now", "today, 9:26 pm", "tomorrow, 9:26 pm" or "Thu 19 Nov, 9:26 pm". */
export function dueText(dueAt: string | null | undefined, now: Date): string {
  if (!dueAt) return "not scheduled";
  const due = new Date(dueAt);
  if (due.getTime() <= now.getTime()) return "now";
  const days = Math.round((dayStart(due) - dayStart(now)) / 86_400_000);
  if (days === 0) return `today, ${clockText(due)}`;
  if (days === 1) return `tomorrow, ${clockText(due)}`;
  return `${dateText(dueAt)}, ${clockText(due)}`;
}

/** "Today", "Yesterday" or "Thu 12 Nov", for chat date dividers. */
export function dayLabel(iso: string, now: Date): string {
  const days = Math.round((dayStart(now) - dayStart(new Date(iso))) / 86_400_000);
  if (days === 0) return "Today";
  if (days === 1) return "Yesterday";
  return dateText(iso);
}
