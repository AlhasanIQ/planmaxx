import { describe, expect, test } from "bun:test";
import { finalizeIterationInstruction, wholePlanAnchor } from "../src/lib/finalizeIteration";

describe("finalize iteration helpers", () => {
  test("targets the complete non-empty plan without a trailing blank line", () => {
    expect(wholePlanAnchor("# Plan\n\n- One\n")).toEqual({ startLine: 1, endLine: 3 });
    expect(wholePlanAnchor("")).toEqual({ startLine: 1, endLine: 1 });
  });

  test("preserves edited review feedback in a whole-plan iteration instruction", () => {
    expect(
      finalizeIterationInstruction({
        summary: "Make the rollout safer.",
        reviewerDecisions: ["Move validation before migration."],
        promotedSideAnswers: ["Question: Why?\nAnswer: It protects existing users."],
      }),
    ).toContain("Move validation before migration.");
  });
});
