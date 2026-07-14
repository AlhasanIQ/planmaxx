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
  source: "initial" | "turn" | "immediate" | "iteration" | "external";
  createdAt: string;
  plan: string;
  anchor?: Anchor;
  summary?: string;
  feedback?: RevisionFeedback[];
}

export interface RevisionFeedback {
  revisionId: string;
  threadId: string;
  anchor: Anchor;
  resultAnchor: Anchor;
  selectedText?: string;
  kind: ThreadKind;
  messages: Message[];
}

export interface SectionProposal {
  id: string;
  kind?: "review";
  parentId: string;
  threadId?: string;
  anchor: Anchor;
  appliedAnchor?: Anchor;
  appliedHunks?: AppliedHunk[];
  replacementAnchor?: Anchor;
  originalSection?: string;
  proposedSection?: string;
  proposedPlan?: string;
  summary: string;
  instruction: string;
  rawResponse: string;
  includedThreadIds?: string[];
  consumedSideAnswerIds?: string[];
  reviewDigest?: Digest;
  obsolete?: boolean;
  createdAt: string;
}

// The state endpoint intentionally omits model output and duplicate plan
// bodies. Mutations may still return the complete proposal internally.
export interface PendingProposalSummary {
  id: string;
  kind?: "review";
  parentId: string;
  threadId?: string;
  anchor: Anchor;
  replacementAnchor: Anchor;
  summary: string;
  instruction: string;
  reviewDigest?: Digest;
  obsolete?: boolean;
  createdAt: string;
}

export interface AppliedHunk {
  anchor: Anchor;
  result: Anchor;
  lineDelta: number;
}

export interface ChangeRow {
  id: string;
  index: number;
  kind: "context" | "remove" | "add";
  before?: number;
  after?: number;
  text: string;
  clusterId?: string;
}

export interface DocumentSnapshot {
  format: PlanFormat;
  text: string;
  lines: string[];
  terminalNewline: boolean;
}

export interface ChangeCluster {
  id: string;
  firstRow: number;
  lastRow: number;
  beforeStart?: number;
  beforeEnd?: number;
  afterStart?: number;
  afterEnd?: number;
}

export interface ThreadPlacement {
  threadId: string;
  rowId: string;
  rowIndex: number;
}

export interface FeedbackPlacement {
  revisionId: string;
  threadId: string;
  rowId: string;
  rowIndex: number;
}

export interface ChangeView {
  mode: "proposal" | "revision";
  isDirect: boolean;
  baseId: string;
  targetId: string;
  before: DocumentSnapshot;
  after: DocumentSnapshot;
  rows: ChangeRow[];
  clusters: ChangeCluster[];
  threadPlacements: ThreadPlacement[];
  feedback: RevisionFeedback[];
  feedbackPlacements: FeedbackPlacement[];
}

export type RevisionComparison = ChangeView;

export interface Session {
  schemaVersion: 2;
  id: string;
  plan: string;
  planPath: string;
  planFormat: PlanFormat;
  currentRevisionId: string;
  revisions: Revision[];
  pendingProposal?: PendingProposalSummary | null;
  threads: Thread[];
  sideAnswers: SideAnswer[];
  digest: Digest;
  phase: "active" | "proposal_pending" | "terminal";
  capabilities: {
    canFinalize: boolean;
    canEditFeedback: boolean;
    canRestoreRevision: boolean;
    canApplyProposal: boolean;
  };
  activeChange?: ChangeView | null;
}

export type PlanFormat = "markdown" | "html";

// A reviewer's comment can be a "decision" (instructions/feedback that should
// reach Codex in the next turn) or a "note" (private to the reviewer, kept out
// of the handoff).
export type ThreadKind = "decision" | "note";
export type ThreadStatus = "open" | "resolved" | "stale";
