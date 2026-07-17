import { describe, expect, test } from "bun:test";
import { nextReviewIndex, reviewNavigationIdentity, reviewScrollBehavior, reviewStopLabel, reviewStopSelector, reviewStopSummary } from "../src/lib/reviewNavigation";
import type { ReviewStop } from "../src/types";

const stop = (value: Partial<ReviewStop> = {}): ReviewStop => ({
  id: "change:cluster-1", kind: "change", rowId: "row-1", rowIndex: 0, clusterId: "cluster-1", ...value,
});

describe("review navigation presentation", () => {
  test("labels replacement, add-only, and remove-only ranges", () => {
    expect(reviewStopLabel(stop({ beforeStart: 2, beforeEnd: 3, afterStart: 2, afterEnd: 4 }))).toBe("Change · lines 2–3 → lines 2–4");
    expect(reviewStopLabel(stop({ kind: "comment", beforeStart: 5, beforeEnd: 8, afterStart: 20, afterEnd: 25 }))).toBe("Comment · lines 5–8");
    expect(reviewStopLabel(stop({ kind: "feedback", beforeStart: 5, beforeEnd: 5, afterStart: 20, afterEnd: 22 }))).toBe("Accepted feedback · line 5 → lines 20–22");
    expect(reviewStopLabel(stop({ beforeStart: undefined, afterStart: 7, afterEnd: 7 }))).toBe("Change · line 7");
    expect(reviewStopLabel(stop({ beforeStart: 9, beforeEnd: 10, afterStart: undefined }))).toBe("Change · lines 9–10");
  });

  test("summarizes feedback separately from uncovered changes", () => {
    expect(reviewStopSummary([
      stop({ id: "comment:one", kind: "comment", threadId: "one" }),
      stop({ id: "feedback:r:t", kind: "feedback", revisionId: "r", threadId: "t" }),
      stop(),
    ])).toBe("2 comments · 1 change");
  });

  test("selects exact comment, feedback, and change targets", () => {
    expect(reviewStopSelector(stop({ kind: "comment", threadId: "thread-1" }))).toBe('[data-thread-id="thread-1"]');
    expect(reviewStopSelector(stop({ kind: "feedback", revisionId: "rev-2", threadId: "thread-1" }))).toBe('[data-feedback-id="rev-2:thread-1"]');
    expect(reviewStopSelector(stop())).toBe('[data-change-cluster="cluster-1"]');
  });

  test("keeps navigation inside its boundaries", () => {
    expect(nextReviewIndex(-1, 1, 3)).toBe(0);
    expect(nextReviewIndex(0, -1, 3)).toBe(0);
    expect(nextReviewIndex(0, 1, 3)).toBe(1);
    expect(nextReviewIndex(2, 1, 3)).toBe(2);
    expect(nextReviewIndex(-1, 1, 0)).toBe(-1);
  });

  test("changes identity when comparison or stop order changes", () => {
    const stops = [stop({ id: "one" }), stop({ id: "two" })];
    expect(reviewNavigationIdentity("rev-1:rev-2", stops)).not.toBe(reviewNavigationIdentity("rev-1:rev-3", stops));
    expect(reviewNavigationIdentity("rev-1:rev-2", stops)).not.toBe(reviewNavigationIdentity("rev-1:rev-2", [...stops].reverse()));
  });

  test("honors reduced-motion scrolling", () => {
    expect(reviewScrollBehavior(true)).toBe("auto");
    expect(reviewScrollBehavior(false)).toBe("smooth");
  });
});
