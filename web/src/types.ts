export interface Anchor {
  startLine: number;
  startChar?: number;
  endLine: number;
  endChar?: number;
}

export interface Position {
  x: number;
  y: number;
}

export interface Message {
  id: string;
  author: string;
  body: string;
  createdAt: string;
}

export interface Thread {
  id: string;
  anchor: Anchor;
  selectedText?: string;
  kind: ThreadKind;
  status: ThreadStatus;
  position: Position;
  messages: Message[];
}

export interface SideAnswer {
  id: string;
  threadId: string;
  question: string;
  answer: string;
  promoted: boolean;
  createdAt: string;
}

export interface Digest {
  summary: string;
  reviewerDecisions: string[];
  promotedSideAnswers: string[];
}

export interface Revision {
  id: string;
  commitId?: string;
  parentId?: string;
  source: "initial" | "turn" | "immediate" | "external";
  createdAt: string;
  plan: string;
  anchor?: Anchor;
  summary?: string;
}

export interface SectionProposal {
  id: string;
  parentId: string;
  threadId?: string;
  anchor: Anchor;
  appliedAnchor?: Anchor;
  replacementAnchor?: Anchor;
  originalSection: string;
  proposedSection: string;
  proposedPlan: string;
  summary: string;
  instruction: string;
  rawResponse: string;
  includedThreadIds?: string[];
  obsolete?: boolean;
  createdAt: string;
}

export interface DiffLine {
  kind: "context" | "remove" | "add";
  before?: number;
  after?: number;
  text: string;
}

export interface Session {
  id: string;
  plan: string;
  planPath: string;
  currentRevisionId: string;
  revisions: Revision[];
  pendingProposal?: SectionProposal | null;
  threads: Thread[];
  sideAnswers: SideAnswer[];
  digest: Digest;
}

// A reviewer's comment can be a "decision" (instructions/feedback that should
// reach Codex in the next turn) or a "note" (private to the reviewer, kept out
// of the handoff).
export type ThreadKind = "decision" | "note";
export type ThreadStatus = "open" | "resolved" | "stale";
