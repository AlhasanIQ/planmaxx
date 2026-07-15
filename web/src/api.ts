import type { SideQuestionContext } from "./lib/selectionContext";
import type { Anchor, ChangeView, Digest, Revision, SectionProposal, Session, SideAnswer, Thread, ThreadIntent } from "./types";

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

function normalizeSession(raw: Session): Session {
  if (raw.schemaVersion !== 4) {
    throw new Error(`Unsupported PlanMaxx API schema ${String(raw.schemaVersion ?? "missing")}; reload the rebuilt app.`);
  }
  return raw;
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
  createThread: async (anchor: Anchor, body: string, selectedText = "", intent: ThreadIntent = "instruction") => {
    return request<Thread>("/api/threads", {
      method: "POST",
      body: JSON.stringify({ anchor, body, selectedText, intent }),
    });
  },
  setThreadIntent: (threadId: string, intent: ThreadIntent) =>
    request<{ status: string }>(`/api/threads/${encodeURIComponent(threadId)}/intent`, {
      method: "POST",
      body: JSON.stringify({ intent }),
    }),
  createFollowUp: (threadId: string) =>
    request<Thread>(`/api/threads/${encodeURIComponent(threadId)}/follow-up`, { method: "POST", body: "{}" }),
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
  markThreadAddressed: (threadId: string, revisionId: string) =>
    request<{ status: string }>(`/api/threads/${encodeURIComponent(threadId)}/mark-addressed`, {
      method: "POST",
      body: JSON.stringify({ revisionId }),
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
  revisionDiff: (from: string, to: string) =>
    request<ChangeView>(
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
  proposeReview: (digest: Digest) =>
    request<SectionProposal>("/api/revisions/propose-review", {
      method: "POST",
      body: JSON.stringify(digest),
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
