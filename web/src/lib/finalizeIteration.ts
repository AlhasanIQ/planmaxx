import type { Anchor, Digest } from "../types";

export function wholePlanAnchor(plan: string): Anchor {
  const lines = plan.split(/\r?\n/);
  if (lines.length > 1 && lines.at(-1) === "") lines.pop();
  return { startLine: 1, endLine: Math.max(1, lines.length) };
}

export function finalizeIterationInstruction(digest: Digest): string {
  const sections: string[] = [
    "Use the final-review feedback below as the authoritative instruction. Revise the complete plan and keep unaffected content accurate and coherent.",
  ];
  if (digest.summary.trim()) sections.push(`Review summary:\n${digest.summary.trim()}`);
  if (digest.reviewerDecisions.length > 0) {
    sections.push(`Reviewer decisions:\n${digest.reviewerDecisions.map((item) => `- ${item}`).join("\n")}`);
  }
  if (digest.promotedSideAnswers.length > 0) {
    sections.push(`Promoted /btw Q+A:\n${digest.promotedSideAnswers.map((item) => `- ${item}`).join("\n")}`);
  }
  return sections.join("\n\n");
}
