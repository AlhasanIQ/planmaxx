import { useLayoutEffect, useState } from "react";
import type React from "react";
import type { SideAnswer, Thread, ThreadKind } from "../types";
import { ThreadCard } from "./ThreadCard";

export type CommentView = "inline" | "alongside";

interface CommentRailMetric {
  top: number;
  height: number;
}

export function CommentThreadStack({
  threads, sideAnswersByThread, focusedThreadId, onHover, onSetKind, onReply, onDelete, onEdit,
  onAskSide, onIterate, onPromote, onUnpromote, agentActions, disabled, sideQuestionsEnabled,
  placement, anchorLine, top, hidden,
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
  const activeThreads = threads.filter((thread) => (thread.status ?? "open") === "open");
  const historicalThreads = threads.filter((thread) => (thread.status ?? "open") !== "open");
  const renderThread = (thread: Thread) => (
    <ThreadCard
      key={thread.id} thread={thread} kind={thread.kind ?? "decision"}
      sideAnswers={sideAnswersByThread.get(thread.id) ?? []} isFocused={focusedThreadId === thread.id}
      onHover={onHover} onSetKind={onSetKind} onReply={onReply} onDelete={onDelete} onEdit={onEdit}
      onAskSide={onAskSide} onIterate={onIterate} onPromote={onPromote} onUnpromote={onUnpromote}
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
      {historicalThreads.length > 0 ? (
        <div className="historical-thread-group">
          <div className="historical-thread-label">Earlier feedback · not sent to Codex</div>
          {historicalThreads.map(renderThread)}
        </div>
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
