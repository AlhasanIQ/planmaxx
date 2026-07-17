import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, Code2, Columns2, Eye, GitCompareArrows, ListTree, Loader2, MessageSquarePlus, Search, Sparkles } from "lucide-react";
import { renderPlanLines, renderSourceLines } from "../lib/markdown";
import { htmlPreviewDocument } from "../lib/htmlPreview";
import type { Anchor, ChangeView, DocumentSnapshot, PendingProposalSummary, PlanFormat, ReviewStop, RevisionComparison, RevisionFeedback, SideAnswer, Thread, ThreadIntent } from "../types";
import { anchorLabel, anchorTouchesLine } from "../lib/anchors";
import { inlineCommentComposerPlacement } from "../lib/commentPlacement";
import { comparisonGutterValues, comparisonLineIdentity } from "../lib/comparisonLines";
import { highlightCodeBlocks, type HighlightToken } from "../lib/codeHighlight";
import { documentOutline, type OutlineItem } from "../lib/documentOutline";
import { reviewScrollBehavior } from "../lib/reviewNavigation";
import { groupSideAnswersByThread, threadsByAnchorEnd, threadsByBackendPlacement, visibleThreads } from "../lib/threadPlacement";
import { CommentThreadStack, useCommentRailMetrics, type CommentView } from "./CommentLayer";
import { ProposalActions } from "./ProposalActions";
import { RevisionFeedbackList, RevisionFeedbackSummary } from "./RevisionFeedback";
import { RenderedLine } from "./RenderedLine";
import { ReviewNavigator } from "./ReviewNavigator";
import { DocumentOutline } from "./DocumentOutline";
import { DraftBoundaryHandles, draftFromSelection, restoreNativeSelection, selectedTextForAnchorInArticle, usePlanHighlights } from "./SelectionLayer";

export type { CommentView } from "./CommentLayer";

interface PlanProps {
  plan: string;
  planFormat: PlanFormat;
  theme: "light" | "dark";
  proposal?: PendingProposalSummary | null;
	proposalChange?: ChangeView | null;
  comparison?: RevisionComparison | null;
  comparisonLoading: boolean;
  onClearComparison: () => void;
  threads: Thread[];
  sideAnswers: SideAnswer[];
  hoveredThreadId: string | null;
  focusedThreadId: string | null;
  editingThread: Thread | null;
  commentView: CommentView;
  commentFilter: string;
  onCommentViewChange: (view: CommentView) => void;
  onCommentFilterChange: (filter: string) => void;
  onCreateComment: (anchor: Anchor, body: string, selectedText: string, intent: ThreadIntent) => Promise<boolean>;
  onUpdateComment: (threadId: string, anchor: Anchor, body: string, selectedText: string) => Promise<boolean>;
  onAskSideFromDraft: (anchor: Anchor, body: string, selectedText: string) => Promise<boolean>;
  onIterateDraft: (anchor: Anchor, instruction: string, selectedText: string) => Promise<boolean>;
  disabled: boolean;
  proposalDisabled: boolean;
  proposalIterating: boolean;
  onApplyProposal: (proposalId: string) => void;
  onDiscardProposal: (proposalId: string) => void;
  onIterateProposal: (anchor: Anchor, instruction: string) => Promise<boolean>;
  onEditDone: () => void;
  onFocusThread: (threadId: string) => void;
  onHoverThread: (threadId: string | null) => void;
  onSetThreadIntent: (threadId: string, intent: ThreadIntent) => void | Promise<void>;
  onReplyThread: (threadId: string) => void;
  onDeleteThread: (threadId: string) => void;
  onEditThread: (threadId: string) => void;
  onMarkAddressed: (threadId: string) => void;
  onCreateFollowUp: (threadId: string) => void;
  onAskSide: (thread: Thread) => void;
  onIterateThread: (thread: Thread) => void | Promise<void>;
  onIncludeAnswer: (answerId: string) => void;
  onKeepAnswerPrivate: (answerId: string) => void;
  threadAgentActions: Record<string, "asking" | "iterating">;
  sideQuestionsEnabled: boolean;
}

interface CommentDraft {
  threadId?: string;
  anchor: Anchor;
  selectedText: string;
  body: string;
  intent: ThreadIntent;
}

