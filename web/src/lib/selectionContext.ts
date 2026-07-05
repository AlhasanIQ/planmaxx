import type { Anchor, Session, Thread } from "../types";
import { planExcerpt } from "./markdown";

export interface SideQuestionContext {
  filePath: string;
  reference: string;
  selectedText: string;
  planExcerpt: string;
}

export function sideQuestionContext(session: Session, thread: Thread): SideQuestionContext {
  const planExcerptText = planExcerpt(session.plan, thread.anchor.startLine, thread.anchor.endLine);
  const storedSelection = thread.selectedText ?? "";
  const sourceSelection = selectedTextForAnchor(session.plan, thread.anchor);
  const selectedText = storedSelection.trim()
    ? storedSelection
    : sourceSelection.trim()
      ? sourceSelection
      : planExcerptText;
  return {
    filePath: session.planPath || "",
    reference: anchorReference(session.planPath || "", thread.anchor),
    selectedText,
    planExcerpt: planExcerptText,
  };
}

function selectedTextForAnchor(plan: string, anchor: Anchor): string {
  const lines = splitPlanLines(plan);
  const start = Math.max(1, anchor.startLine);
  const end = Math.max(start, anchor.endLine);
  const parts: string[] = [];
  for (let lineNumber = start; lineNumber <= end; lineNumber++) {
    const line = lines[lineNumber - 1] ?? "";
    if (!hasCharacterRange(anchor)) {
      parts.push(line);
      continue;
    }
    if (lineNumber === start && lineNumber === end) {
      parts.push(line.slice(anchor.startChar ?? 0, anchor.endChar ?? line.length));
    } else if (lineNumber === start) {
      parts.push(line.slice(anchor.startChar ?? 0));
    } else if (lineNumber === end) {
      parts.push(line.slice(0, anchor.endChar ?? line.length));
    } else {
      parts.push(line);
    }
  }
  return parts.join("\n");
}

function anchorReference(filePath: string, anchor: Anchor): string {
  const path = filePath || "(unknown file)";
  if (!hasCharacterRange(anchor)) {
    return anchor.startLine === anchor.endLine
      ? `${path}:${anchor.startLine}`
      : `${path}:${anchor.startLine}-${anchor.endLine}`;
  }
  return `${path}:${anchor.startLine}:${column(anchor.startChar)}-${anchor.endLine}:${column(anchor.endChar)}`;
}

export function promotedSideAnswerText(session: Session, answerId: string): string | null {
  const answer = session.sideAnswers.find((a) => a.id === answerId);
  if (!answer) return null;
  const thread = session.threads.find((t) => t.id === answer.threadId);
  const parts: string[] = [];
  if (thread) {
    const context = sideQuestionContext(session, thread);
    parts.push(`/btw context: ${context.reference}`);
    if (context.selectedText.trim()) {
      parts.push(`Selected text:\n${context.selectedText}`);
    }
  }
  parts.push(`Question:\n${answer.question}`);
  parts.push(`Answer:\n${answer.answer}`);
  return parts.join("\n");
}

function hasCharacterRange(anchor: Anchor): boolean {
  return (anchor.startChar ?? 0) !== 0 || (anchor.endChar ?? 0) !== 0;
}

function column(offset: number | undefined): number {
  return (offset ?? 0) + 1;
}

function splitPlanLines(plan: string): string[] {
  const lines = plan.split(/\r?\n/);
  if (lines.length > 1 && lines[lines.length - 1] === "") {
    lines.pop();
  }
  return lines;
}
