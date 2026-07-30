import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, Code2, Columns2, Eye, GitCompareArrows, ListTree, Loader2, MessageSquarePlus, Search, Sparkles } from "lucide-react";
import { renderPlanLines, renderSourceLines } from "../lib/markdown";
import { htmlPreviewAnnotation, htmlPreviewDocument, sourceOffsetsForAnchor, type HTMLPreviewAnnotation } from "../lib/htmlPreview";
import { positionHTMLPreviewComments } from "../lib/htmlPreviewLayout";
import type { Anchor, ChangeView, DocumentSnapshot, PendingProposalSummary, PlanFormat, ReviewStop, RevisionComparison, RevisionFeedback, SideAnswer, Thread, ThreadIntent } from "../types";
import { anchorLabel, anchorTouchesLine } from "../lib/anchors";
import { inlineCommentComposerPlacement } from "../lib/commentPlacement";
import { comparisonGutterValues, comparisonLineIdentity } from "../lib/comparisonLines";
import { highlightCodeBlocks, highlightHTMLSource, type HighlightToken } from "../lib/codeHighlight";
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
  proposalRefineDisabled: boolean;
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

interface HTMLPreviewRect {
  left: number;
  top: number;
  right: number;
  bottom: number;
  width: number;
  height: number;
}

interface HTMLPreviewTargetLayout {
  id: string;
  target: HTMLPreviewRect | null;
  slot: HTMLPreviewRect | null;
}

