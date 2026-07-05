import { useEffect, useLayoutEffect, useMemo, useRef } from "react";
import { ThreadCard } from "./ThreadCard";
import type { SideAnswer, Thread, ThreadKind } from "../types";

interface ThreadsProps {
  threads: Thread[];
  sideAnswers: SideAnswer[];
  filter: string;
  focusedThreadId: string | null;
  hoveredThreadId: string | null;
  onHover: (id: string | null) => void;
  onMove: (id: string, x: number, y: number) => void;
  onSetKind: (id: string, kind: ThreadKind) => void | Promise<void>;
  onReply: (id: string) => void;
  onDelete: (id: string) => void;
  onEdit: (id: string) => void;
  onAskSide: (t: Thread) => void;
  onIterate: (t: Thread) => void;
  onPromote: (id: string) => void;
  onUnpromote: (id: string) => void;
  askingThreadIds: Record<string, boolean>;
  sideQuestionsEnabled: boolean;
}

const MIN_HEIGHT = 240;
const GAP = 12;

export function Threads(props: ThreadsProps) {
  const {
    threads,
    sideAnswers,
    filter,
    focusedThreadId,
    onHover,
    onMove,
    onSetKind,
    onReply,
    onDelete,
    onEdit,
    onAskSide,
    onIterate,
    onPromote,
    onUnpromote,
    askingThreadIds,
    sideQuestionsEnabled,
  } = props;
  const containerRef = useRef<HTMLDivElement>(null);

  const sideAnswersByThread = useMemo(() => groupSideAnswersByThread(sideAnswers), [sideAnswers]);
  const visible = useMemo(() => {
    const normalized = filter.trim().toLowerCase();
    const filtered = normalized
      ? threads.filter((thread) =>
          searchText(thread, sideAnswersByThread.get(thread.id) ?? []).includes(normalized),
        )
      : threads;
    const focusedThread = focusedThreadId ? threads.find((thread) => thread.id === focusedThreadId) : undefined;
    return focusedThread
      ? [focusedThread, ...filtered.filter((thread) => thread.id !== focusedThread.id)]
      : filtered;
  }, [filter, focusedThreadId, sideAnswersByThread, threads]);

  useLayoutEffect(() => {
    layoutCards(containerRef.current, visible, focusedThreadId);
  }, [askingThreadIds, focusedThreadId, sideQuestionsEnabled, visible]);

  useEffect(() => {
    function onResize() {
      layoutCards(containerRef.current, visible, focusedThreadId);
    }
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, [visible, focusedThreadId]);

  if (visible.length === 0) {
    return (
      <div className="rounded-[var(--radius-card)] border border-dashed border-border-strong bg-surface-elevated/50 p-6 text-sm text-foreground-muted">
        {threads.length === 0 ? (
          <div className="space-y-2">
            <p>No comments yet.</p>
            <ul className="space-y-1 text-[13px]">
              <li>• Hover a line and click the speech-bubble icon, or drag across lines to anchor a range.</li>
              <li>• Mark each comment as a <strong className="text-foreground">decision</strong> (→ next turn) or a <strong className="text-foreground">note</strong> (stays here).</li>
              <li>• Ask Codex a <strong className="text-foreground">/btw</strong> side question; answers are ephemeral until you opt in.</li>
            </ul>
          </div>
        ) : (
          "No threads match your filter."
        )}
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      className="relative"
      style={{ minHeight: `${MIN_HEIGHT}px` }}
    >
      {visible.map((thread) => (
        <ThreadCard
          key={thread.id}
          thread={thread}
          kind={thread.kind ?? "decision"}
          sideAnswers={sideAnswersByThread.get(thread.id) ?? []}
          isFocused={focusedThreadId === thread.id}
          onHover={onHover}
          onMove={onMove}
          onSetKind={onSetKind}
          onReply={onReply}
          onDelete={onDelete}
          onEdit={onEdit}
          onAskSide={onAskSide}
          onIterate={onIterate}
          onPromote={onPromote}
          onUnpromote={onUnpromote}
          isAskingSide={Boolean(askingThreadIds[thread.id])}
          sideQuestionsEnabled={sideQuestionsEnabled}
        />
      ))}
    </div>
  );
}

function groupSideAnswersByThread(sideAnswers: SideAnswer[]): Map<string, SideAnswer[]> {
  const grouped = new Map<string, SideAnswer[]>();
  for (const answer of sideAnswers) {
    const current = grouped.get(answer.threadId);
    if (current) {
      current.push(answer);
    } else {
      grouped.set(answer.threadId, [answer]);
    }
  }
  return grouped;
}

function searchText(thread: Thread, sideAnswers: SideAnswer[]): string {
  const msgs = thread.messages.map((m) => m.body).join("\n");
  const answers: string[] = [];
  for (const answer of sideAnswers) {
    answers.push(`${answer.question}\n${answer.answer}`);
  }
  const ans = answers.join("\n");
  return `${msgs}\n${ans}`.toLowerCase();
}

function layoutCards(container: HTMLDivElement | null, threads: Thread[], focusedThreadId: string | null) {
  if (!container) return;
  const cards = Array.from(container.querySelectorAll<HTMLElement>("[data-thread-id]"));
  if (cards.length === 0) {
    container.style.minHeight = "";
    return;
  }
  const width = container.clientWidth;
  const threadById = new Map(threads.map((thread) => [thread.id, thread]));
  cards.forEach((c) => {
    c.style.width = `${Math.min(320, width)}px`;
  });
  const positioned = cards
    .map((card, index) => {
      const id = card.dataset.threadId!;
      const t = threadById.get(id);
      const x = Number.isFinite(t?.position.x) ? t!.position.x : 0;
      const y = Number.isFinite(t?.position.y) ? t!.position.y : 120 + index * 96;
      return { card, x, y };
    })
    .sort((a, b) => {
      if (a.card.dataset.threadId === focusedThreadId) return -1;
      if (b.card.dataset.threadId === focusedThreadId) return 1;
      return a.y - b.y;
    });

  let nextTop = 0;
  for (const { card, x, y } of positioned) {
    const maxLeft = Math.max(0, width - card.offsetWidth);
    const left = Math.min(Math.max(x, 0), maxLeft);
    const layoutY = card.dataset.threadId === focusedThreadId ? 0 : y;
    const top = Math.max(0, layoutY, nextTop);
    Object.assign(card.style, {
      left: `${left}px`,
      top: `${top}px`,
    });
    nextTop = top + card.offsetHeight + GAP;
  }
  container.style.minHeight = `${Math.max(MIN_HEIGHT, nextTop)}px`;
}
