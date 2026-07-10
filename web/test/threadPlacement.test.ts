import { describe, expect, test } from "bun:test";
import { threadsByAnchorEnd, visibleThreads } from "../src/lib/threadPlacement";
import type { SideAnswer, Thread } from "../src/types";

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
});