interface HTMLPreviewLayout {
  width: number;
  height: number;
  targets: HTMLPreviewTargetLayout[];
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
  proposalRefineDisabled,
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
  const htmlPreviewRef = useRef<HTMLIFrameElement>(null);
  const htmlPreviewShellRef = useRef<HTMLDivElement>(null);
  const htmlPositionRef = useRef<{ line: number; offset: number } | null>(null);
  const pendingSourceRestoreRef = useRef(false);
  const pendingPreviewRestoreLineRef = useRef<number | null>(null);
  const [htmlPreviewReady, setHTMLPreviewReady] = useState(0);
  const [htmlPreviewLayout, setHTMLPreviewLayout] = useState<HTMLPreviewLayout>({ width: 0, height: 0, targets: [] });
  const [htmlPreviewOverlayHeights, setHTMLPreviewOverlayHeights] = useState<Record<string, number>>({});
  const [htmlPreviewRailOffset, setHTMLPreviewRailOffset] = useState(0);
  const renderLines = planFormat === "html" ? renderSourceLines : renderPlanLines;
  const lines = useMemo(() => renderLines(plan), [plan, renderLines]);
  const highlightedCode = useHighlightedCode(plan, planFormat, theme);
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
  const retargetDraft = useCallback(({ anchor, selectedText }: Pick<CommentDraft, "anchor" | "selectedText">) => {
    setDraft((current) =>
      current
        ? { ...current, anchor, selectedText }
        : { anchor, selectedText, body: "", intent: "instruction" },
    );
  }, []);
  const [htmlView, setHTMLView] = useState<"preview" | "source">("preview");
  const showHTMLPreview = planFormat === "html" && htmlView === "preview" && !proposal && !comparison;
  const sideAnswersByThread = useMemo(() => groupSideAnswersByThread(sideAnswers), [sideAnswers]);
  const displayedThreads = useMemo(
    () => visibleThreads(threads, sideAnswers, commentFilter, focusedThreadId),
    [commentFilter, focusedThreadId, sideAnswers, threads],
  );
	const activeDisplayedThreads = useMemo(() => displayedThreads.filter((thread) => thread.bucket === "active"), [displayedThreads]);
  const activeDisplayedThreadIDs = useMemo(() => new Set(activeDisplayedThreads.map((thread) => thread.id)), [activeDisplayedThreads]);
  const htmlPreviewLayoutByID = useMemo(
    () => new Map(htmlPreviewLayout.targets.map((target) => [target.id, target])),
    [htmlPreviewLayout.targets],
  );
  const htmlAlongsidePositions = useMemo(
    () => positionHTMLPreviewComments(
      activeDisplayedThreads.map((thread) => {
        const target = htmlPreviewLayoutByID.get(thread.id)?.target;
        return {
          id: thread.id,
          targetTop: target && target.bottom > 0 && target.top < htmlPreviewLayout.height ? target.top : undefined,
          height: htmlPreviewOverlayHeights[thread.id] ?? 190,
        };
      }),
      htmlPreviewLayout.height,
    ),
    [activeDisplayedThreads, htmlPreviewLayout.height, htmlPreviewLayoutByID, htmlPreviewOverlayHeights],
  );
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
  const commentRailMetrics = useCommentRailMetrics(
    articleRef,
    commentRailRef,
    commentView,
    commentRailLines,
    `${planFormat}:${htmlView}:${Boolean(activeChange)}`,
  );

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
    if (disabled || submittingDraft) return;
    const anchor = { startLine: lineNumber, endLine: lineNumber };
    retargetDraft({
      anchor,
      selectedText: selectedTextForAnchorInArticle(articleRef.current, anchor),
    });
  }

  function handlePointerUp(e: React.PointerEvent<HTMLElement>) {
    if (disabled || submittingDraft) return;
    if ((e.target as HTMLElement | null)?.closest(".inline-comment-composer")) return;
    if ((e.target as HTMLElement | null)?.closest(".draft-boundary-handle")) return;
    const selection = window.getSelection();
    const selectionDraft = draftFromSelection(selection);
    if (!selectionDraft) return;
    retargetDraft(selectionDraft);
    requestAnimationFrame(() => restoreNativeSelection(articleRef.current, selectionDraft.anchor));
  }

  function currentSelectedText(current: CommentDraft): string {
    if (showHTMLPreview) return current.selectedText;
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
    if (showHTMLPreview) {
      postHTMLPreviewScroll({ startLine: item.line, endLine: item.line });
      return;
    }
    window.requestAnimationFrame(() => window.requestAnimationFrame(() => {
      const target = document.querySelector<HTMLElement>(`[data-document-line="${item.line}"]`);
      target?.scrollIntoView({
        block: "start",
        behavior: reviewScrollBehavior(Boolean(window.matchMedia?.("(prefers-reduced-motion: reduce)").matches)),
      });
    }));
  }

  useEffect(() => {
    if (!showHTMLPreview) return;
    const article = articleRef.current;
    const shell = htmlPreviewShellRef.current;
    if (!article || !shell) return;
    let frame = 0;
    const measure = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const articleRect = article.getBoundingClientRect();
        const shellRect = shell.getBoundingClientRect();
        setHTMLPreviewRailOffset(Math.max(0, Math.round(shellRect.top - articleRect.top)));
      });
    };
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    observer?.observe(article);
    observer?.observe(shell);
    window.addEventListener("resize", measure);
    measure();
    return () => {
      cancelAnimationFrame(frame);
      observer?.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [showHTMLPreview]);

  const htmlPreviewAnnotations = useMemo(() => {
    const annotations: HTMLPreviewAnnotation[] = [];
    for (const thread of threads) {
      if (thread.lifecycle !== "active") continue;
      const annotation = htmlPreviewAnnotation(plan, thread.anchor, thread.id);
      if (!annotation) continue;
      annotations.push({
        ...annotation,
        active: thread.id === hoveredThreadId || thread.id === focusedThreadId,
        inlineSlot: commentView === "inline" && activeDisplayedThreadIDs.has(thread.id),
        slotHeight: htmlPreviewOverlayHeights[thread.id] ?? 260,
      });
    }
    if (draft) {
      const annotation = htmlPreviewAnnotation(plan, draft.anchor, "draft");
      if (annotation) annotations.push({
        ...annotation,
        active: true,
        draft: true,
        inlineSlot: true,
        slotHeight: htmlPreviewOverlayHeights.draft ?? 330,
      });
    }
    return annotations;
  }, [
    activeDisplayedThreadIDs,
    commentView,
    draft,
    focusedThreadId,
    hoveredThreadId,
    htmlPreviewOverlayHeights,
    plan,
    threads,
  ]);

  const postHTMLPreviewAnnotations = useCallback(() => {
    htmlPreviewRef.current?.contentWindow?.postMessage({
      type: "planmaxx:preview-annotations",
      annotations: htmlPreviewAnnotations,
      disabled,
    }, "*");
  }, [disabled, htmlPreviewAnnotations]);

  const postHTMLPreviewScroll = useCallback((anchor: Anchor, behavior?: "auto" | "smooth") => {
    const range = sourceOffsetsForAnchor(plan, anchor);
    htmlPreviewRef.current?.contentWindow?.postMessage({
      type: "planmaxx:preview-scroll",
      ...range,
      behavior: behavior ?? reviewScrollBehavior(Boolean(window.matchMedia?.("(prefers-reduced-motion: reduce)").matches)),
    }, "*");
  }, [plan]);

  const postHTMLPreviewPing = useCallback((threadId: string) => {
    htmlPreviewRef.current?.contentWindow?.postMessage({
      type: "planmaxx:preview-ping",
      id: threadId,
    }, "*");
  }, []);

  const postHTMLPreviewScrollBy = useCallback((top: number, left = 0) => {
    htmlPreviewRef.current?.contentWindow?.postMessage({
      type: "planmaxx:preview-scroll-by",
      top,
      left,
    }, "*");
  }, []);

  const measureHTMLPreviewOverlay = useCallback((id: string, height: number) => {
    const rounded = Math.max(1, Math.ceil(height));
    setHTMLPreviewOverlayHeights((current) =>
      current[id] === rounded ? current : { ...current, [id]: rounded },
    );
  }, []);

  const pulseThreadCard = useCallback((threadId: string) => {
    window.requestAnimationFrame(() => window.requestAnimationFrame(() => {
      const escaped = typeof CSS !== "undefined" && CSS.escape ? CSS.escape(threadId) : threadId.replace(/["\\]/g, "\\$&");
      for (const card of document.querySelectorAll<HTMLElement>(`[data-thread-id="${escaped}"]`)) {
        card.classList.remove("is-link-ping");
        void card.offsetWidth;
        card.classList.add("is-link-ping");
        window.setTimeout(() => card.classList.remove("is-link-ping"), 760);
      }
    }));
  }, []);

  const activateHTMLPreviewThread = useCallback((threadId: string, origin: "card" | "preview") => {
    onFocusThread(threadId);
    pulseThreadCard(threadId);
    const thread = threads.find((candidate) => candidate.id === threadId);
    if (origin === "card" && thread?.lifecycle === "active" && showHTMLPreview) {
      postHTMLPreviewScroll(thread.anchor);
      postHTMLPreviewPing(threadId);
    } else if (origin === "card" && thread?.lifecycle === "active") {
      window.requestAnimationFrame(() => {
        const target = articleRef.current?.querySelector<HTMLElement>(
          `[data-document-line="${thread.anchor.startLine}"]`,
        );
        target?.scrollIntoView({
          block: "center",
          behavior: reviewScrollBehavior(Boolean(window.matchMedia?.("(prefers-reduced-motion: reduce)").matches)),
        });
        if (target) {
          target.classList.remove("is-link-ping");
          void target.offsetWidth;
          target.classList.add("is-link-ping");
          window.setTimeout(() => target.classList.remove("is-link-ping"), 760);
        }
      });
    }
  }, [onFocusThread, postHTMLPreviewPing, postHTMLPreviewScroll, pulseThreadCard, showHTMLPreview, threads]);

  const captureHTMLSourcePosition = useCallback(() => {
    const line = sourceLineAtViewport(articleRef.current);
    if (!line) return;
    htmlPositionRef.current = {
      line,
      offset: sourceOffsetsForAnchor(plan, { startLine: line, endLine: line }).start,
    };
  }, [plan]);

  function changeHTMLView(next: "preview" | "source") {
    if (next === htmlView) return;
    if (next === "preview") {
      captureHTMLSourcePosition();
      pendingPreviewRestoreLineRef.current = htmlPositionRef.current?.line ?? null;
    } else {
      pendingSourceRestoreRef.current = true;
    }
    setHTMLView(next);
  }

  useEffect(() => {
    if (planFormat !== "html" || htmlView !== "source" || activeChange || !pendingSourceRestoreRef.current) return;
    const line = htmlPositionRef.current?.line;
    if (!line) {
      pendingSourceRestoreRef.current = false;
      return;
    }
    window.requestAnimationFrame(() => window.requestAnimationFrame(() => {
      const target = articleRef.current?.querySelector<HTMLElement>(`[data-document-line="${line}"]`);
      if (target) {
        const top = window.scrollY + target.getBoundingClientRect().top - 72;
        window.scrollTo({ top: Math.max(0, top), behavior: "auto" });
      }
      pendingSourceRestoreRef.current = false;
      captureHTMLSourcePosition();
    }));
  }, [activeChange, captureHTMLSourcePosition, htmlView, planFormat]);

  useEffect(() => {
    if (planFormat !== "html" || htmlView !== "source" || activeChange) return;
    let frame = 0;
    const capture = () => {
      frame = 0;
      if (!pendingSourceRestoreRef.current) captureHTMLSourcePosition();
    };
    const schedule = () => {
      if (!frame) frame = window.requestAnimationFrame(capture);
    };
    window.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    schedule();
    return () => {
      window.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      if (frame) window.cancelAnimationFrame(frame);
    };
  }, [activeChange, captureHTMLSourcePosition, htmlView, planFormat]);

  useEffect(() => {
    if (!showHTMLPreview || htmlPreviewReady === 0) return;
    postHTMLPreviewAnnotations();
  }, [htmlPreviewReady, postHTMLPreviewAnnotations, showHTMLPreview]);

  useEffect(() => {
    if (!showHTMLPreview || htmlPreviewReady === 0) return;
    const line = pendingPreviewRestoreLineRef.current;
    if (!line) return;
    let secondFrame = 0;
    const firstFrame = window.requestAnimationFrame(() => {
      secondFrame = window.requestAnimationFrame(() => {
        pendingPreviewRestoreLineRef.current = null;
        postHTMLPreviewScroll({ startLine: line, endLine: line }, "auto");
      });
    });
    return () => {
      window.cancelAnimationFrame(firstFrame);
      if (secondFrame) window.cancelAnimationFrame(secondFrame);
    };
  }, [htmlPreviewReady, postHTMLPreviewScroll, showHTMLPreview]);

  useEffect(() => {
    if (!showHTMLPreview || htmlPreviewReady === 0 || !focusedAnchor) return;
    postHTMLPreviewScroll(focusedAnchor);
  }, [focusedAnchor, htmlPreviewReady, postHTMLPreviewScroll, showHTMLPreview]);

  const handleHTMLPreviewReady = useCallback(() => {
    setHTMLPreviewReady((value) => value + 1);
    const line = pendingPreviewRestoreLineRef.current ?? htmlPositionRef.current?.line;
    if (line) postHTMLPreviewScroll({ startLine: line, endLine: line }, "auto");
  }, [postHTMLPreviewScroll]);

  const handleHTMLPreviewPosition = useCallback((position: { line: number; offset: number }) => {
    if (!showHTMLPreview) return;
    if (pendingPreviewRestoreLineRef.current !== null) return;
    pendingPreviewRestoreLineRef.current = null;
    htmlPositionRef.current = position;
  }, [showHTMLPreview]);

  const handleHTMLPreviewSelection = useCallback((selection: { anchor: Anchor; selectedText: string }) => {
    if (disabled || submittingDraft) return;
    retargetDraft(selection);
  }, [disabled, retargetDraft, submittingDraft]);

  const handleHTMLPreviewLayout = useCallback((layout: HTMLPreviewLayout) => {
    setHTMLPreviewLayout(layout);
  }, []);

  return (
    <div className={`plan-with-comment-rail is-${commentView === "alongside" && (!showHTMLPreview || activeDisplayedThreads.length > 0) ? "alongside" : "inline"}${showHTMLPreview ? " has-html-preview" : ""}${activeChange ? " has-review-navigation" : ""}`}>
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
                onClick={() => changeHTMLView("preview")}
                aria-pressed={htmlView === "preview"}
                disabled={Boolean(proposal || comparison)}
                title={proposal || comparison ? "Preview is unavailable while showing source changes" : "Render the HTML plan safely"}
              >
                <Eye size={13} /> Preview
              </button>
              <button
                type="button"
                className={`btn btn-sm${htmlView === "source" || proposal || comparison ? " btn-primary" : " btn-ghost"}`}
                onClick={() => changeHTMLView("source")}
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
	      refineDisabled={proposalRefineDisabled}
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
                        onActivate={(threadId) => activateHTMLPreviewThread(threadId, "card")}
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
          Select rendered text or click an element to review it. The plan stays network-blocked in an isolated sandbox; proposed changes open in Source.
        </div>
      ) : null}
      <div className="plan-body py-2">
		        {planFormat === "html" && !activeChange ? (
		          <div hidden={!showHTMLPreview}>
                <div ref={htmlPreviewShellRef} className="html-preview-frame-shell">
	              <HTMLPlanPreview
	                frameRef={htmlPreviewRef}
	                source={plan}
	                theme={theme}
	                onReady={handleHTMLPreviewReady}
	                onPosition={handleHTMLPreviewPosition}
	                onSelection={handleHTMLPreviewSelection}
	                onFocusThread={(threadId) => activateHTMLPreviewThread(threadId, "preview")}
                  onLayout={handleHTMLPreviewLayout}
	              />
                  <div className="html-preview-overlay-layer" aria-label="Comments in rendered HTML">
                    {commentView === "inline" ? activeDisplayedThreads.map((thread) => (
                      <HTMLPreviewAnchoredOverlay
                        key={thread.id}
                        id={thread.id}
                        rect={htmlPreviewLayoutByID.get(thread.id)?.slot ?? null}
                        onMeasure={measureHTMLPreviewOverlay}
                        onScrollBy={postHTMLPreviewScrollBy}
                      >
                        <CommentThreadStack
                          threads={[thread]}
                          sideAnswersByThread={sideAnswersByThread}
                          focusedThreadId={focusedThreadId}
                          onHover={onHoverThread}
                          onActivate={(threadId) => activateHTMLPreviewThread(threadId, "card")}
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
                      </HTMLPreviewAnchoredOverlay>
                    )) : null}
                    {draft ? (
                      <HTMLPreviewAnchoredOverlay
                        id="draft"
                        rect={htmlPreviewLayoutByID.get("draft")?.slot ?? null}
                        onMeasure={measureHTMLPreviewOverlay}
                        onScrollBy={postHTMLPreviewScrollBy}
                      >
                        <InlineCommentComposer
                          draft={draft}
                          spacerLines={0}
                          submitting={submittingDraft}
                          agentAction={draftAgentAction}
                          disabled={disabled}
                          agentActionsEnabled={sideQuestionsEnabled}
                          setDraft={setDraft}
                          onCancel={cancelDraft}
                          onSubmit={submitDraft}
                          onAskSide={askSideFromDraft}
                          onIterate={iterateDraft}
                        />
                      </HTMLPreviewAnchoredOverlay>
                    ) : null}
                  </div>
                </div>
		          </div>
		        ) : null}
	        {!showHTMLPreview ? displayRows.map((row, idx) => {
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
                          if (anchoredThreadId) activateHTMLPreviewThread(anchoredThreadId, "preview");
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
                    sourceIndent={planFormat === "html" ? line.sourceIndent : undefined}
                    anchoredThreadId={anchoredThreadId}
                    codeTokens={isProposedLine || lineNumber === undefined ? undefined : highlightedCode.get(lineNumber)}
                    onFocusThread={(threadId) => activateHTMLPreviewThread(threadId, "preview")}
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
                    agentActionsEnabled={sideQuestionsEnabled}
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
                    refineDisabled={proposalRefineDisabled}
                    iterating={proposalIterating}
                    onApply={onApplyProposal}
                    onDiscard={onDiscardProposal}
                    onIterate={onIterateProposal}
                  />
                ) : null}
              </div>
            </div>
          );
	        }) : null}
      </div>
      {!showHTMLPreview ? (
        <DraftBoundaryHandles
          articleRef={articleRef}
          anchor={draft?.anchor ?? null}
          onChange={updateDraftAnchor}
        />
      ) : null}
    </article>
    {commentView === "alongside" && showHTMLPreview && activeDisplayedThreads.length > 0 ? (
      <aside
        className="plan-comment-rail html-preview-comment-rail"
        aria-label="Comments alongside rendered HTML"
        style={{ marginTop: htmlPreviewRailOffset }}
      >
        {activeDisplayedThreads.map((thread) => (
          <HTMLPreviewRailComment
            key={thread.id}
            id={thread.id}
            top={htmlAlongsidePositions.get(thread.id)}
            targetTop={htmlPreviewLayoutByID.get(thread.id)?.target?.top}
            visible={htmlAlongsidePositions.has(thread.id)}
            onMeasure={measureHTMLPreviewOverlay}
            onScrollBy={postHTMLPreviewScrollBy}
          >
            <CommentThreadStack
              threads={[thread]}
              sideAnswersByThread={sideAnswersByThread}
              focusedThreadId={focusedThreadId}
              onHover={onHoverThread}
              onActivate={(threadId) => activateHTMLPreviewThread(threadId, "card")}
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
            />
          </HTMLPreviewRailComment>
        ))}
      </aside>
    ) : commentView === "alongside" ? (
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
              onActivate={(threadId) => activateHTMLPreviewThread(threadId, "card")}
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

function sourceLineAtViewport(article: HTMLElement | null): number | null {
  if (!article) return null;
  const threshold = 72;
  const rows = [...article.querySelectorAll<HTMLElement>("[data-document-line]")];
  const visible = rows.find((row) => {
    const rect = row.getBoundingClientRect();
    return rect.bottom > threshold && rect.top < window.innerHeight;
  });
  const line = Number(visible?.dataset.documentLine);
  return Number.isInteger(line) && line > 0 ? line : null;
}

export const Plan = ReviewDocument;

function useMeasuredHTMLPreviewComment<T extends HTMLElement>(
  id: string,
  onMeasure: (id: string, height: number) => void,
  onScrollBy?: (top: number, left?: number) => void,
) {
  const ref = useRef<T>(null);
  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    const measure = () => onMeasure(id, element.getBoundingClientRect().height);
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    observer?.observe(element);
    measure();
    return () => observer?.disconnect();
  }, [id, onMeasure]);
  useEffect(() => {
    const element = ref.current;
    if (!element || !onScrollBy) return;
    const forwardWheel = (event: WheelEvent) => {
      const scrollable = (event.target instanceof Element ? event.target : null)?.closest<HTMLElement>(
        "textarea,[data-html-preview-card-scroll]",
      );
      if (scrollable) {
        const canScrollUp = event.deltaY < 0 && scrollable.scrollTop > 0;
        const canScrollDown = event.deltaY > 0 && scrollable.scrollTop + scrollable.clientHeight < scrollable.scrollHeight;
        if (canScrollUp || canScrollDown) return;
      }
      event.preventDefault();
      event.stopPropagation();
      onScrollBy(event.deltaY, event.deltaX);
    };
    element.addEventListener("wheel", forwardWheel, { passive: false });
    return () => element.removeEventListener("wheel", forwardWheel);
  }, [onScrollBy]);
  return ref;
}

