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
  intent: ThreadIntent;
  lifecycle: ThreadLifecycle;
  bucket: ThreadBucket;
  delivery: "iteration" | "private" | "none";
  addressedRevisionId?: string;
  position: Position;
  messages: Message[];
  capabilities: ThreadCapabilities;
}

export interface ThreadCapabilities {
  canEdit: boolean;
  canReply: boolean;
  canChangeIntent: boolean;
  canAsk: boolean;
  canIterate: boolean;
  canReanchor: boolean;
  canMarkAddressed: boolean;
  canDelete: boolean;
  canCreateFollowUp: boolean;
}

export interface SideAnswer {
  id: string;
  threadId: string;
  question: string;
  answer: string;
  included: boolean;
  delivery: "iteration" | "private" | "none";
  createdAt: string;
  capabilities: {
    canInclude: boolean;
    canKeepPrivate: boolean;
  };
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
  reviewStops: ReviewStop[];
}

export interface ReviewStop {
  id: string;
  kind: "comment" | "feedback" | "change";
  rowId: string;
  rowIndex: number;
  threadId?: string;
  revisionId?: string;
  clusterId?: string;
  beforeStart?: number;
  beforeEnd?: number;
  afterStart?: number;
  afterEnd?: number;
}

export type RevisionComparison = ChangeView;

export interface Session {
  schemaVersion: 4;
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
  counts: {
    activeInstructions: number;
    activePrivateNotes: number;
    includedAnswers: number;
    privateAnswers: number;
    detachedFeedback: number;
    addressedHistory: number;
  };
  phase: "active" | "proposal_pending" | "terminal";
  capabilities: {
    canFinalize: boolean;
    canIterate: boolean;
    canEditFeedback: boolean;
    canRestoreRevision: boolean;
    canApplyProposal: boolean;
  };
  activeChange?: ChangeView | null;
}

export type PlanFormat = "markdown" | "html";

// Persisted revision-feedback snapshots retain the legacy kind vocabulary.
export type ThreadKind = "decision" | "note";
export type ThreadIntent = "instruction" | "private";
export type ThreadLifecycle = "active" | "addressed" | "detached";
export type ThreadBucket = "active" | "attention" | "history";
