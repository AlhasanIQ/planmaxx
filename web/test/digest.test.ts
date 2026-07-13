import { describe, expect, test } from "bun:test";
import { buildDigestDraft, countHandoff } from "../src/lib/digest";
import type { Session, Thread } from "../src/types";

describe("digest helpers", () => {
  test("include only open decision threads in the handoff draft", () => {
    const session = sessionFixture([
      threadFixture("thread-1", "decision", "open", "Ship this."),
      threadFixture("thread-2", "note", "open", "Keep private."),
      threadFixture("thread-3", "decision", "resolved", "Already handled."),
      threadFixture("thread-4", "decision", "stale", "Needs re-anchor."),
    ]);

    expect(buildDigestDraft(session)).toEqual({
      summary: "Approved with review comments.",
      reviewerDecisions: ["Ship this."],
      promotedSideAnswers: [],
    });
  });

  test("counts open decisions, notes, and non-open decisions separately", () => {
    const session = sessionFixture([
      threadFixture("thread-1", "decision", "open", "Ship this."),
      threadFixture("thread-2", "note", "open", "Keep private."),
      threadFixture("thread-3", "decision", "resolved", "Already handled."),
    ]);

    expect(countHandoff(session)).toEqual({
      decisions: 1,
      notes: 2,
      promoted: 0,
      ephemeral: 0,
    });
  });

  test("keeps promoted answers from historical threads out of the next handoff", () => {
    const session = sessionFixture([
      threadFixture("open", "decision", "open", "Current feedback."),
      threadFixture("resolved", "decision", "resolved", "Handled feedback."),
      threadFixture("stale", "decision", "stale", "Changed feedback."),
    ]);
    session.sideAnswers = [
      sideAnswer("open-answer", "open", "Open answer"),
      sideAnswer("resolved-answer", "resolved", "Resolved answer"),
      sideAnswer("stale-answer", "stale", "Stale answer"),
    ];

    expect(buildDigestDraft(session).promotedSideAnswers).toHaveLength(1);
    expect(buildDigestDraft(session).promotedSideAnswers[0]).toContain("Open answer");
    expect(countHandoff(session)).toMatchObject({ promoted: 1, ephemeral: 2 });
  });
});

function sessionFixture(threads: Thread[]): Session {
  return {
    id: "session-1",
    plan: "# Plan",
    planPath: "/repo/plan.md",
    currentRevisionId: "rev-1",
    revisions: [],
    threads,
    sideAnswers: [],
    digest: {
      summary: "",
      reviewerDecisions: [],
      promotedSideAnswers: [],
    },
  };
}

function sideAnswer(id: string, threadId: string, answer: string) {
  return {
    id,
    threadId,
    question: "Question?",
    answer,
    promoted: true,
    createdAt: new Date(0).toISOString(),
  };
}

function threadFixture(
  id: string,
  kind: Thread["kind"],
  status: Thread["status"],
  body: string,
): Thread {
  return {
    id,
    kind,
    status,
    anchor: { startLine: 1, endLine: 1 },
    selectedText: "",
    position: { x: 0, y: 0 },
    messages: [
      {
        id: `${id}-msg`,
        author: "reviewer",
        body,
        createdAt: new Date(0).toISOString(),
      },
    ],
  };
}