function HTMLPreviewAnchoredOverlay({
  id,
  rect,
  onMeasure,
  onScrollBy,
  children,
}: {
  id: string;
  rect: HTMLPreviewRect | null;
  onMeasure: (id: string, height: number) => void;
  onScrollBy: (top: number, left?: number) => void;
  children: React.ReactNode;
}) {
  const ref = useMeasuredHTMLPreviewComment<HTMLElement>(id, onMeasure, onScrollBy);
  return (
    <aside
      ref={ref}
      className="html-preview-anchored-overlay"
      data-html-preview-overlay={id}
      aria-label={id === "draft" ? "HTML preview comment" : undefined}
      style={rect ? {
        top: rect.top,
        left: Math.max(12, rect.left),
        width: Math.max(240, rect.width),
        visibility: "visible",
      } : { visibility: "hidden" }}
    >
      {children}
    </aside>
  );
}

function HTMLPreviewRailComment({
  id,
  top,
  targetTop,
  visible,
  onMeasure,
  onScrollBy,
  children,
}: {
  id: string;
  top: number | undefined;
  targetTop: number | undefined;
  visible: boolean;
  onMeasure: (id: string, height: number) => void;
  onScrollBy: (top: number, left?: number) => void;
  children: React.ReactNode;
}) {
  const ref = useMeasuredHTMLPreviewComment<HTMLDivElement>(id, onMeasure, onScrollBy);
  const anchorOffset = (targetTop ?? top ?? 0) - (top ?? 0);
  return (
    <div
      ref={ref}
      className={`html-preview-rail-comment${visible ? "" : " is-hidden"}`}
      data-html-preview-rail-comment={id}
      aria-hidden={!visible}
      style={{
        top,
        visibility: visible ? "visible" : "hidden",
        "--html-comment-anchor-y": `${anchorOffset}px`,
        "--html-comment-connector-top": `${Math.min(20, anchorOffset)}px`,
        "--html-comment-connector-height": `${Math.abs(anchorOffset - 20)}px`,
      } as React.CSSProperties}
    >
      <div className="html-preview-rail-card-scroll" data-html-preview-card-scroll="">
        {children}
      </div>
    </div>
  );
}