export const ReviewDocument = memo(function ReviewDocument({
  plan,
  planFormat,
  theme,
  proposal,
	proposalChange,
  comparison,
  comparisonLoading,
  onClearComparison,
  threads,
  sideAnswers,
  hoveredThreadId,
  focusedThreadId,
  editingThread,
  commentView,
  commentFilter,
  onCommentViewChange,
  onCommentFilterChange,
  onCreateComment,
  onUpdateComment,
  onAskSideFromDraft,
  onIterateDraft,
  disabled,
  proposalDisabled,
  proposalIterating,
  onApplyProposal,
  onDiscardProposal,
  onIterateProposal,
  onEditDone,
  onFocusThread,
  onHoverThread,
  onSetThreadIntent,
  onReplyThread,
  onDeleteThread,
  onEditThread,
  onMarkAddressed,
  onCreateFollowUp,
  onAskSide,
  onIterateThread,
  onIncludeAnswer,
  onKeepAnswerPrivate,
  threadAgentActions,
  sideQuestionsEnabled,
}: PlanProps) {
  const articleRef = useRef<HTMLElement>(null);
  const commentRailRef = useRef<HTMLElement>(null);
  const renderLines = planFormat === "html" ? renderSourceLines : renderPlanLines;
  const lines = useMemo(() => renderLines(plan), [plan, renderLines]);
  const highlightedCode = useHighlightedCode(planFormat === "markdown" ? plan : "", theme);
	const activeChange = proposal ? proposalChange : comparison;
  const outlineItems = useMemo(
    () => documentOutline(activeChange ? snapshotRenderText(activeChange.after) : plan, planFormat),
    [activeChange, plan, planFormat],
  );
  const [activeReviewStop, setActiveReviewStop] = useState<ReviewStop | null>(null);
	useEffect(() => {
	  if (!activeChange) setActiveReviewStop(null);
	}, [activeChange]);
  const comparisonBeforeLines = useMemo(
	() => (activeChange ? renderLines(snapshotRenderText(activeChange.before)) : []),
	[activeChange, renderLines],
  );
  const comparisonAfterLines = useMemo(
	() => (activeChange ? renderLines(snapshotRenderText(activeChange.after)) : []),
	[activeChange, renderLines],
  );
  const displayRows = useMemo(() => {
	if (!activeChange) {
      return lines.map((line, index) => ({
        diffKind: "context" as const,
        line,
        displayLineNumber: index + 1,
        anchorLineNumber: index + 1,
        beforeLineNumber: undefined,
        afterLineNumber: undefined,
        clusterId: undefined,
		rowId: `line-${index + 1}`,
        sourceLineNumber: index + 1,
      }));
    }
	const diffLines = activeChange.rows;
    return diffLines.map((diffLine) => {
	  const sourceLines = diffLine.kind === "add" ? comparisonAfterLines : comparisonBeforeLines;
      const sourceIndex = ((diffLine.kind === "add" ? diffLine.after : diffLine.before) ?? 0) - 1;
      const comparisonIdentity = comparison ? comparisonLineIdentity(diffLine) : null;
      return {
        diffKind: diffLine.kind,
        line: sourceLines[sourceIndex] ?? renderLines(diffLine.text)[0],
        displayLineNumber: comparisonIdentity?.displayLineNumber ?? diffLine.before ?? diffLine.after ?? 0,
        anchorLineNumber: comparisonIdentity
          ? comparisonIdentity.anchorLineNumber
          : diffLine.before ?? diffLine.after,
        beforeLineNumber: comparisonIdentity?.beforeLineNumber,
        afterLineNumber: comparisonIdentity?.afterLineNumber,
		clusterId: diffLine.clusterId,
		rowId: diffLine.id,
        sourceLineNumber: diffLine.after,
      };
    });
	}, [activeChange, comparison, comparisonAfterLines, comparisonBeforeLines, lines, renderLines]);
  const feedbackByRow = useMemo(() => {
	const byRow = new Map<number, RevisionFeedback[]>();
	if (!comparison?.isDirect) return byRow;
	const feedbackByID = new Map(comparison.feedback.map((item) => [`${item.revisionId}:${item.threadId}`, item]));
	for (const placement of comparison.feedbackPlacements) {
	  const feedback = feedbackByID.get(`${placement.revisionId}:${placement.threadId}`);
	  if (!feedback) continue;
	  const entries = byRow.get(placement.rowIndex) ?? [];
	  entries.push(feedback);
	  byRow.set(placement.rowIndex, entries);
	}
	return byRow;
  }, [comparison]);
  const lastProposalChangeIndex = useMemo(
    () => displayRows.reduce((last, row, index) => (row.diffKind === "context" ? last : index), -1),
    [displayRows],
  );
  const [draft, setDraft] = useState<CommentDraft | null>(null);
  const [submittingDraft, setSubmittingDraft] = useState(false);
  const [draftAgentAction, setDraftAgentAction] = useState<"asking" | "iterating" | null>(null);
  const [htmlView, setHTMLView] = useState<"preview" | "source">("preview");
  const showHTMLPreview = planFormat === "html" && htmlView === "preview" && !proposal && !comparison;
  const sideAnswersByThread = useMemo(() => groupSideAnswersByThread(sideAnswers), [sideAnswers]);
  const displayedThreads = useMemo(
    () => visibleThreads(threads, sideAnswers, commentFilter, focusedThreadId),
    [commentFilter, focusedThreadId, sideAnswers, threads],
  );
	const activeDisplayedThreads = useMemo(() => displayedThreads.filter((thread) => thread.bucket === "active"), [displayedThreads]);
	const attentionThreads = useMemo(() => displayedThreads.filter((thread) => thread.bucket === "attention"), [displayedThreads]);
	const historyThreads = useMemo(() => comparison ? [] : displayedThreads.filter((thread) => thread.bucket === "history"), [comparison, displayedThreads]);
	const [attentionExpanded, setAttentionExpanded] = useState(false);
	useEffect(() => {
	  if (attentionThreads.length === 0) setAttentionExpanded(false);
	  else if (commentFilter.trim() || attentionThreads.some((thread) => thread.id === focusedThreadId)) setAttentionExpanded(true);
	}, [attentionThreads, commentFilter, focusedThreadId]);
	const threadsAtPlacement = useMemo(
	  () => activeChange ? threadsByBackendPlacement(activeDisplayedThreads, activeChange.threadPlacements) : threadsByAnchorEnd(activeDisplayedThreads),
	  [activeChange, activeDisplayedThreads],
	);
	const commentRailLines = useMemo(() => [...threadsAtPlacement.keys()].sort((a, b) => a - b), [threadsAtPlacement]);
  const commentRailMetrics = useCommentRailMetrics(articleRef, commentRailRef, commentView, commentRailLines);

  const hoveredAnchor = useMemo(() => {
    if (!hoveredThreadId) return null;
    const t = threads.find((x) => x.id === hoveredThreadId);
    return t?.lifecycle === "active" ? t.anchor : null;
  }, [hoveredThreadId, threads]);
  const focusedAnchor = useMemo(() => {
    if (!focusedThreadId) return null;
    const t = threads.find((x) => x.id === focusedThreadId);
    return t?.lifecycle === "active" ? t.anchor : null;
  }, [focusedThreadId, threads]);
  const activeAnchor = hoveredAnchor ?? focusedAnchor;

  usePlanHighlights(articleRef, threads, activeAnchor, draft?.anchor ?? null);

  useEffect(() => {
    if (!editingThread) return;
	const maxLine = Math.max(1, lines.length);
	const editAnchor = editingThread.lifecycle === "detached"
	  ? { startLine: Math.min(Math.max(1, editingThread.anchor.startLine), maxLine), endLine: Math.min(Math.max(1, editingThread.anchor.endLine), maxLine) }
	  : editingThread.anchor;
    setDraft({
      threadId: editingThread.id,
	  anchor: editAnchor,
      selectedText:
		editingThread.lifecycle === "detached" ? selectedTextForAnchorInArticle(articleRef.current, editAnchor) : editingThread.selectedText || selectedTextForAnchorInArticle(articleRef.current, editAnchor),
      body: editingThread.messages[0]?.body ?? "",
      intent: editingThread.intent,
    });
  }, [editingThread, lines.length]);

  // Selecting text opens a convenience composer, but it remains a draft until
  // the reviewer explicitly submits it. A click away drops only an untouched
  // new draft, leaving native selection behavior (copy, lookup, and so on)
  // entirely under browser control.
  useEffect(() => {
    if (!draft || draft.threadId || draft.body.trim() || submittingDraft) return;
    const dismissEmptyDraft = (event: PointerEvent) => {
      const target = event.target instanceof Element ? event.target : null;
      if (target?.closest(".inline-comment-composer, .draft-boundary-handle")) return;
      setDraft(null);
    };
    document.addEventListener("pointerdown", dismissEmptyDraft);
    return () => document.removeEventListener("pointerdown", dismissEmptyDraft);
  }, [draft, submittingDraft]);

  // Map line -> first thread id anchored to it (for "go to thread" affordance).
  const lineToThread = useMemo(() => {
    const map = new Map<number, string>();
    for (const t of threads) {
	  if (t.lifecycle !== "active") continue;
      for (let i = t.anchor.startLine; i <= t.anchor.endLine; i++) {
        if (!map.has(i)) map.set(i, t.id);
      }
    }
    return map;
  }, [threads]);

  function openFullLineDraft(lineNumber: number) {
    if (disabled) return;
    const anchor = { startLine: lineNumber, endLine: lineNumber };
    setDraft({
      anchor,
      selectedText: selectedTextForAnchorInArticle(articleRef.current, anchor),
      body: "",
      intent: "instruction",
    });
  }

  function handlePointerUp(e: React.PointerEvent<HTMLElement>) {
    if (disabled) return;
    if ((e.target as HTMLElement | null)?.closest(".inline-comment-composer")) return;
    if ((e.target as HTMLElement | null)?.closest(".draft-boundary-handle")) return;
    const selection = window.getSelection();
    const selectionDraft = draftFromSelection(selection);
    const next = selectionDraft ? { ...selectionDraft, intent: "instruction" as const } : null;
    if (!next) return;
    setDraft(next);
    requestAnimationFrame(() => restoreNativeSelection(articleRef.current, next.anchor));
  }

  function currentSelectedText(current: CommentDraft): string {
    return selectedTextForAnchorInArticle(articleRef.current, current.anchor) || current.selectedText;
  }

  async function submitDraft() {
    if (submittingDraft || !draft || !draft.body.trim()) return;
    setSubmittingDraft(true);
    try {
      const ok = draft.threadId
        ? await onUpdateComment(draft.threadId, draft.anchor, draft.body.trim(), currentSelectedText(draft))
        : await onCreateComment(draft.anchor, draft.body.trim(), currentSelectedText(draft), draft.intent);
      if (!ok) return;
      if (draft.threadId) onEditDone();
      setDraft(null);
    } finally {
      setSubmittingDraft(false);
    }
  }

  async function askSideFromDraft() {
    if (submittingDraft || !draft || draft.threadId || !draft.body.trim()) return;
    setSubmittingDraft(true);
    setDraftAgentAction("asking");
    try {
      const ok = await onAskSideFromDraft(draft.anchor, draft.body.trim(), currentSelectedText(draft));
      if (!ok) return;
      setDraft(null);
    } finally {
      setDraftAgentAction(null);
      setSubmittingDraft(false);
    }
  }

  async function iterateDraft() {
    if (submittingDraft || !draft || draft.threadId || !draft.body.trim()) return;
    setSubmittingDraft(true);
    setDraftAgentAction("iterating");
    try {
      const ok = await onIterateDraft(draft.anchor, draft.body.trim(), currentSelectedText(draft));
      if (!ok) return;
      setDraft(null);
    } finally {
      setDraftAgentAction(null);
      setSubmittingDraft(false);
    }
  }

  function cancelDraft() {
    setDraft(null);
    if (draft?.threadId) onEditDone();
  }

  const updateDraftAnchor = useCallback((anchor: Anchor) => {
    setDraft((current) =>
      current
        ? {
            ...current,
            anchor,
            selectedText: selectedTextForAnchorInArticle(articleRef.current, anchor),
          }
        : current,
    );
  }, []);
  const draftComposerPlacement = draft
    ? inlineCommentComposerPlacement(draft.anchor.endLine, lines.length)
    : null;

  function navigateToOutline(item: OutlineItem) {
    if (showHTMLPreview) setHTMLView("source");
    window.requestAnimationFrame(() => window.requestAnimationFrame(() => {
      const target = document.querySelector<HTMLElement>(`[data-document-line="${item.line}"]`);
      target?.scrollIntoView({
        block: "start",
        behavior: reviewScrollBehavior(Boolean(window.matchMedia?.("(prefers-reduced-motion: reduce)").matches)),
      });
    }));
  }

  return (
    <div className={`plan-with-comment-rail is-${showHTMLPreview ? "inline" : commentView}${activeChange ? " has-review-navigation" : ""}`}>
    <article
      ref={articleRef}
      className="plan-markdown relative overflow-hidden rounded-[var(--radius-card)] border border-border bg-surface-elevated shadow-[var(--shadow-soft)]"
      onPointerUp={handlePointerUp}
    >
      <header className="plan-comment-toolbar">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-xs font-semibold text-foreground-muted">Comments</span>
          <button
            type="button"
            className={`btn btn-sm${commentView === "inline" ? " btn-primary" : " btn-ghost"}`}
            onClick={() => onCommentViewChange("inline")}
            aria-pressed={commentView === "inline"}
            title="Show threads directly below their anchored range"
          >
            <ListTree size={13} /> In place
          </button>
          <button
            type="button"
            className={`btn btn-sm${commentView === "alongside" ? " btn-primary" : " btn-ghost"}`}
            onClick={() => onCommentViewChange("alongside")}
            aria-pressed={commentView === "alongside"}
            title="Show threads beside their final anchored line"
          >
            <Columns2 size={13} /> Alongside
          </button>
          {planFormat === "html" ? (
            <span className="html-view-toggle" aria-label="HTML plan view">
              <button
                type="button"
                className={`btn btn-sm${htmlView === "preview" ? " btn-primary" : " btn-ghost"}`}
                onClick={() => setHTMLView("preview")}
                aria-pressed={htmlView === "preview"}
                disabled={Boolean(proposal || comparison)}
                title={proposal || comparison ? "Preview is unavailable while showing source changes" : "Render the HTML plan safely"}
              >
                <Eye size={13} /> Preview
              </button>
              <button
                type="button"
                className={`btn btn-sm${htmlView === "source" || proposal || comparison ? " btn-primary" : " btn-ghost"}`}
                onClick={() => setHTMLView("source")}
                aria-pressed={htmlView === "source" || Boolean(proposal || comparison)}
              >
                <Code2 size={13} /> Source
              </button>
            </span>
          ) : null}
        </div>
        <label className="relative block min-w-48">
          <Search size={13} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-foreground-muted" />
          <input
            id="thread-filter"
            type="search"
            className="field h-8 py-1 pl-7 text-xs"
            placeholder="Filter comments"
            value={commentFilter}
            onChange={(event) => onCommentFilterChange(event.target.value)}
            autoComplete="off"
          />
        </label>
      </header>
	  {proposal?.kind === "review" ? (
	    <ProposalActions
	      proposal={proposal}
	      disabled={proposalDisabled}
	      iterating={proposalIterating}
	      prominent
	      onApply={onApplyProposal}
	      onDiscard={onDiscardProposal}
	      onIterate={onIterateProposal}
	    />
	  ) : null}
	  {!proposal && comparisonLoading ? (
	    <div className="plan-comparison-loading" role="status" aria-live="polite">
	      <Loader2 size={15} className="animate-spin" /> Loading revision changes…
	    </div>
	  ) : null}
	  {!proposal && comparison ? (
	    <div className="plan-comparison-banner">
		  <span><GitCompareArrows size={14} /> Showing changes: {comparison.baseId} → {comparison.targetId}</span>
	      <span className="comparison-line-key">Line numbers: before → current</span>
	      <button type="button" className="btn btn-ghost btn-sm" onClick={onClearComparison}>Hide changes</button>
	    </div>
	  ) : null}
	  {attentionThreads.length > 0 ? <details className="comment-state-overview attention-overview" open={attentionExpanded} onToggle={(event) => setAttentionExpanded(event.currentTarget.open)}>
		<summary className="attention-overview-summary">
		  <span><AlertTriangle size={14} /> {attentionThreads.length} unanchored {attentionThreads.length === 1 ? "comment" : "comments"}</span>
		  <small>Review separately · In place/Alongside applies only to anchored comments</small>
		</summary>
		<div className="attention-overview-body">
		<CommentThreadStack
		  threads={attentionThreads}
		  sideAnswersByThread={sideAnswersByThread}
		  focusedThreadId={focusedThreadId}
		  reviewTargetThreadId={activeReviewStop?.kind === "comment" ? activeReviewStop.threadId : undefined}
		  onHover={onHoverThread}
		  onSetIntent={onSetThreadIntent}
		  onReply={onReplyThread}
		  onDelete={onDeleteThread}
		  onEdit={onEditThread}
		  onMarkAddressed={onMarkAddressed}
		  onCreateFollowUp={onCreateFollowUp}
		  onAskSide={onAskSide}
		  onIterate={onIterateThread}
		  onInclude={onIncludeAnswer}
		  onKeepPrivate={onKeepAnswerPrivate}
		  agentActions={threadAgentActions}
		  disabled={disabled}
		  sideQuestionsEnabled={sideQuestionsEnabled}
		  placement="inline"
		  historyOpen={false}
		/>
		</div>
	  </details> : null}
	  {historyThreads.length > 0 ? <div className="comment-state-overview history-overview">
		<CommentThreadStack
		  threads={historyThreads}
		  sideAnswersByThread={sideAnswersByThread}
		  focusedThreadId={focusedThreadId}
		  onHover={onHoverThread}
		  onSetIntent={onSetThreadIntent}
		  onReply={onReplyThread}
		  onDelete={onDeleteThread}
		  onEdit={onEditThread}
		  onMarkAddressed={onMarkAddressed}
		  onCreateFollowUp={onCreateFollowUp}
		  onAskSide={onAskSide}
		  onIterate={onIterateThread}
		  onInclude={onIncludeAnswer}
		  onKeepPrivate={onKeepAnswerPrivate}
		  agentActions={threadAgentActions}
		  disabled={disabled}
		  sideQuestionsEnabled={sideQuestionsEnabled}
		  placement="inline"
		  historyOpen={Boolean(commentFilter.trim() || historyThreads.some((thread) => thread.id === focusedThreadId))}
		/>
	  </div> : null}
	  {!proposal && comparison && !comparison.isDirect && comparison.feedback.length > 0 ? (
		<RevisionFeedbackSummary feedback={comparison.feedback} />
      ) : null}
      {showHTMLPreview ? (
        <div className="html-preview-notice">
          HTML is rendered in a scriptless, network-blocked sandbox. Switch to Source to comment or iterate.
          {threads.length > 0 ? ` ${threads.length} existing ${threads.length === 1 ? "comment is" : "comments are"} available in Source.` : ""}
        </div>
      ) : null}
      <div className="plan-body py-2">
        {showHTMLPreview ? (
          <HTMLPlanPreview source={plan} theme={theme} />
        ) : displayRows.map((row, idx) => {
          const lineNumber = row.anchorLineNumber;
          const line = row.line;
          const isProposedLine = Boolean(proposal && row.diffKind === "add");
          const isHistoricalLine = Boolean(comparison && row.diffKind === "remove");
          const commentable = !isProposedLine && !isHistoricalLine && lineNumber !== undefined;
		  const commentPlacement = activeChange ? idx : lineNumber;
		  const lineThreads = commentPlacement === undefined ? [] : threadsAtPlacement.get(commentPlacement) ?? [];
          const inDraft = commentable && draft ? anchorTouchesLine(draft.anchor, lineNumber) : false;
          const inHoverAnchor = commentable && activeAnchor && anchorTouchesLine(activeAnchor, lineNumber);
          const anchoredThreadId = commentable ? lineToThread.get(lineNumber) : undefined;
          const comparisonGutter = comparison
            ? comparisonGutterValues(row.beforeLineNumber, row.afterLineNumber)
            : null;
          return (
			<div key={`${row.diffKind}-${row.beforeLineNumber ?? "-"}-${row.afterLineNumber ?? "-"}-${idx}`} className={`plan-row-with-comments${activeReviewStop?.kind === "change" && activeReviewStop.clusterId === row.clusterId ? " is-review-target" : ""}${commentView === "alongside" && lineThreads.length > 0 ? " has-alongside-comment" : ""}`}>
              <div
                className="plan-row-main"
                style={
				  commentView === "alongside" && commentPlacement !== undefined
				    ? { minHeight: commentRailMetrics.get(commentPlacement)?.height }
                    : undefined
                }
              >
                <div
                  data-line={commentable ? lineNumber : undefined}
				  data-comment-placement={commentPlacement}
				  data-change-cluster={row.clusterId}
				  data-change-row={row.rowId}
                  data-document-line={row.sourceLineNumber}
                  className={`line-row${line.kind === "blank" ? " is-blank" : ""}${
                    line.kind === "table-header" ? " is-table-header" : ""
                  }${line.kind === "table-divider" ? " is-table-divider" : ""}${
                    line.kind === "table-row" ? " is-table-row" : ""
                  }${comparison ? " is-comparison" : ""}${
                    inDraft ? " is-anchored" : ""
                  }${inHoverAnchor ? " is-hover-anchor" : ""}${
                    row.diffKind === "remove" ? " is-proposal-remove" : ""
                  }${row.diffKind === "add" ? " is-proposal-add" : ""}`}
                >
                  <div className="line-number">
                    {comparison ? (
                      <span
                        className="comparison-line-numbers"
                        aria-label={`Before line ${row.beforeLineNumber ?? "none"}; current line ${row.afterLineNumber ?? "none"}`}
                      >
                        {row.beforeLineNumber !== undefined ? (
                          <span className="comparison-line-number is-before">{comparisonGutter?.before}</span>
                        ) : (
                          <span className="comparison-line-marker is-add" aria-hidden="true">{comparisonGutter?.before}</span>
                        )}
                        {row.afterLineNumber !== undefined ? (
                          <span className="comparison-line-number is-current">{comparisonGutter?.after}</span>
                        ) : (
                          <span className="comparison-line-marker is-remove" aria-hidden="true">{comparisonGutter?.after}</span>
                        )}
                      </span>
                    ) : row.displayLineNumber}
                  </div>
                  <div className="pin-cell">
                    {commentable ? (
                      <button
                        type="button"
                        className={`pin-btn${anchoredThreadId ? " has-anchor" : ""}`}
                        title={anchoredThreadId ? "Open existing thread" : `Comment on line ${lineNumber}`}
                        aria-label={anchoredThreadId ? "Open existing thread" : `Comment on line ${lineNumber}`}
                        onMouseDown={(event) => event.stopPropagation()}
                        onClick={(event) => {
                          event.stopPropagation();
                          if (anchoredThreadId) onFocusThread(anchoredThreadId);
                          else openFullLineDraft(lineNumber);
                        }}
                        disabled={disabled}
                      >
                        <MessageSquarePlus size={14} />
                      </button>
                    ) : null}
                  </div>
				  <RenderedLine
					html={line.html}
					lineNumber={commentable ? lineNumber : undefined}
                    isTableRow={line.kind === "table-header" || line.kind === "table-divider" || line.kind === "table-row"}
                    anchoredThreadId={anchoredThreadId}
                    codeTokens={isProposedLine || lineNumber === undefined ? undefined : highlightedCode.get(lineNumber)}
                    onFocusThread={onFocusThread}
                  />
                </div>

				{comparison && feedbackByRow.get(idx)?.length ? (
				  <RevisionFeedbackList feedback={feedbackByRow.get(idx) ?? []} activeFeedbackId={activeReviewStop?.kind === "feedback" ? `${activeReviewStop.revisionId}:${activeReviewStop.threadId}` : undefined} />
                ) : null}

                {commentable && draft && draftComposerPlacement?.afterLine === lineNumber ? (
                  <InlineCommentComposer
                    draft={draft}
                    spacerLines={draftComposerPlacement.spacerLines}
                    submitting={submittingDraft}
                    agentAction={draftAgentAction}
                    disabled={disabled}
                    setDraft={setDraft}
                    onCancel={cancelDraft}
                    onSubmit={submitDraft}
                    onAskSide={askSideFromDraft}
                    onIterate={iterateDraft}
                  />
                ) : null}
				{commentView === "inline" && lineThreads.length > 0 ? (
				  <CommentThreadStack
                    threads={lineThreads}
                    sideAnswersByThread={sideAnswersByThread}
                    focusedThreadId={focusedThreadId}
					reviewTargetThreadId={activeReviewStop?.kind === "comment" ? activeReviewStop.threadId : undefined}
                    onHover={onHoverThread}
					onSetIntent={onSetThreadIntent}
                    onReply={onReplyThread}
                    onDelete={onDeleteThread}
                    onEdit={onEditThread}
					onMarkAddressed={onMarkAddressed}
					onCreateFollowUp={onCreateFollowUp}
                    onAskSide={onAskSide}
                    onIterate={onIterateThread}
					onInclude={onIncludeAnswer}
					onKeepPrivate={onKeepAnswerPrivate}
                    agentActions={threadAgentActions}
                    disabled={disabled}
                    sideQuestionsEnabled={sideQuestionsEnabled}
                    placement="inline"
                  />
                ) : null}
				{proposal && proposal.kind !== "review" && idx === lastProposalChangeIndex ? (
				  <ProposalActions
                    proposal={proposal}
                    disabled={proposalDisabled}
                    iterating={proposalIterating}
                    onApply={onApplyProposal}
                    onDiscard={onDiscardProposal}
                    onIterate={onIterateProposal}
                  />
                ) : null}
              </div>
            </div>
          );
        })}
      </div>
      {!showHTMLPreview ? (
        <DraftBoundaryHandles
          articleRef={articleRef}
          anchor={draft?.anchor ?? null}
          onChange={updateDraftAnchor}
        />
      ) : null}
    </article>
    {commentView === "alongside" && !showHTMLPreview ? (
      <aside ref={commentRailRef} className="plan-comment-rail" aria-label="Comments alongside plan lines">
        {commentRailLines.map((lineNumber) => {
          const metric = commentRailMetrics.get(lineNumber);
          return (
			<CommentThreadStack
              key={lineNumber}
			  threads={threadsAtPlacement.get(lineNumber) ?? []}
              sideAnswersByThread={sideAnswersByThread}
              focusedThreadId={focusedThreadId}
			  reviewTargetThreadId={activeReviewStop?.kind === "comment" ? activeReviewStop.threadId : undefined}
              onHover={onHoverThread}
			  onSetIntent={onSetThreadIntent}
              onReply={onReplyThread}
              onDelete={onDeleteThread}
              onEdit={onEditThread}
			  onMarkAddressed={onMarkAddressed}
			  onCreateFollowUp={onCreateFollowUp}
              onAskSide={onAskSide}
              onIterate={onIterateThread}
			  onInclude={onIncludeAnswer}
			  onKeepPrivate={onKeepAnswerPrivate}
              agentActions={threadAgentActions}
              disabled={disabled}
              sideQuestionsEnabled={sideQuestionsEnabled}
              placement="alongside"
              anchorLine={lineNumber}
              top={metric?.top}
              hidden={!metric}
            />
          );
        })}
      </aside>
    ) : null}
    <DocumentOutline items={outlineItems} onNavigate={navigateToOutline} />
    {activeChange ? (
      <ReviewNavigator
        identity={`${activeChange.baseId}:${activeChange.targetId}`}
        stops={activeChange.reviewStops}
        onFocusThread={onFocusThread}
        onActiveChange={setActiveReviewStop}
      />
    ) : null}
    </div>
  );
});

