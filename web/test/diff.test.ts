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
});