function HTMLPlanPreview({
  frameRef,
  source,
  theme,
  onReady,
  onPosition,
  onSelection,
  onFocusThread,
  onLayout,
}: {
  frameRef: React.RefObject<HTMLIFrameElement>;
  source: string;
  theme: "light" | "dark";
  onReady: () => void;
  onPosition: (position: { line: number; offset: number }) => void;
  onSelection: (selection: { anchor: Anchor; selectedText: string }) => void;
  onFocusThread: (threadId: string) => void;
  onLayout: (layout: HTMLPreviewLayout) => void;
}) {
  const srcDoc = useMemo(() => htmlPreviewDocument(source, theme), [source, theme]);
  useEffect(() => {
    function handleMessage(event: MessageEvent) {
      if (event.source !== frameRef.current?.contentWindow) return;
      const message = event.data as {
        type?: string;
        anchor?: Anchor;
        selectedText?: string;
        threadId?: string;
        line?: number;
        offset?: number;
        width?: number;
        height?: number;
        targets?: HTMLPreviewTargetLayout[];
      };
      if (message.type === "planmaxx:preview-ready") {
        onReady();
      } else if (
        message.type === "planmaxx:preview-position" &&
        Number.isInteger(message.line) &&
        Number.isFinite(message.offset)
      ) {
        onPosition({ line: message.line!, offset: message.offset! });
      } else if (
        message.type === "planmaxx:preview-selection" &&
        message.anchor &&
        typeof message.selectedText === "string"
      ) {
        onSelection({ anchor: message.anchor, selectedText: message.selectedText });
      } else if (message.type === "planmaxx:preview-focus-thread" && message.threadId) {
        onFocusThread(message.threadId);
      } else if (
        message.type === "planmaxx:preview-layout" &&
        Number.isFinite(message.width) &&
        Number.isFinite(message.height) &&
        Array.isArray(message.targets)
      ) {
        onLayout({
          width: message.width!,
          height: message.height!,
          targets: message.targets,
        });
      }
    }
    window.addEventListener("message", handleMessage);
    return () => window.removeEventListener("message", handleMessage);
  }, [frameRef, onFocusThread, onLayout, onPosition, onReady, onSelection]);

  return (
    <iframe
      ref={frameRef}
      className="html-plan-preview"
      title="Rendered HTML plan preview"
      sandbox="allow-scripts"
      srcDoc={srcDoc}
      onLoad={onReady}
    />
  );
}

