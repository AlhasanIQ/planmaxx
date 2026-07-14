import type { ChangeRow } from "../types";

export interface ComparisonLineIdentity {
  beforeLineNumber?: number;
  afterLineNumber?: number;
  displayLineNumber: number;
  // Comments always belong to the current (after) revision. Removed rows have
  // no current source location and are deliberately not commentable.
  anchorLineNumber?: number;
}

export function comparisonLineIdentity(line: ChangeRow): ComparisonLineIdentity {
  return {
    beforeLineNumber: line.before,
    afterLineNumber: line.after,
    displayLineNumber: line.after ?? line.before ?? 0,
    anchorLineNumber: line.after,
  };
}

export function comparisonGutterValues(before?: number, after?: number): {
  before: number | "+";
  after: number | "−";
} {
  return {
    before: before ?? "+",
    after: after ?? "−",
  };
}
