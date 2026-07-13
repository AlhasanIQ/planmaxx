import type { DiffLine } from "../types";

export interface ComparisonLineIdentity {
  beforeLineNumber?: number;
  afterLineNumber?: number;
  displayLineNumber: number;
  // Comments always belong to the current (after) revision. Removed rows have
  // no current source location and are deliberately not commentable.
  anchorLineNumber?: number;
}

export function comparisonLineIdentity(line: DiffLine): ComparisonLineIdentity {
  return {
    beforeLineNumber: line.before,
    afterLineNumber: line.after,
    displayLineNumber: line.after ?? line.before ?? 0,
    anchorLineNumber: line.after,
  };
}