function useHighlightedCode(plan: string, format: PlanFormat, theme: "light" | "dark") {
  const [highlighted, setHighlighted] = useState<Map<number, HighlightToken[]>>(new Map());

  useEffect(() => {
    let current = true;
    const highlight = format === "html"
      ? highlightHTMLSource(plan, theme)
      : highlightCodeBlocks(plan, theme);
    void highlight.then((next) => {
      if (current) setHighlighted(next);
    });
    return () => {
      current = false;
    };
  }, [format, plan, theme]);

  return highlighted;
}

function InlineCommentComposer({
  draft,
  spacerLines,
  submitting,
  agentAction,
  disabled,
  agentActionsEnabled,
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
  agentActionsEnabled: boolean;
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
    const frame = window.requestAnimationFrame(() => {
      textareaRef.current?.focus({ preventScroll: true });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [
    draft.anchor.endChar,
    draft.anchor.endLine,
    draft.anchor.startChar,
    draft.anchor.startLine,
    draft.threadId,
  ]);

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
          <span>{agentAction === "asking" ? "The agent is thinking about this /btw…" : "The agent is iterating on this selection…"}</span>
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
        {!isEditing && agentActionsEnabled ? (
          <button
            type="button"
            className="btn"
            onClick={onAskSide}
            disabled={!canSubmit || submitting || disabled}
            title="Save this comment and ask the agent about the selected text"
          >
            {agentAction === "asking" ? "Asking…" : "/btw"}
          </button>
        ) : null}
        {!isEditing && agentActionsEnabled ? (
          <button
            type="button"
            className="btn"
            onClick={onIterate}
            disabled={!canSubmit || submitting || disabled}
            title="Ask the agent to rewrite only the selected section"
          >
            <Sparkles size={13} /> {agentAction === "iterating" ? "Iterating…" : "Iterate section"}
          </button>
        ) : null}
      </div>
    </div>
  );
}
