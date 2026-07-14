import { describe, expect, test } from "bun:test";
import { digestForIteration } from "../src/lib/digest";

describe("digest helpers", () => {
  test("replaces untouched approval defaults when the reviewer iterates", () => {
    expect(digestForIteration({
      summary: "Approved with review comments.",
      reviewerDecisions: ["Change it"],
      promotedSideAnswers: [],
    }, "Approved with review comments.").summary).toBe(
      "Not approved yet; revise the plan using this review feedback.",
    );
    expect(digestForIteration({
      summary: "Use my explicit iteration summary",
      reviewerDecisions: ["Change it"],
      promotedSideAnswers: [],
    }, "Approved with review comments.").summary).toBe("Use my explicit iteration summary");
  });

  test("uses a useful iteration direction when there is no feedback", () => {
    expect(digestForIteration({
      summary: "Approved without comments.",
      reviewerDecisions: [],
      promotedSideAnswers: [],
    }, "Approved without comments.").summary).toBe(
      "Not approved yet; revise the complete plan before approval.",
    );
  });
});
