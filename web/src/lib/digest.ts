import type { Digest } from "../types";

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
