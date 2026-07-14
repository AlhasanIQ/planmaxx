import { describe, expect, test } from "bun:test";
import { threadsByAnchorEnd, threadsByBackendPlacement, visibleThreads } from "../src/lib/threadPlacement";
import type { SideAnswer, Thread, ThreadPlacement } from "../src/types";

function thread(id: string, startLine: number, endLine: number, body = id): Thread {
  return {
    id,
    anchor: { startLine, endLine },
    kind: "decision",
    status: "open",
    position: { x: 0, y: 0 },
    messages: [{ id: `${id}-message`, author: "reviewer", body, createdAt: "2026-01-01T00:00:00Z" }],
  };
}

describe("thread placement", () => {
  test("stacks same-line comments and places overlapping ranges after their final line", () => {
    const grouped = threadsByAnchorEnd([
      thread("first", 3, 3),
      thread("range", 2, 5),
      thread("second", 3, 3),
      thread("overlap", 4, 5),
    ]);

    expect(grouped.get(3)?.map((item) => item.id)).toEqual(["first", "second"]);
    expect(grouped.get(5)?.map((item) => item.id)).toEqual(["range", "overlap"]);
  });

  test("filters by /btw content but retains the focused thread", () => {
    const threads = [thread("focused", 1, 1, "Unrelated"), thread("answer", 2, 2, "Other")];
    const answers: SideAnswer[] = [{
      id: "side-1",
      threadId: "answer",
      question: "What does this mean?",
      answer: "Relevant /btw answer",
      promoted: false,
      createdAt: "2026-01-01T00:00:00Z",
    }];

    expect(visibleThreads(threads, answers, "relevant", "focused").map((item) => item.id)).toEqual(["focused", "answer"]);
  });

  test("uses the backend-selected row after a complete change cluster", () => {
    const placements: ThreadPlacement[] = [{ threadId: "remove-both", rowId: "row-4", rowIndex: 3 }];
    const grouped = threadsByBackendPlacement([thread("remove-both", 21, 22)], placements);
    expect(grouped.get(3)?.map((item) => item.id)).toEqual(["remove-both"]);
    expect(grouped.has(1)).toBe(false);
  });

  test("co-locates overlapping comments at one backend placement", () => {
    const placements: ThreadPlacement[] = [
      { threadId: "whole-range", rowId: "row-8", rowIndex: 7 },
      { threadId: "middle", rowId: "row-8", rowIndex: 7 },
      { threadId: "last", rowId: "row-8", rowIndex: 7 },
    ];
    const grouped = threadsByBackendPlacement([
      thread("whole-range", 55, 58),
      thread("middle", 56, 57),
      thread("last", 58, 58),
    ], placements);
    expect(grouped.get(7)?.map((item) => item.id)).toEqual(["whole-range", "middle", "last"]);
    expect(grouped.size).toBe(1);
  });
});
