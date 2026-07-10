import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { CheckCircle2, Columns2, ListTree, MessageSquarePlus, RotateCcw, Search, Sparkles, Trash2 } from "lucide-react";
import { renderPlanLines } from "../lib/markdown";
import type { Anchor, SectionProposal, SideAnswer, Thread, ThreadKind } from "../types";
import { anchorLabel, anchorTouchesLine } from "../lib/anchors";
import { inlineCommentComposerPlacement } from "../lib/commentPlacement";
import { lineDiff } from "../lib/diff";
import { highlightCodeBlocks, type HighlightToken } from "../lib/codeHighlight";
import { groupSideAnswersByThread, threadsByAnchorEnd, visibleThreads } from "../lib/threadPlacement";
import { ThreadCard } from "./ThreadCard";

export type CommentView = "inline" | "alongside";

interface PlanProps {
  plan: string;
  theme: "light" | "dark";
  proposal?: SectionProposal | null;
  threads: Thread[];
  sideAnswers: SideAnswer[];
  hoveredThreadId: string | null;
  focusedThreadId: string | null;
  editingThread: Thread | null;
  commentView: CommentView;
  commentFilter: string;
  onCommentViewChange: (view: CommentView) => void;
  onCommentFilterChange: (filter: string) => void;
  onCreateComment: (anchor: Anchor, body: string, selectedText: string) => Promise<boolean>;
  onUpdateComment: (threadId: string, anchor: Anchor, body: string, selectedText: string) => Promise<boolean>;
  onAskSideFromDraft: (anchor: Anchor, body: string, selectedText: string) => Promise<boolean>;
  onIterateDraft: (anchor: Anchor, instruction: string) => Promise<boolean>;
  disabled: boolean;
  proposalDisabled: boolean;
  proposalIterating: boolean;
  onApplyProposal: (proposalId: string) => void;
  onDiscardProposal: (proposalId: string) => void;
  onIterateProposal: (anchor: Anchor, instruction: string) => Promise<boolean>;
  onEditDone: () => void;
  onFocusThread: (threadId: string) => void;
  onHoverThread: (threadId: string | null) => void;
  onSetThreadKind: (threadId: string, kind: ThreadKind) => void | Promise<void>;
  onReplyThread: (threadId: string) => void;
  onDeleteThread: (threadId: string) => void;
  onEditThread: (threadId: string) => void;
  onAskSide: (thread: Thread) => void;
  onIterateThread: (thread: Thread) => void | Promise<void>;
  onPromoteAnswer: (answerId: string) => void;
  onUnpromoteAnswer: (answerId: string) => void;
  threadAgentActions: Record<string, "asking" | "iterating">;
  sideQuestionsEnabled: boolean;
}

interface CommentDraft {
  threadId?: string;
  anchor: Anchor;
  selectedText: string;
  body: string;
}

interface CommentRailMetric {
  top: number;
  height: number;
}

