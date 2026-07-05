import { describe, expect, test } from "bun:test";
import { promotedSideAnswerText, sideQuestionContext } from "../src/lib/selectionContext";
import type { Session, Thread } from "../src/types";

describe("sideQuestionContext", () => {
  test("uses exact selected text with a line:character file reference", () => {
    const thread = threadFixture({
      selectedText: "specific",
      anchor: { startLine: 2, startChar: 9, endLine: 2, endChar: 17 },
    });
    const context = sideQuestionContext(sessionFixture(thread), thread);

    expect(context.filePath).toBe("/repo/plan.md");
    expect(context.reference).toBe("/repo/plan.md:2:10-2:18");
    expect(context.selectedText).toBe("specific");
    expect(context.planExcerpt).toBe("Select a specific word here.");
  });

  test("promoted handoff text includes the /btw question and answer", () => {
    const thread = threadFixture({
      selectedText: "specific",
      anchor: { startLine: 2, startChar: 9, endLine: 2, endChar: 17 },
    });
    const session = sessionFixture(thread);

    expect(promotedSideAnswerText(session, "side-1")).toBe(
      [
        "/btw context: /repo/plan.md:2:10-2:18",
        "Selected text:\nspecific",
        "Question:\nWhy this word?",
        "Answer:\nBecause it is the smallest actionable scope.",
      ].join("\n"),
    );
  });
});

function sessionFixture(thread: Thread): Session {
  return {
    id: "session-1",
    plan: "# Plan\nSelect a specific word here.",
    planPath: "/repo/plan.md",
    threads: [thread],
    sideAnswers: [
      {
        id: "side-1",
        threadId: thread.id,
        question: "Why this word?",
        answer: "Because it is the smallest actionable scope.",
        promoted: true,
        createdAt: new Date(0).toISOString(),
      },
    ],
    digest: {
      summary: "",
      reviewerDecisions: [],
      promotedSideAnswers: [],
    },
  };
}

function threadFixture(overrides: Partial<Thread>): Thread {
  return {
    id: "thread-1",
    anchor: { startLine: 2, endLine: 2 },
    selectedText: "",
    position: { x: 0, y: 0 },
    messages: [],
    ...overrides,
  };
}
