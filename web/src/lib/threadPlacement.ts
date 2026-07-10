import type { SideAnswer, Thread } from "../types";

export function visibleThreads(
  threads: Thread[],
  sideAnswers: SideAnswer[],
  filter: string,
  focusedThreadId: string | null,
): Thread[] {
  const normalized = filter.trim().toLowerCase();
  const answersByThread = groupSideAnswersByThread(sideAnswers);
  const filtered = normalized
    ? threads.filter((thread) => threadSearchText(thread, answersByThread.get(thread.id) ?? []).includes(normalized))
    : threads;
  const focused = focusedThreadId ? threads.find((thread) => thread.id === focusedThreadId) : undefined;
  return focused && !filtered.some((thread) => thread.id === focused.id)
    ? [focused, ...filtered]
    : filtered;
}

// A range's discussion belongs after its final source line. This guarantees a
// multi-line or overlapping comment never covers text that follows its range.
export function threadsByAnchorEnd(threads: Thread[]): Map<number, Thread[]> {
  const grouped = new Map<number, Thread[]>();
  for (const thread of threads) {
    const endLine = thread.anchor.endLine;
    const current = grouped.get(endLine);
    if (current) current.push(thread);
    else grouped.set(endLine, [thread]);
  }
  return grouped;
}

export function groupSideAnswersByThread(sideAnswers: SideAnswer[]): Map<string, SideAnswer[]> {
  const grouped = new Map<string, SideAnswer[]>();
  for (const answer of sideAnswers) {
    const current = grouped.get(answer.threadId);
    if (current) current.push(answer);
    else grouped.set(answer.threadId, [answer]);
  }
  return grouped;
}

function threadSearchText(thread: Thread, sideAnswers: SideAnswer[]): string {
  const messages = thread.messages.map((message) => message.body).join("\n");
  const answers = sideAnswers.map((answer) => `${answer.question}\n${answer.answer}`).join("\n");
  return `${messages}\n${answers}`.toLowerCase();
}
