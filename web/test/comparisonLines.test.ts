import { describe, expect, test } from "bun:test";
import { comparisonLineIdentity } from "../src/lib/comparisonLines";

describe("comparisonLineIdentity", () => {
  test("keeps before and current line numbers distinct when a deletion shifts later lines", () => {
    expect(comparisonLineIdentity({ kind: "context", before: 59, after: 58, text: "unchanged" })).toEqual({
      beforeLineNumber: 59,
      afterLineNumber: 58,
      displayLineNumber: 58,
      anchorLineNumber: 58,
    });
  });

  test("anchors a newly added comparison row to its current line", () => {
    expect(comparisonLineIdentity({ kind: "add", after: 59, text: "new" })).toEqual({
      afterLineNumber: 59,
      displayLineNumber: 59,
      anchorLineNumber: 59,
    });
  });

  test("does not expose a comment anchor for a removed row", () => {
    expect(comparisonLineIdentity({ kind: "remove", before: 60, text: "old" })).toEqual({
      beforeLineNumber: 60,
      displayLineNumber: 60,
    });
  });
});
