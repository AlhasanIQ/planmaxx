import type { Digest, Session } from "../types";
import { promotedSideAnswerText } from "./selectionContext";

export function buildDigestDraft(session: Session): Digest {
  const decisions: string[] = [];
  for (const thread of session.threads) {
    if ((thread.kind ?? "decision") !== "decision") continue;
    if ((thread.status ?? "open") !== "open") continue;
    for (const message of thread.messages) {
      decisions.push(message.body);
    }
  }
  const promoted: string[] = [];
  for (const answer of session.sideAnswers) {
    if (!answer.promoted) continue;
    promoted.push(promotedSideAnswerText(session, answer.id) ?? answer.answer);
  }
  const hasContent = decisions.length > 0 || promoted.length > 0;
  return {
    summary: hasContent ? "Approved with review comments." : "Approved without comments.",
    reviewerDecisions: decisions,
    promotedSideAnswers: promoted,
  };
}

export interface HandoffCounts {
  decisions: number;
  notes: number;
  promoted: number;
  ephemeral: number;
}

export function countHandoff(session: Session): HandoffCounts {
  let decisions = 0;
  let notes = 0;
  for (const thread of session.threads) {
    if ((thread.kind ?? "decision") === "decision" && (thread.status ?? "open") === "open") {
      decisions++;
    } else {
      notes++;
    }
  }
  let promoted = 0;
  let ephemeral = 0;
  for (const a of session.sideAnswers) {
    if (a.promoted) promoted++;
    else ephemeral++;
  }
  return { decisions, notes, promoted, ephemeral };
}
