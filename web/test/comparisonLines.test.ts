import { describe, expect, test } from "bun:test";
import { comparisonGutterValues, comparisonLineIdentity } from "../src/lib/comparisonLines";

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

  test("uses a minus for removed rows and a plus for added rows", () => {
    expect(comparisonGutterValues(149, undefined)).toEqual({ before: 149, after: "−" });
    expect(comparisonGutterValues(undefined, 147)).toEqual({ before: "+", after: 147 });
  });
});