function snapshotRenderText(snapshot: DocumentSnapshot): string {
  return snapshot.lines.join("\n") + (snapshot.terminalNewline ? "\n" : "");
}

export const Plan = ReviewDocument;

function HTMLPlanPreview({ source, theme }: { source: string; theme: "light" | "dark" }) {
  const srcDoc = useMemo(() => htmlPreviewDocument(source, theme), [source, theme]);
  return (
    <iframe
      className="html-plan-preview"
      title="Rendered HTML plan preview"
      sandbox=""
      srcDoc={srcDoc}
    />
  );
}

function useHighlightedCode(plan: string, theme: "light" | "dark") {
  const [highlighted, setHighlighted] = useState<Map<number, HighlightToken[]>>(new Map());

  useEffect(() => {
    let current = true;
    void highlightCodeBlocks(plan, theme).then((next) => {
      if (current) setHighlighted(next);
    });
    return () => {
      current = false;
    };
  }, [plan, theme]);

  return highlighted;
}

function InlineCommentComposer({
  draft,
  spacerLines,
  submitting,
  agentAction,
  disabled,
  setDraft,
  onCancel,
  onSubmit,
  onAskSide,
  onIterate,
}: {
  draft: CommentDraft;
  spacerLines: number;
  submitting: boolean;
  agentAction: "asking" | "iterating" | null;
  disabled: boolean;
  setDraft: (draft: CommentDraft) => void;
  onCancel: () => void;
  onSubmit: () => void;
  onAskSide: () => void;
  onIterate: () => void;
}) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const canSubmit = draft.body.trim().length > 0;
  const isEditing = Boolean(draft.threadId);

  useEffect(() => {
    textareaRef.current?.focus();
  }, []);

  function onBodyKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      onSubmit();
    }
    if (e.key === "Escape") {
      e.preventDefault();
      onCancel();
    }
  }

  return (
    <div
      className="inline-comment-composer"
      style={spacerLines > 0 ? { marginTop: `calc(8px + ${spacerLines} * 1.7em)` } : undefined}
    >
      <div className="inline-comment-header">
        <span>{anchorLabel(draft.anchor)}</span>
      </div>
      <label className="block text-xs font-semibold text-foreground-muted">
        Comment
        <textarea
          ref={textareaRef}
          value={draft.body}
          onChange={(e) => setDraft({ ...draft, body: e.target.value })}
          onKeyDown={onBodyKeyDown}
          rows={3}
          placeholder="Leave a comment for this selection..."
          className="field mt-1 resize-y font-sans"
          disabled={submitting || disabled}
        />
      </label>
      {agentAction ? (
        <div className="btw-thinking mt-2" role="status" aria-live="polite">
          <Sparkles size={13} />
          <span>{agentAction === "asking" ? "Codex is thinking about this /btw…" : "Codex is iterating on this selection…"}</span>
        </div>
      ) : null}
	  {!isEditing ? <fieldset className="composer-intent" aria-label="Comment intent">
		<legend>After saving</legend>
		<button type="button" className={`kind-pill${draft.intent === "instruction" ? " is-active is-go" : ""}`} onClick={() => setDraft({ ...draft, intent: "instruction" })} disabled={submitting || disabled} aria-pressed={draft.intent === "instruction"}>Use in iteration</button>
		<button type="button" className={`kind-pill${draft.intent === "private" ? " is-active is-stay" : ""}`} onClick={() => setDraft({ ...draft, intent: "private" })} disabled={submitting || disabled} aria-pressed={draft.intent === "private"}>Private note</button>
	  </fieldset> : null}
      <div className="flex justify-end gap-2">
        <button type="button" className="btn" onClick={onCancel} disabled={submitting || disabled}>
          Cancel
        </button>
        <button
          type="button"
          className="btn btn-primary"
          onClick={onSubmit}
          disabled={!canSubmit || submitting || disabled}
        >
          {submitting ? (agentAction ? "Processing…" : "Saving…") : isEditing ? "Save comment" : "Add comment"}
        </button>
        {!isEditing ? (
          <button
            type="button"
            className="btn"
            onClick={onAskSide}
            disabled={!canSubmit || submitting || disabled}
            title="Save this comment and ask Codex about the selected text on the side"
          >
            {agentAction === "asking" ? "Asking…" : "/btw"}
          </button>
        ) : null}
        {!isEditing ? (
          <button
            type="button"
            className="btn"
            onClick={onIterate}
            disabled={!canSubmit || submitting || disabled}
            title="Ask Codex to rewrite only the selected section"
          >
            <Sparkles size={13} /> {agentAction === "iterating" ? "Iterating…" : "Iterate section"}
          </button>
        ) : null}
      </div>
    </div>
  );
}
