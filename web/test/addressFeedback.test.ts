import { describe, expect, test } from "bun:test";
import { eligibleRevisions, suggestRevision } from "../src/components/dialogs/AddressFeedbackDialog";
import type { Revision, Thread } from "../src/types";

describe("addressed feedback revision choice", () => {
  const thread = {
    id: "thread-1",
    anchor: { startLine: 2, endLine: 2 },
    intent: "instruction",
    lifecycle: "detached",
    bucket: "attention",
    delivery: "none",
    position: { x: 0, y: 0 },
    messages: [{ id: "msg-1", author: "reviewer", body: "change it", createdAt: "2026-07-10T12:05:00Z" }],
    capabilities: {
      canEdit: true, canReply: false, canChangeIntent: false, canAsk: false, canIterate: false,
      canReanchor: true, canMarkAddressed: true, canDelete: true, canCreateFollowUp: false,
    },
  } satisfies Thread;
  const revisions = [
    revision("rev-1", "2026-07-10T11:00:00Z", "initial"),
    revision("rev-2", "2026-07-10T12:03:00Z", "immediate", "rev-1"),
    revision("rev-3", "2026-07-10T12:39:00Z", "external", "rev-2"),
    revision("rev-4", "2026-07-11T10:00:00Z", "iteration", "rev-3"),
  ];

  test("excludes the root and revisions older than the feedback", () => {
    expect(eligibleRevisions(thread, revisions).map((item) => item.id)).toEqual(["rev-3", "rev-4"]);
  });

  test("suggests the first external revision after the feedback", () => {
    expect(suggestRevision(thread, eligibleRevisions(thread, revisions))?.id).toBe("rev-3");
  });
});

function revision(id: string, createdAt: string, source: Revision["source"], parentId?: string): Revision {
  return { id, createdAt, source, parentId, plan: "" };
}