export const Plan = memo(function Plan({
  plan,
  theme,
  proposal,
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
  onSetThreadKind,
  onReplyThread,
  onDeleteThread,
  onEditThread,
  onAskSide,
  onIterateThread,
  onPromoteAnswer,
  onUnpromoteAnswer,
  threadAgentActions,
  sideQuestionsEnabled,
}: PlanProps) {
  const articleRef = useRef<HTMLElement>(null);
  const commentRailRef = useRef<HTMLElement>(null);
  const lines = useMemo(() => renderPlanLines(plan), [plan]);
  const highlightedCode = useHighlightedCode(plan, theme);
  const proposalLines = useMemo(
    () => (proposal ? renderPlanLines(proposal.proposedPlan) : []),
    [proposal],
  );
  const displayRows = useMemo(() => {
    if (!proposal) {
      return lines.map((line, index) => ({ diffKind: "context" as const, line, lineNumber: index + 1 }));
    }
    return lineDiff(plan, proposal.proposedPlan).map((diffLine) => {
      const lineNumber = diffLine.before ?? diffLine.after ?? 0;
      const sourceLines = diffLine.kind === "add" ? proposalLines : lines;
      const sourceIndex = ((diffLine.kind === "add" ? diffLine.after : diffLine.before) ?? 0) - 1;
      return {
        diffKind: diffLine.kind,
        line: sourceLines[sourceIndex] ?? renderPlanLines(diffLine.text)[0],
        lineNumber,
      };
    });
  }, [lines, plan, proposal, proposalLines]);
  const lastProposalChangeIndex = useMemo(
    () => displayRows.reduce((last, row, index) => (row.diffKind === "context" ? last : index), -1),
    [displayRows],
  );
  const [draft, setDraft] = useState<CommentDraft | null>(null);
  const [submittingDraft, setSubmittingDraft] = useState(false);
  const [draftAgentAction, setDraftAgentAction] = useState<"asking" | "iterating" | null>(null);
  const sideAnswersByThread = useMemo(() => groupSideAnswersByThread(sideAnswers), [sideAnswers]);
  const displayedThreads = useMemo(
    () => visibleThreads(threads, sideAnswers, commentFilter, focusedThreadId),
    [commentFilter, focusedThreadId, sideAnswers, threads],
  );
  const threadsAtLine = useMemo(() => threadsByAnchorEnd(displayedThreads), [displayedThreads]);
  const commentRailLines = useMemo(() => [...threadsAtLine.keys()].sort((a, b) => a - b), [threadsAtLine]);
  const commentRailMetrics = useCommentRailMetrics(articleRef, commentRailRef, commentView, commentRailLines);

  const hoveredAnchor = useMemo(() => {
    if (!hoveredThreadId) return null;
    const t = threads.find((x) => x.id === hoveredThreadId);
    return t?.status === "open" ? t.anchor : null;
  }, [hoveredThreadId, threads]);
  const focusedAnchor = useMemo(() => {
    if (!focusedThreadId) return null;
    const t = threads.find((x) => x.id === focusedThreadId);
    return t?.status === "open" ? t.anchor : null;
  }, [focusedThreadId, threads]);
  const activeAnchor = hoveredAnchor ?? focusedAnchor;

  usePlanHighlights(articleRef, threads, activeAnchor, draft?.anchor ?? null);

  useEffect(() => {
    if (!editingThread) return;
    setDraft({
      threadId: editingThread.id,
      anchor: editingThread.anchor,
      selectedText:
        editingThread.selectedText || selectedTextForAnchorInArticle(articleRef.current, editingThread.anchor),
      body: editingThread.messages[0]?.body ?? "",
    });
  }, [editingThread]);

  // Map line -> first thread id anchored to it (for "go to thread" affordance).
  const lineToThread = useMemo(() => {
    const map = new Map<number, string>();
    for (const t of threads) {
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
    });
  }

  function handlePointerUp(e: React.PointerEvent<HTMLElement>) {
    if (disabled) return;
    if ((e.target as HTMLElement | null)?.closest(".inline-comment-composer")) return;
    if ((e.target as HTMLElement | null)?.closest(".draft-boundary-handle")) return;
    const selection = window.getSelection();
    const next = draftFromSelection(selection);
    if (!next) return;
    setDraft(next);
    selection?.removeAllRanges();
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
        : await onCreateComment(draft.anchor, draft.body.trim(), currentSelectedText(draft));
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
      const ok = await onIterateDraft(draft.anchor, draft.body.trim());
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

  return (
    <div className={`plan-with-comment-rail is-${commentView}`}>
    <article
      ref={articleRef}
      className="plan-markdown relative overflow-hidden rounded-[var(--radius-card)] border border-border bg-surface-elevated shadow-[var(--shadow-soft)]"
      onPointerUp={handlePointerUp}
    >
      <header className="plan-comment-toolbar">
        <div className="flex items-center gap-1.5">
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
      <div className="plan-body py-2">
        {displayRows.map((row, idx) => {
          const lineNumber = row.lineNumber;
          const line = row.line;
          const isProposedLine = row.diffKind === "add";
          const lineThreads = isProposedLine ? [] : threadsAtLine.get(lineNumber) ?? [];
          const inDraft = !isProposedLine && draft ? anchorTouchesLine(draft.anchor, lineNumber) : false;
          const inHoverAnchor = !isProposedLine && activeAnchor && anchorTouchesLine(activeAnchor, lineNumber);
          const anchoredThreadId = isProposedLine ? undefined : lineToThread.get(lineNumber);
          return (
            <div key={`${row.diffKind}-${lineNumber}-${idx}`} className="plan-row-with-comments">
              <div
                className="plan-row-main"
                style={
                  commentView === "alongside" && !isProposedLine
                    ? { minHeight: commentRailMetrics.get(lineNumber)?.height }
                    : undefined
                }
              >
                <div
                  data-line={isProposedLine ? undefined : lineNumber}
                  className={`line-row${line.kind === "blank" ? " is-blank" : ""}${
                    line.kind === "table-header" ? " is-table-header" : ""
                  }${line.kind === "table-divider" ? " is-table-divider" : ""}${
                    line.kind === "table-row" ? " is-table-row" : ""
                  }${
                    inDraft ? " is-anchored" : ""
                  }${inHoverAnchor ? " is-hover-anchor" : ""}${
                    row.diffKind === "remove" ? " is-proposal-remove" : ""
                  }${row.diffKind === "add" ? " is-proposal-add" : ""}`}
                >
                  <div className="line-number">{lineNumber}</div>
                  <div className="pin-cell">
                    {!isProposedLine ? (
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
                  <PlanLineContent
                    html={line.html}
                    lineNumber={lineNumber}
                    anchoredThreadId={anchoredThreadId}
                    codeTokens={isProposedLine ? undefined : highlightedCode.get(lineNumber)}
                    onFocusThread={onFocusThread}
                  />
                </div>

                {draft && draftComposerPlacement?.afterLine === lineNumber ? (
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
                  <PlanThreadStack
                    threads={lineThreads}
                    sideAnswersByThread={sideAnswersByThread}
                    focusedThreadId={focusedThreadId}
                    onHover={onHoverThread}
                    onSetKind={onSetThreadKind}
                    onReply={onReplyThread}
                    onDelete={onDeleteThread}
                    onEdit={onEditThread}
                    onAskSide={onAskSide}
                    onIterate={onIterateThread}
                    onPromote={onPromoteAnswer}
                    onUnpromote={onUnpromoteAnswer}
                    agentActions={threadAgentActions}
                    disabled={disabled}
                    sideQuestionsEnabled={sideQuestionsEnabled}
                    placement="inline"
                  />
                ) : null}
                {proposal && idx === lastProposalChangeIndex ? (
                  <InlineProposalControls
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
      <DraftBoundaryHandles
        articleRef={articleRef}
        anchor={draft?.anchor ?? null}
        onChange={updateDraftAnchor}
      />
    </article>
    {commentView === "alongside" ? (
      <aside ref={commentRailRef} className="plan-comment-rail" aria-label="Comments alongside plan lines">
        {commentRailLines.map((lineNumber) => {
          const metric = commentRailMetrics.get(lineNumber);
          return (
            <PlanThreadStack
              key={lineNumber}
              threads={threadsAtLine.get(lineNumber) ?? []}
              sideAnswersByThread={sideAnswersByThread}
              focusedThreadId={focusedThreadId}
              onHover={onHoverThread}
              onSetKind={onSetThreadKind}
              onReply={onReplyThread}
              onDelete={onDeleteThread}
              onEdit={onEditThread}
              onAskSide={onAskSide}
              onIterate={onIterateThread}
              onPromote={onPromoteAnswer}
              onUnpromote={onUnpromoteAnswer}
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
    </div>
  );
});

function PlanThreadStack({
  threads,
  sideAnswersByThread,
  focusedThreadId,
  onHover,
  onSetKind,
  onReply,
  onDelete,
  onEdit,
  onAskSide,
  onIterate,
  onPromote,
  onUnpromote,
  agentActions,
  disabled,
  sideQuestionsEnabled,
  placement,
  anchorLine,
  top,
  hidden,
}: {
  threads: Thread[];
  sideAnswersByThread: Map<string, SideAnswer[]>;
  focusedThreadId: string | null;
  onHover: (threadId: string | null) => void;
  onSetKind: (threadId: string, kind: ThreadKind) => void | Promise<void>;
  onReply: (threadId: string) => void;
  onDelete: (threadId: string) => void;
  onEdit: (threadId: string) => void;
  onAskSide: (thread: Thread) => void;
  onIterate: (thread: Thread) => void | Promise<void>;
  onPromote: (answerId: string) => void;
  onUnpromote: (answerId: string) => void;
  agentActions: Record<string, "asking" | "iterating">;
  disabled: boolean;
  sideQuestionsEnabled: boolean;
  placement: CommentView;
  anchorLine?: number;
  top?: number;
  hidden?: boolean;
}) {
  return (
    <section
      className={`plan-thread-stack is-${placement}`}
      aria-label="Comments"
      data-anchor-line={anchorLine}
      style={placement === "alongside" ? { top, visibility: hidden ? "hidden" : undefined } : undefined}
    >
      {placement === "inline" ? (
        <div className="inline-thread-stack-label">
          {threads.length === 1 ? "Comment" : `${threads.length} comments`}
        </div>
      ) : null}
      {threads.map((thread) => (
        <ThreadCard
          key={thread.id}
          thread={thread}
          kind={thread.kind ?? "decision"}
          sideAnswers={sideAnswersByThread.get(thread.id) ?? []}
          isFocused={focusedThreadId === thread.id}
          onHover={onHover}
          onSetKind={onSetKind}
          onReply={onReply}
          onDelete={onDelete}
          onEdit={onEdit}
          onAskSide={onAskSide}
          onIterate={onIterate}
          onPromote={onPromote}
          onUnpromote={onUnpromote}
          agentAction={agentActions[thread.id]}
          disabled={disabled}
          sideQuestionsEnabled={sideQuestionsEnabled}
          presentation={placement === "inline" ? "inline" : "rail"}
        />
      ))}
    </section>
  );
}

function useCommentRailMetrics(
  articleRef: React.RefObject<HTMLElement>,
  railRef: React.RefObject<HTMLElement>,
  commentView: CommentView,
  lines: number[],
) {
  const [metrics, setMetrics] = useState<Map<number, CommentRailMetric>>(new Map());
  const lineKey = lines.join(",");

  useLayoutEffect(() => {
    if (commentView !== "alongside") {
      setMetrics(new Map());
      return;
    }
    const article = articleRef.current;
    const rail = railRef.current;
    if (!article || !rail) return;

    let frame = 0;
    const update = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const articleRect = article.getBoundingClientRect();
        const next = new Map<number, CommentRailMetric>();
        for (const line of lines) {
          const row = article.querySelector<HTMLElement>(`.line-row[data-line="${line}"]`);
          const stack = rail.querySelector<HTMLElement>(`[data-anchor-line="${line}"]`);
          if (!row || !stack) continue;
          next.set(line, {
            top: Math.round(row.getBoundingClientRect().top - articleRect.top),
            height: Math.ceil(stack.getBoundingClientRect().height),
          });
        }
        setMetrics((current) => (sameCommentRailMetrics(current, next) ? current : next));
      });
    };

    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(update);
    observer?.observe(article);
    for (const line of lines) {
      const stack = rail.querySelector<HTMLElement>(`[data-anchor-line="${line}"]`);
      if (stack) observer?.observe(stack);
    }
    update();
    window.addEventListener("resize", update);
    return () => {
      cancelAnimationFrame(frame);
      observer?.disconnect();
      window.removeEventListener("resize", update);
    };
  }, [articleRef, commentView, lineKey, railRef, lines]);

  return metrics;
}

function sameCommentRailMetrics(a: Map<number, CommentRailMetric>, b: Map<number, CommentRailMetric>) {
  if (a.size !== b.size) return false;
  for (const [line, metric] of a) {
    const next = b.get(line);
    if (!next || next.top !== metric.top || next.height !== metric.height) return false;
  }
  return true;
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

function InlineProposalControls({
  proposal,
  disabled,
  iterating,
  onApply,
  onDiscard,
  onIterate,
}: {
  proposal: SectionProposal;
  disabled: boolean;
  iterating: boolean;
  onApply: (proposalId: string) => void;
  onDiscard: (proposalId: string) => void;
  onIterate: (anchor: Anchor, instruction: string) => Promise<boolean>;
}) {
  const [instruction, setInstruction] = useState("");
  const canIterate = instruction.trim().length > 0 && !disabled;

  async function iterateAgain() {
    if (!canIterate) return;
    const ok = await onIterate(proposal.anchor, instruction.trim());
    if (ok) setInstruction("");
  }

  return (
    <section className="inline-proposal-controls">
      <div className="flex items-center gap-2">
        <span className="handoff-arrow" aria-hidden><Sparkles size={14} /></span>
        <div>
          <h2 className="text-[13px] font-semibold tracking-tight">Pending proposal</h2>
          <p className="text-[11px] text-foreground-muted">{anchorLabel(proposal.anchor)}</p>
        </div>
      </div>
      {proposal.summary ? <p className="inline-proposal-summary">{proposal.summary}</p> : null}
      <label className="mt-3 block text-xs font-semibold text-foreground-muted">
        Refine
        <textarea
          className="field mt-1 min-h-20 resize-y font-sans"
          value={instruction}
          onChange={(event) => setInstruction(event.target.value)}
          disabled={disabled}
          placeholder="Ask for a narrower, clearer, or more specific version..."
        />
      </label>
      <div className="mt-3 flex flex-wrap justify-end gap-2">
        <button type="button" className="btn btn-sm" onClick={iterateAgain} disabled={!canIterate}>
          <RotateCcw size={13} /> {iterating ? "Iterating…" : "Iterate again"}
        </button>
        <button type="button" className="btn btn-ghost btn-sm btn-danger" onClick={() => onDiscard(proposal.id)} disabled={disabled}>
          <Trash2 size={13} /> Discard
        </button>
        <button type="button" className="btn btn-primary btn-sm" onClick={() => onApply(proposal.id)} disabled={disabled}>
          <CheckCircle2 size={13} /> Apply
        </button>
      </div>
    </section>
  );
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

const PlanLineContent = memo(function PlanLineContent({
  html,
  lineNumber,
  anchoredThreadId,
  codeTokens,
  onFocusThread,
}: {
  html: string;
  lineNumber: number;
  anchoredThreadId: string | undefined;
  codeTokens?: HighlightToken[];
  onFocusThread: (threadId: string) => void;
}) {
  const content = useMemo(() => codeTokens
    ? codeTokens.map((token, index) => (
      <span key={index} style={{ color: token.color, fontStyle: token.fontStyle === 1 ? "italic" : undefined, fontWeight: token.fontStyle === 2 ? 700 : undefined, textDecoration: token.fontStyle === 4 ? "underline" : undefined }}>
        {token.content}
      </span>
    ))
    : renderInlineNodes(html || "&nbsp;"), [codeTokens, html]);

  function activate(event: React.MouseEvent | React.KeyboardEvent) {
    if (!anchoredThreadId) return;
    const selection = window.getSelection();
    if (selection && !selection.isCollapsed) return;
    event.stopPropagation();
    onFocusThread(anchoredThreadId);
  }

  function onKeyDown(event: React.KeyboardEvent) {
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    activate(event);
  }

  if (anchoredThreadId) {
    const anchoredLineProps = {
      onClick: activate,
      onKeyDown,
      role: "button" as const,
      tabIndex: 0,
    };

    return (
      <div
        className="line-content"
        data-line-content={lineNumber}
        {...anchoredLineProps}
      >
        {content}
      </div>
    );
  }

  return (
    <div className="line-content" data-line-content={lineNumber}>
      {content}
    </div>
  );
});

function renderInlineNodes(html: string): React.ReactNode {
  if (typeof document === "undefined") {
    return html;
  }
  const template = document.createElement("template");
  template.innerHTML = html;
  return Array.from(template.content.childNodes).map((node, index) =>
    inlineNodeToReact(node, `inline-${index}`),
  );
}

function inlineNodeToReact(node: ChildNode, key: string): React.ReactNode {
  if (node.nodeType === Node.TEXT_NODE) {
    return node.textContent;
  }
  if (node.nodeType !== Node.ELEMENT_NODE) {
    return null;
  }

  const element = node as HTMLElement;
  const children = Array.from(element.childNodes).map((child, index) =>
    inlineNodeToReact(child, `${key}-${index}`),
  );
  switch (element.tagName.toLowerCase()) {
    case "a": {
      const href = element.getAttribute("href") ?? "";
      const title = element.getAttribute("title") ?? undefined;
      return (
        <a key={key} href={href} title={title}>
          {children}
        </a>
      );
    }
    case "br":
      return <br key={key} />;
    case "code":
      return <code key={key}>{children}</code>;
    case "del":
      return <del key={key}>{children}</del>;
    case "em":
      return <em key={key}>{children}</em>;
    case "strong":
      return <strong key={key}>{children}</strong>;
    case "span":
      return (
        <span
          key={key}
          className={element.getAttribute("class") ?? undefined}
          style={spanStyle(element.getAttribute("style"))}
        >
          {children}
        </span>
      );
    default:
      return <span key={key}>{children}</span>;
  }
}

function spanStyle(style: string | null): React.CSSProperties | undefined {
  const padding = /^padding-left:\s*(\d+)px$/i.exec(style ?? "");
  if (padding) return { paddingLeft: `${padding[1]}px` };
  const columns = /^grid-template-columns:repeat\((\d+),\s*minmax\(9rem,\s*1fr\)\);min-width:(\d+)px$/i.exec(style ?? "");
  if (columns) return {
    gridTemplateColumns: `repeat(${columns[1]}, minmax(9rem, 1fr))`,
    minWidth: `${columns[2]}px`,
  };
  return undefined;
}

function DraftBoundaryHandles({
  articleRef,
  anchor,
  onChange,
}: {
  articleRef: React.RefObject<HTMLElement>;
  anchor: Anchor | null;
  onChange: (anchor: Anchor) => void;
}) {
  const dragSide = useRef<"start" | "end" | null>(null);
  const [positions, setPositions] = useState<{
    start: BoundaryHandlePosition;
    end: BoundaryHandlePosition;
  } | null>(null);

  const updatePositions = useCallback(() => {
    const root = articleRef.current;
    if (!root || !anchor) {
      setPositions(null);
      return;
    }
    const characterAnchor = materializeCharacterAnchor(root, anchor);
    const start = boundaryHandlePosition(root, characterAnchor.startLine, characterAnchor.startChar ?? 0);
    const end = boundaryHandlePosition(root, characterAnchor.endLine, characterAnchor.endChar ?? 0);
    setPositions(start && end ? { start, end } : null);
  }, [anchor, articleRef]);
  const updatePositionsRef = useRef(updatePositions);

  useEffect(() => {
    updatePositionsRef.current = updatePositions;
  }, [updatePositions]);

  useLayoutEffect(() => {
    updatePositionsRef.current();
    if (!anchor) return;
    const update = () => updatePositionsRef.current();
    window.addEventListener("resize", update);
    window.addEventListener("scroll", update, true);
    return () => {
      window.removeEventListener("resize", update);
      window.removeEventListener("scroll", update, true);
    };
  }, [anchor]);

  useEffect(() => {
    if (!anchor) return;
    const activeAnchor = anchor;

    function onPointerMove(event: PointerEvent) {
      const root = articleRef.current;
      const side = dragSide.current;
      if (!root || !side) return;
      const point = anchorPointFromClientPosition(root, event.clientX, event.clientY);
      if (!point) return;
      event.preventDefault();
      onChange(moveAnchorBoundary(root, activeAnchor, side, point));
    }

    function onPointerUp() {
      dragSide.current = null;
    }

    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", onPointerUp);
    window.addEventListener("pointercancel", onPointerUp);
    return () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
      window.removeEventListener("pointercancel", onPointerUp);
    };
  }, [anchor, articleRef, onChange]);

  if (!anchor || !positions) return null;

  return (
    <>
      <BoundaryHandle
        side="start"
        position={positions.start}
        onPointerDown={() => {
          dragSide.current = "start";
        }}
      />
      <BoundaryHandle
        side="end"
        position={positions.end}
        onPointerDown={() => {
          dragSide.current = "end";
        }}
      />
    </>
  );
}

function BoundaryHandle({
  side,
  position,
  onPointerDown,
}: {
  side: "start" | "end";
  position: BoundaryHandlePosition;
  onPointerDown: () => void;
}) {
  return (
    <button
      type="button"
      className={`draft-boundary-handle is-${side}`}
      style={{
        left: `${position.left}px`,
        top: `${position.top}px`,
        height: `${position.height}px`,
      }}
      aria-label={side === "start" ? "Move selection start" : "Move selection end"}
      title={side === "start" ? "Move selection start" : "Move selection end"}
      onPointerDown={(event) => {
        event.preventDefault();
        event.stopPropagation();
        onPointerDown();
      }}
    >
      <span aria-hidden className="draft-boundary-grip" />
    </button>
  );
}

function draftFromSelection(selection: Selection | null): CommentDraft | null {
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return null;
  const range = selection.getRangeAt(0);
  const startLine = lineContentForNode(range.startContainer);
  const endLine = lineContentForNode(range.endContainer);
  if (!startLine || !endLine || startLine.closest(".inline-comment-composer")) return null;

  const startLineNumber = Number(startLine.dataset.lineContent);
  const endLineNumber = Number(endLine.dataset.lineContent);
  if (!Number.isInteger(startLineNumber) || !Number.isInteger(endLineNumber)) return null;

  const startChar = textOffset(startLine, range.startContainer, range.startOffset);
  const endChar = textOffset(endLine, range.endContainer, range.endOffset);
  const anchor = {
    startLine: startLineNumber,
    startChar,
    endLine: endLineNumber,
    endChar,
  };
  const quote = textForAnchorContents(startLine, endLine, anchor).trim();
  if (!quote || compareAnchorPoints(anchor.startLine, anchor.startChar, anchor.endLine, anchor.endChar) === 0) {
    return null;
  }

  return {
    anchor,
    selectedText: quote,
    body: "",
  };
}

function selectedTextForAnchorInArticle(root: HTMLElement | null, anchor: Anchor): string {
  if (!root) return "";
  const startContent = lineContent(root, anchor.startLine);
  const endContent = lineContent(root, anchor.endLine);
  if (!startContent || !endContent) return "";
  return textForAnchorContents(startContent, endContent, anchor);
}

function lineContentForNode(node: Node): HTMLElement | null {
  const element = node.nodeType === Node.ELEMENT_NODE ? (node as Element) : node.parentElement;
  return element?.closest<HTMLElement>("[data-line-content]") ?? null;
}

function textOffset(container: HTMLElement, node: Node, offset: number): number {
  const range = document.createRange();
  range.selectNodeContents(container);
  range.setEnd(node, offset);
  return range.toString().length;
}

interface BoundaryHandlePosition {
  left: number;
  top: number;
  height: number;
}

interface AnchorPoint {
  line: number;
  char: number;
}

function textForAnchorContents(startContent: HTMLElement, endContent: HTMLElement, anchor: Anchor): string {
  const root = startContent.closest<HTMLElement>("article");
  const parts: string[] = [];
  for (let line = anchor.startLine; line <= anchor.endLine; line++) {
    const content =
      line === anchor.startLine
        ? startContent
        : line === anchor.endLine
          ? endContent
          : root
            ? lineContent(root, line)
            : null;
    const text = content?.textContent ?? "";
    if (line === anchor.startLine && line === anchor.endLine) {
      parts.push(text.slice(anchor.startChar ?? 0, anchor.endChar ?? text.length));
    } else if (line === anchor.startLine) {
      parts.push(text.slice(anchor.startChar ?? 0));
    } else if (line === anchor.endLine) {
      parts.push(text.slice(0, anchor.endChar ?? text.length));
    } else {
      parts.push(text);
    }
  }
  return parts.join("\n");
}

function materializeCharacterAnchor(root: HTMLElement, anchor: Anchor): Anchor {
  const endContent = lineContent(root, anchor.endLine);
  return {
    startLine: anchor.startLine,
    startChar: anchor.startChar ?? 0,
    endLine: anchor.endLine,
    endChar: anchor.endChar ?? textLength(endContent),
  };
}

function moveAnchorBoundary(
  root: HTMLElement,
  anchor: Anchor,
  side: "start" | "end",
  point: AnchorPoint,
): Anchor {
  const current = materializeCharacterAnchor(root, anchor);
  const next =
    side === "start"
      ? { ...current, startLine: point.line, startChar: point.char }
      : { ...current, endLine: point.line, endChar: point.char };
  const order = compareAnchorPoints(next.startLine, next.startChar ?? 0, next.endLine, next.endChar ?? 0);
  if (order === 0) return current;
  if (order < 0) return next;
  return {
    startLine: next.endLine,
    startChar: next.endChar,
    endLine: next.startLine,
    endChar: next.startChar,
  };
}

function boundaryHandlePosition(
  root: HTMLElement,
  lineNumber: number,
  char: number,
): BoundaryHandlePosition | null {
  const content = lineContent(root, lineNumber);
  if (!content) return null;

  const range = document.createRange();
  setBoundary(range, "start", content, clamp(char, 0, textLength(content)));
  range.collapse(true);

  const rect = range.getClientRects()[0];
  const contentRect = content.getBoundingClientRect();
  const rootRect = root.getBoundingClientRect();
  const fallbackLeft = char > 0 ? contentRect.right : contentRect.left;

  return {
    left: (rect?.left ?? fallbackLeft) - rootRect.left,
    top: (rect?.top ?? contentRect.top) - rootRect.top,
    height: rect?.height || contentRect.height || 22,
  };
}

function anchorPointFromClientPosition(
  root: HTMLElement,
  clientX: number,
  clientY: number,
): AnchorPoint | null {
  const directPoint = caretPointFromClientPosition(clientX, clientY);
  const directContent = directPoint ? lineContentForNode(directPoint.node) : null;
  if (directPoint && directContent && root.contains(directContent)) {
    return anchorPointForContent(directContent, directPoint.node, directPoint.offset);
  }

  const fallbackContent = lineContentFromClientPosition(root, clientX, clientY);
  if (!fallbackContent) return null;
  const rect = fallbackContent.getBoundingClientRect();
  const clampedX = clamp(clientX, rect.left + 1, Math.max(rect.left + 1, rect.right - 1));
  const clampedY = clamp(clientY, rect.top + 1, Math.max(rect.top + 1, rect.bottom - 1));
  const fallbackPoint = caretPointFromClientPosition(clampedX, clampedY);
  const fallbackPointContent = fallbackPoint ? lineContentForNode(fallbackPoint.node) : null;
  if (fallbackPoint && fallbackPointContent === fallbackContent) {
    return anchorPointForContent(fallbackContent, fallbackPoint.node, fallbackPoint.offset);
  }

  return {
    line: Number(fallbackContent.dataset.lineContent),
    char: clientX <= rect.left ? 0 : textLength(fallbackContent),
  };
}

function anchorPointForContent(content: HTMLElement, node: Node, offset: number): AnchorPoint | null {
  const line = Number(content.dataset.lineContent);
  if (!Number.isInteger(line)) return null;
  return {
    line,
    char: clamp(textOffset(content, node, offset), 0, textLength(content)),
  };
}

function lineContentFromClientPosition(
  root: HTMLElement,
  clientX: number,
  clientY: number,
): HTMLElement | null {
  const element = document.elementFromPoint(clientX, clientY);
  if (element && root.contains(element)) {
    const direct = element.closest<HTMLElement>("[data-line-content]");
    if (direct) return direct;
    const row = element.closest<HTMLElement>("[data-line]");
    const rowContent = row?.querySelector<HTMLElement>("[data-line-content]");
    if (rowContent) return rowContent;
  }

  let closest: HTMLElement | null = null;
  let closestDistance = Number.POSITIVE_INFINITY;
  for (const row of root.querySelectorAll<HTMLElement>("[data-line]")) {
    const rect = row.getBoundingClientRect();
    const distance =
      clientY < rect.top ? rect.top - clientY : clientY > rect.bottom ? clientY - rect.bottom : 0;
    if (distance < closestDistance) {
      closestDistance = distance;
      closest = row.querySelector<HTMLElement>("[data-line-content]");
    }
  }
  return closest;
}

function caretPointFromClientPosition(clientX: number, clientY: number): { node: Node; offset: number } | null {
  const caretDocument = document as Document & {
    caretPositionFromPoint?: (x: number, y: number) => { offsetNode: Node; offset: number } | null;
    caretRangeFromPoint?: (x: number, y: number) => Range | null;
  };
  const position = caretDocument.caretPositionFromPoint?.(clientX, clientY);
  if (position) {
    return { node: position.offsetNode, offset: position.offset };
  }
  const range = caretDocument.caretRangeFromPoint?.(clientX, clientY);
  if (range) {
    return { node: range.startContainer, offset: range.startOffset };
  }
  return null;
}

function textLength(content: HTMLElement | null): number {
  return content?.textContent?.length ?? 0;
}

function compareAnchorPoints(startLine: number, startChar: number, endLine: number, endChar: number): number {
  if (startLine !== endLine) return startLine - endLine;
  return startChar - endChar;
}

function clamp(value: number, min: number, max: number): number {
  if (max < min) return min;
  return Math.min(max, Math.max(min, value));
}

function usePlanHighlights(
  articleRef: React.RefObject<HTMLElement>,
  threads: Thread[],
  hoveredAnchor: Anchor | null,
  draftAnchor: Anchor | null,
) {
  useEffect(() => {
    const root = articleRef.current;
    const highlights = (CSS as typeof CSS & { highlights?: HighlightRegistry }).highlights;
    const HighlightClass = (window as Window & { Highlight?: HighlightConstructor }).Highlight;
    if (!root || !highlights || !HighlightClass) return;
    ensureHighlightStyles();

    const threadRanges = threads
      .filter((thread) => thread.status === "open")
      .flatMap((thread) => rangesForAnchor(root, thread.anchor));
    const draftRanges = draftAnchor ? rangesForAnchor(root, draftAnchor) : [];
    const hoverRanges = hoveredAnchor ? rangesForAnchor(root, hoveredAnchor) : [];

    highlights.set("planmaxx-comment-anchor", new HighlightClass(...threadRanges));
    highlights.set("planmaxx-draft-anchor", new HighlightClass(...draftRanges));
    highlights.set("planmaxx-hover-anchor", new HighlightClass(...hoverRanges));

    return () => {
      highlights.delete("planmaxx-comment-anchor");
      highlights.delete("planmaxx-draft-anchor");
      highlights.delete("planmaxx-hover-anchor");
    };
  }, [articleRef, threads, hoveredAnchor, draftAnchor]);
}

function ensureHighlightStyles() {
  if (document.getElementById("planmaxx-highlight-styles")) return;
  const style = document.createElement("style");
  style.id = "planmaxx-highlight-styles";
  style.textContent = `
    ::highlight(planmaxx-comment-anchor) {
      background: color-mix(in srgb, var(--color-accent) 16%, transparent);
    }
    ::highlight(planmaxx-draft-anchor) {
      background: color-mix(in srgb, var(--color-warning) 32%, transparent);
    }
    ::highlight(planmaxx-hover-anchor) {
      background: color-mix(in srgb, var(--color-accent) 28%, transparent);
    }
  `;
  document.head.appendChild(style);
}

function rangesForAnchor(root: HTMLElement, anchor: Anchor): Range[] {
  const ranges: Range[] = [];
  const startChar = anchor.startChar ?? 0;
  const endChar = anchor.endChar ?? 0;
  const hasCharRange = startChar !== 0 || endChar !== 0;

  if (hasCharRange) {
    const start = lineContent(root, anchor.startLine);
    const end = lineContent(root, anchor.endLine);
    if (!start || !end) return ranges;
    const range = document.createRange();
    setBoundary(range, "start", start, startChar);
    setBoundary(range, "end", end, endChar);
    ranges.push(range);
    return ranges;
  }

  for (let line = anchor.startLine; line <= anchor.endLine; line++) {
    const content = lineContent(root, line);
    if (!content) continue;
    const range = document.createRange();
    range.selectNodeContents(content);
    ranges.push(range);
  }
  return ranges;
}

function lineContent(root: HTMLElement, lineNumber: number): HTMLElement | null {
  return root.querySelector<HTMLElement>(`[data-line-content="${lineNumber}"]`);
}

function setBoundary(
  range: Range,
  side: "start" | "end",
  container: HTMLElement,
  offset: number,
) {
  let remaining = Math.max(0, offset);
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
  let node = walker.nextNode();
  while (node) {
    const length = node.textContent?.length ?? 0;
    if (remaining <= length) {
      if (side === "start") range.setStart(node, remaining);
      else range.setEnd(node, remaining);
      return;
    }
    remaining -= length;
    node = walker.nextNode();
  }
  if (side === "start") range.setStart(container, container.childNodes.length);
  else range.setEnd(container, container.childNodes.length);
}

interface HighlightRegistry {
  set: (name: string, highlight: unknown) => void;
  delete: (name: string) => void;
}

type HighlightConstructor = new (...ranges: Range[]) => unknown;
