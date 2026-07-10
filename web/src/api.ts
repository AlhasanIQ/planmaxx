import type { SideQuestionContext } from "./lib/selectionContext";
import type { Anchor, DiffLine, Digest, Revision, RevisionFeedback, SectionProposal, Session, SideAnswer, Thread, ThreadKind } from "./types";

export class ApiError extends Error {
  constructor(message: string, public status: number) {
    super(message);
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
    ...init,
  });
  let data: unknown = {};
  try {
    data = await res.json();
  } catch {
    // empty body
  }
  if (!res.ok) {
    const message =
      typeof data === "object" && data && "error" in data
        ? String((data as { error: unknown }).error)
        : `Request failed with ${res.status}`;
    throw new ApiError(message, res.status);
  }
  return data as T;
}

// Go nil slices marshal as JSON null. Normalize so React code can treat
// these fields as arrays without per-callsite guards.
function normalizeSession(raw: Session): Session {
  return {
    ...raw,
    planPath: raw.planPath ?? "",
    currentRevisionId: raw.currentRevisionId ?? "",
    revisions: raw.revisions ?? [],
    pendingProposal: raw.pendingProposal ?? null,
    threads: (raw.threads ?? []).map((t) => ({
      ...t,
      kind: t.kind ?? "decision",
      status: t.status ?? "open",
      messages: t.messages ?? [],
    })),
    sideAnswers: raw.sideAnswers ?? [],
    digest: {
      summary: raw.digest?.summary ?? "",
      reviewerDecisions: raw.digest?.reviewerDecisions ?? [],
      promotedSideAnswers: raw.digest?.promotedSideAnswers ?? [],
    },
  };
}

function normalizeDigest(raw: Digest): Digest {
  return {
    summary: raw.summary ?? "",
    reviewerDecisions: raw.reviewerDecisions ?? [],
    promotedSideAnswers: raw.promotedSideAnswers ?? [],
  };
}

export const api = {
  getState: async () => normalizeSession(await request<Session>("/api/state")),
  createThread: async (anchor: Anchor, body: string, selectedText = "") => {
    const t = await request<Thread>("/api/threads", {
      method: "POST",
      body: JSON.stringify({ anchor, body, selectedText }),
    });
    return { ...t, kind: t.kind ?? "decision", status: t.status ?? "open", messages: t.messages ?? [] };
  },
  setThreadKind: (threadId: string, kind: ThreadKind) =>
    request<{ status: string }>(`/api/threads/${encodeURIComponent(threadId)}/kind`, {
      method: "POST",
      body: JSON.stringify({ kind }),
    }),
  reply: (threadId: string, body: string) =>
    request<{ status: string }>(`/api/threads/${encodeURIComponent(threadId)}/reply`, {
      method: "POST",
      body: JSON.stringify({ body }),
    }),
  deleteThread: (threadId: string) =>
    request<{ status: string }>(`/api/threads/${encodeURIComponent(threadId)}/delete`, {
      method: "POST",
      body: "{}",
    }),
  moveThread: (threadId: string, x: number, y: number) =>
    request<{ status: string }>(`/api/threads/${encodeURIComponent(threadId)}/move`, {
      method: "POST",
      body: JSON.stringify({ x, y }),
    }),
  editThread: (threadId: string, anchor: Anchor, body: string, selectedText = "") =>
    request<{ status: string }>(`/api/threads/${encodeURIComponent(threadId)}/edit`, {
      method: "POST",
      body: JSON.stringify({ anchor, body, selectedText }),
    }),
  sideQuestion: (threadId: string, question: string, context: SideQuestionContext) =>
    request<SideAnswer>("/api/side-questions", {
      method: "POST",
      body: JSON.stringify({ threadID: threadId, question, ...context }),
    }),
  promote: (id: string) =>
    request<{ status: string }>(`/api/side-answers/${encodeURIComponent(id)}/promote`, {
      method: "POST",
      body: "{}",
    }),
  unpromote: (id: string) =>
    request<{ status: string }>(`/api/side-answers/${encodeURIComponent(id)}/unpromote`, {
      method: "POST",
      body: "{}",
    }),
  digestDraft: async () =>
    normalizeDigest(await request<Digest>("/api/digest/draft", { method: "POST" })),
  revisions: () =>
    request<{ currentRevisionId: string; revisions: Revision[]; pendingProposal?: SectionProposal | null }>("/api/revisions"),
  revisionDiff: (from: string, to: string) =>
    request<{ from: string; to: string; lines: DiffLine[]; feedback?: RevisionFeedback[] }>(
      `/api/revisions/${encodeURIComponent(from)}/diff/${encodeURIComponent(to)}`,
    ),
  restoreRevision: (revisionId: string) =>
    request<Revision>(`/api/revisions/${encodeURIComponent(revisionId)}/restore`, {
      method: "POST",
      body: "{}",
    }),
  proposeSection: (threadId: string | undefined, anchor: Anchor, instruction: string) =>
    request<SectionProposal>("/api/revisions/propose-section", {
      method: "POST",
      body: JSON.stringify({ threadId, anchor, instruction }),
    }),
  applyProposal: (proposalId: string) =>
    request<Revision>(`/api/revisions/proposals/${encodeURIComponent(proposalId)}/apply`, {
      method: "POST",
      body: "{}",
    }),
  discardProposal: (proposalId: string) =>
    request<{ status: string }>(`/api/revisions/proposals/${encodeURIComponent(proposalId)}/discard`, {
      method: "POST",
      body: "{}",
    }),
  finalize: (digest: Digest) =>
    request<{ status: string }>("/api/finalize", {
      method: "POST",
      body: JSON.stringify(digest),
    }),
  cancel: () =>
    request<{ status: string }>("/api/cancel", { method: "POST", body: "{}" }),
};
