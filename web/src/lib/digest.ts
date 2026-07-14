import type { Digest, Session } from "../types";

export function digestForIteration(digest: Digest, initialSummary: string): Digest {
  const summary = digest.summary.trim();
  const untouchedApprovalDefault = summary === initialSummary.trim() && /^Approved (with review|without) comments\.$/.test(summary);
  if (!untouchedApprovalDefault) return digest;
  const hasFeedback = digest.reviewerDecisions.length > 0 || digest.promotedSideAnswers.length > 0;
  return {
    ...digest,
    summary: hasFeedback
      ? "Not approved yet; revise the plan using this review feedback."
      : "Not approved yet; revise the complete plan before approval.",
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
  const openThreadIds = new Set(
    session.threads
      .filter((thread) => (thread.status ?? "open") === "open")
      .map((thread) => thread.id),
  );
  for (const a of session.sideAnswers) {
    if (a.promoted && openThreadIds.has(a.threadId)) promoted++;
    else ephemeral++;
  }
  return { decisions, notes, promoted, ephemeral };
}
