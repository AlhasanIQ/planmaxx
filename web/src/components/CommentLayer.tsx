import { useEffect, useLayoutEffect, useState } from "react";
import type React from "react";
import type { SideAnswer, Thread, ThreadIntent } from "../types";
import { ThreadCard } from "./ThreadCard";

export type CommentView = "inline" | "alongside";

interface CommentRailMetric {
  top: number;
  height: number;
}

export function CommentThreadStack({
  threads, sideAnswersByThread, focusedThreadId, reviewTargetThreadId, onHover, onSetIntent, onReply, onDelete, onEdit,
  onCreateFollowUp, onAskSide, onIterate, onInclude, onKeepPrivate, agentActions, disabled, sideQuestionsEnabled,
  placement, anchorLine, top, hidden, historyOpen,
}: {
  threads: Thread[];
  sideAnswersByThread: Map<string, SideAnswer[]>;
  focusedThreadId: string | null;
  reviewTargetThreadId?: string;
  onHover: (threadId: string | null) => void;
  onSetIntent: (threadId: string, intent: ThreadIntent) => void | Promise<void>;
  onReply: (threadId: string) => void;
  onDelete: (threadId: string) => void;
  onEdit: (threadId: string) => void;
  onCreateFollowUp: (threadId: string) => void;
  onAskSide: (thread: Thread) => void;
  onIterate: (thread: Thread) => void | Promise<void>;
  onInclude: (answerId: string) => void;
  onKeepPrivate: (answerId: string) => void;
  agentActions: Record<string, "asking" | "iterating">;
  disabled: boolean;
  sideQuestionsEnabled: boolean;
  placement: CommentView;
  anchorLine?: number;
  top?: number;
  hidden?: boolean;
  historyOpen?: boolean;
}) {
	const [historyExpanded, setHistoryExpanded] = useState(false);
	useEffect(() => {
	  if (historyOpen) setHistoryExpanded(true);
	}, [historyOpen]);
  const activeThreads = threads.filter((thread) => thread.bucket === "active");
  const attentionThreads = threads.filter((thread) => thread.bucket === "attention");
  const historicalThreads = threads.filter((thread) => thread.bucket === "history");
  const renderThread = (thread: Thread) => (
    <ThreadCard
      key={thread.id} thread={thread}
      sideAnswers={sideAnswersByThread.get(thread.id) ?? []} isFocused={focusedThreadId === thread.id} isReviewTarget={reviewTargetThreadId === thread.id}
      onHover={onHover} onSetIntent={onSetIntent} onReply={onReply} onDelete={onDelete} onEdit={onEdit} onCreateFollowUp={onCreateFollowUp}
      onAskSide={onAskSide} onIterate={onIterate} onInclude={onInclude} onKeepPrivate={onKeepPrivate}
      agentAction={agentActions[thread.id]} disabled={disabled} sideQuestionsEnabled={sideQuestionsEnabled}
      presentation={placement === "inline" ? "inline" : "rail"}
    />
  );
  return (
    <section
      className={`plan-thread-stack is-${placement}`} aria-label="Comments" data-anchor-line={anchorLine}
      style={placement === "alongside" ? { top, visibility: hidden ? "hidden" : undefined } : undefined}
    >
      {placement === "inline" && activeThreads.length > 0 ? (
        <div className="inline-thread-stack-label">{activeThreads.length === 1 ? "Comment" : `${activeThreads.length} comments`}</div>
      ) : null}
      {activeThreads.map(renderThread)}
      {attentionThreads.length > 0 ? (
        <div className="attention-thread-group">
          <div className="attention-thread-label">Needs attention</div>
          {attentionThreads.map(renderThread)}
        </div>
      ) : null}
      {historicalThreads.length > 0 ? (
        <details className="historical-thread-group" open={historyExpanded} onToggle={(event) => setHistoryExpanded(event.currentTarget.open)}>
          <summary className="historical-thread-label">Show addressed feedback ({historicalThreads.length})</summary>
          {historicalThreads.map(renderThread)}
        </details>
      ) : null}
    </section>
  );
}

export function useCommentRailMetrics(
  articleRef: React.RefObject<HTMLElement>, railRef: React.RefObject<HTMLElement>, commentView: CommentView, lines: number[],
) {
  const [metrics, setMetrics] = useState<Map<number, CommentRailMetric>>(new Map());
  const lineKey = lines.join(",");
  useLayoutEffect(() => {
    if (commentView !== "alongside") { setMetrics(new Map()); return; }
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
          const row = article.querySelector<HTMLElement>(`.line-row[data-comment-placement="${line}"]`);
          const stack = rail.querySelector<HTMLElement>(`[data-anchor-line="${line}"]`);
          if (!row || !stack) continue;
          next.set(line, { top: Math.round(row.getBoundingClientRect().top - articleRect.top), height: Math.ceil(stack.getBoundingClientRect().height) });
        }
        setMetrics((current) => sameMetrics(current, next) ? current : next);
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
    return () => { cancelAnimationFrame(frame); observer?.disconnect(); window.removeEventListener("resize", update); };
  }, [articleRef, commentView, lineKey, railRef, lines]);
  return metrics;
}

function sameMetrics(a: Map<number, CommentRailMetric>, b: Map<number, CommentRailMetric>) {
  if (a.size !== b.size) return false;
  for (const [line, metric] of a) {
    const next = b.get(line);
    if (!next || next.top !== metric.top || next.height !== metric.height) return false;
  }
  return true;
}
