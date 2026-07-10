import { describe, expect, test } from "bun:test";
import { parseDiffFromFile } from "@pierre/diffs";

describe("Pierre revision diff", () => {
  test("creates a unified diff from revision contents", () => {
    const diff = parseDiffFromFile(
      { name: "plan.md", contents: "# Plan\n- Before\n- Old\n- After" },
      { name: "plan.md", contents: "# Plan\n- Before\n- New\n- After" },
    );

    expect(diff.hunks).toHaveLength(1);
    expect(diff.deletionLines).toContain("- Old\n");
    expect(diff.additionLines).toContain("- New\n");
  });
});
