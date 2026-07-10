import { describe, expect, test } from "bun:test";
import { lineDiff } from "../src/lib/diff";

describe("lineDiff", () => {
  test("keeps shared lines as context inside replacements", () => {
    expect(lineDiff("alpha\nbeta\ndelta", "alpha\ngamma\ndelta\nepsilon")).toEqual([
      { kind: "context", before: 1, after: 1, text: "alpha" },
      { kind: "remove", before: 2, text: "beta" },
      { kind: "add", after: 2, text: "gamma" },
      { kind: "context", before: 3, after: 3, text: "delta" },
      { kind: "add", after: 4, text: "epsilon" },
    ]);
  });

  test("preserves trailing blank lines", () => {
    expect(lineDiff("alpha\n", "alpha\nbeta\n")).toEqual([
      { kind: "context", before: 1, after: 1, text: "alpha" },
      { kind: "add", after: 2, text: "beta" },
      { kind: "context", before: 2, after: 3, text: "" },
    ]);
  });

  test("shows a change in the middle of the complete plan at its actual line", () => {
    expect(lineDiff("# Plan\n- First\n- Middle\n- Last", "# Plan\n- First\n- Updated middle\n- Last")).toEqual([
      { kind: "context", before: 1, after: 1, text: "# Plan" },
      { kind: "context", before: 2, after: 2, text: "- First" },
      { kind: "remove", before: 3, text: "- Middle" },
      { kind: "add", after: 3, text: "- Updated middle" },
      { kind: "context", before: 4, after: 4, text: "- Last" },
    ]);
  });

  test("does not hide a proposed change outside the originally selected lines", () => {
    const diff = lineDiff("# Plan\n- Selected\n- Unselected", "# Plan\n- Selected\n- Changed elsewhere");

    expect(diff).toContainEqual({ kind: "remove", before: 3, text: "- Unselected" });
    expect(diff).toContainEqual({ kind: "add", after: 3, text: "- Changed elsewhere" });
  });
});
