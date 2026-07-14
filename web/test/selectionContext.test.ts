import { describe, expect, test } from "bun:test";
import { sideQuestionContext } from "../src/lib/selectionContext";
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

  test("prefers the current anchored characters over a trimmed stored quote", () => {
    const thread = threadFixture({
      selectedText: "specific",
      anchor: { startLine: 2, startChar: 8, endLine: 2, endChar: 18 },
    });
    const context = sideQuestionContext(sessionFixture(thread), thread);

    expect(context.selectedText).toBe(" specific ");
  });
});

function sessionFixture(thread: Thread): Session {
  return {
    schemaVersion: 3,
    id: "session-1",
    plan: "# Plan\nSelect a specific word here.",
    planPath: "/repo/plan.md",
    threads: [thread],
    planFormat: "markdown",
    currentRevisionId: "rev-1",
    revisions: [],
    pendingProposal: null,
    sideAnswers: [],
    digest: {
      summary: "",
      reviewerDecisions: [],
      promotedSideAnswers: [],
    },
    counts: {
      activeInstructions: 1,
      activePrivateNotes: 0,
      includedAnswers: 0,
      privateAnswers: 0,
      detachedFeedback: 0,
      addressedHistory: 0,
    },
    phase: "active",
    capabilities: {
      canFinalize: true,
      canIterate: true,
      canEditFeedback: true,
      canRestoreRevision: true,
      canApplyProposal: false,
    },
  };
}

function threadFixture(overrides: Partial<Thread>): Thread {
  return {
    id: "thread-1",
    anchor: { startLine: 2, endLine: 2 },
    selectedText: "",
    intent: "instruction",
    lifecycle: "active",
    bucket: "active",
    delivery: "iteration",
    position: { x: 0, y: 0 },
    messages: [],
    capabilities: {
      canEdit: true,
      canReply: true,
      canChangeIntent: true,
      canAsk: true,
      canIterate: true,
      canReanchor: false,
      canDelete: true,
      canCreateFollowUp: false,
    },
    ...overrides,
  };
}
