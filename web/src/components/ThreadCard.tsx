import { useEffect, useRef, useState } from "react";
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  EyeOff,
  GripVertical,
  MessageCircleQuestion,
  Pencil,
  Reply,
  Sparkles,
  Trash2,
} from "lucide-react";
import type { SideAnswer, Thread, ThreadKind } from "../types";
import { relativeTime } from "../lib/time";
import { anchorLabel } from "../lib/anchors";

interface ThreadCardProps {
  thread: Thread;
  kind: ThreadKind;
  sideAnswers: SideAnswer[];
  isFocused: boolean;
  onHover: (id: string | null) => void;
  onMove: (id: string, x: number, y: number) => void;
  onSetKind: (id: string, kind: ThreadKind) => void;
  onReply: (id: string) => void;
  onDelete: (id: string) => void;
  onEdit: (id: string) => void;
  onAskSide: (thread: Thread) => void;
  onIterate: (thread: Thread) => void | Promise<void>;
  onPromote: (answerId: string) => void;
  onUnpromote: (answerId: string) => void;
  agentAction?: "asking" | "iterating";
  disabled: boolean;
  sideQuestionsEnabled: boolean;
}

export function ThreadCard(props: ThreadCardProps) {
  const {
    thread,
    kind,
    sideAnswers,
    isFocused,
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
    agentAction,
    disabled,
    sideQuestionsEnabled,
  } = props;
  const ref = useRef<HTMLDivElement>(null);
  const [dragging, setDragging] = useState(false);
  const dragMoveRef = useRef({
    id: thread.id,
    onMove,
    position: thread.position,
  });

  useEffect(() => {
    dragMoveRef.current = {
      id: thread.id,
      onMove,
      position: thread.position,
    };
  }, [onMove, thread.id, thread.position]);

  // HTML5 drag is finicky and easy to get wrong; we drive a simple pointer-drag.
  useEffect(() => {
    if (!dragging) return;
    const handle = ref.current;
    if (!handle) return;
    const container = handle.parentElement as HTMLElement | null;
    if (!container) return;
    const containerRect = container.getBoundingClientRect();
    const offset = {
      x: dragOffsetRef.current.x,
      y: dragOffsetRef.current.y,
    };
    const onMoveDoc = (e: PointerEvent) => {
      const { position } = dragMoveRef.current;
      const x = Math.round(e.clientX - containerRect.left - offset.x);
      const y = Math.round(e.clientY - containerRect.top - offset.y);
      handle.style.transform = `translate(${x - position.x}px, ${y - position.y}px)`;
    };
    const onUp = (e: PointerEvent) => {
      const { id, onMove: moveThread } = dragMoveRef.current;
      const x = Math.round(e.clientX - containerRect.left - offset.x);
      const y = Math.round(e.clientY - containerRect.top - offset.y);
      handle.style.transform = "";
      setDragging(false);
      moveThread(id, Math.max(0, x), Math.max(0, y));
    };
    document.addEventListener("pointermove", onMoveDoc);
    document.addEventListener("pointerup", onUp);
    return () => {
      document.removeEventListener("pointermove", onMoveDoc);
      document.removeEventListener("pointerup", onUp);
    };
  }, [dragging]);

  const dragOffsetRef = useRef({ x: 0, y: 0 });

  function onHandleDown(e: React.PointerEvent) {
    if (!ref.current) return;
    const rect = ref.current.getBoundingClientRect();
    dragOffsetRef.current = { x: e.clientX - rect.left, y: e.clientY - rect.top };
    setDragging(true);
    e.preventDefault();
  }

  const status = thread.status ?? "open";
  const isOpen = status === "open";
  const isDecisionIntent = kind === "decision";
  const isProcessing = Boolean(agentAction) || disabled;
  const replyLabel = !isOpen ? "Add note" : isDecisionIntent ? "Add to next turn" : "Add private note";
  const replyTitle = !isOpen
    ? `This thread is ${status} and stays out of the handoff`
    : isDecisionIntent
      ? "This note is included in the next handoff because this thread is marked Decision"
      : "This note stays private unless this thread is switched to Decision";

  return (
    <section
      ref={ref}
      className={`thread is-positioned${isFocused ? " is-focus" : ""}${dragging ? " is-dragging" : ""}${isDecisionIntent ? " is-decision" : " is-note"}${!isOpen ? " is-closed" : ""}`}
      data-thread-id={thread.id}
      data-thread-kind={kind}
      onMouseEnter={() => onHover(thread.id)}
      onMouseLeave={() => onHover(null)}
    >
      <header className="flex items-center gap-1.5">
        <button
          type="button"
          className="drag-handle -ml-1 inline-flex h-6 w-5 items-center justify-center rounded hover:bg-surface-muted"
          onPointerDown={onHandleDown}
          disabled={isProcessing}
          aria-label="Drag thread"
          title="Drag to move"
        >
          <GripVertical size={14} />
        </button>
        <h3 className="flex-1 truncate text-[13px] font-semibold">
          <span className="text-foreground-muted">
            {anchorLabel(thread.anchor)}
          </span>
        </h3>
        <button
          type="button"
          className="btn btn-ghost btn-sm"
          onClick={() => onEdit(thread.id)}
          disabled={isProcessing}
          aria-label="Edit comment"
          title="Edit comment and selected text"
        >
          <Pencil size={13} />
        </button>
        <button
          type="button"
          className="btn btn-ghost btn-sm btn-danger"
          onClick={() => onDelete(thread.id)}
          disabled={isProcessing}
          aria-label="Delete thread"
          title="Delete"
        >
          <Trash2 size={13} />
        </button>
      </header>

      <div className="mt-2 flex items-center gap-1.5">
        <KindToggle kind={kind} onChange={(k) => onSetKind(thread.id, k)} disabled={isProcessing} />
        {!isOpen ? <ThreadStatusPill status={status} /> : null}
      </div>

      <ul className="mt-2.5 space-y-2.5">
        {thread.messages.map((m) => (
          <li key={m.id} className="text-[13px] leading-relaxed">
            <div className="mb-0.5 flex items-center gap-2">
              <span className="chip">{m.author}</span>
              <span className="text-[11px] text-foreground-muted">{relativeTime(m.createdAt)}</span>
            </div>
            <p className="whitespace-pre-wrap break-words text-foreground">{m.body}</p>
          </li>
        ))}
      </ul>

      {sideAnswers.length > 0 ? (
        <ul className="mt-3 space-y-2 border-t border-border pt-2">
          {sideAnswers.map((ans) => (
            <li
              key={ans.id}
              className={`rounded-md border px-2.5 py-2 text-[13px] ${
                ans.promoted
                  ? "border-accent/40 bg-[color-mix(in_srgb,var(--color-accent)_8%,transparent)]"
                  : "border-border bg-surface-muted/40"
              }`}
            >
              <div className="flex flex-wrap items-center gap-2 text-[11px]">
                <span className="chip">
                  <Sparkles size={11} /> /btw
                </span>
                {ans.promoted ? (
                  <span className="pill pill-go">
                    <ArrowRight size={10} /> Q+A in handoff
                  </span>
                ) : (
                  <span className="pill pill-stay">
                    <EyeOff size={10} /> ephemeral, stays here
                  </span>
                )}
              </div>
              <p className="mt-1 font-medium text-foreground">{ans.question}</p>
              <p className="mt-1 whitespace-pre-wrap text-foreground-muted">{ans.answer}</p>
              <div className="mt-2 flex justify-end">
                {ans.promoted ? (
                  <button
                    type="button"
                    className="btn btn-ghost btn-sm"
                    onClick={() => onUnpromote(ans.id)}
                    disabled={isProcessing}
                    title="Drop this /btw Q+A from the next Codex turn"
                  >
                    Remove from handoff
                  </button>
                ) : (
                  <button
                    type="button"
                    className="btn btn-sm"
                    onClick={() => onPromote(ans.id)}
                    disabled={isProcessing}
                    title="Include the /btw question and answer in the next Codex turn"
                  >
                    <ArrowRight size={12} /> Send Q+A to next turn
                  </button>
                )}
              </div>
            </li>
          ))}
        </ul>
      ) : null}

      {agentAction ? (
        <div className="btw-thinking mt-3" role="status" aria-live="polite">
          <Sparkles size={13} />
          <span>{agentAction === "asking" ? "Codex is thinking about this /btw…" : "Codex is iterating on this comment…"}</span>
        </div>
      ) : null}

      <div className="mt-3 flex items-center gap-1.5">
        <button
          type="button"
          className="btn btn-sm flex-1"
          onClick={() => onReply(thread.id)}
          disabled={isProcessing}
          title={replyTitle}
        >
          <Reply size={13} /> {replyLabel}
        </button>
        {sideQuestionsEnabled ? (
          <button
            type="button"
            className="btn btn-sm"
            onClick={() => onAskSide(thread)}
            disabled={isProcessing}
            title="Ask Codex an ephemeral /btw question; stays out of the handoff unless you opt in"
          >
            <MessageCircleQuestion size={13} /> {agentAction === "asking" ? "Asking…" : "Ask /btw"}
          </button>
        ) : null}
        <button
          type="button"
          className="btn btn-sm"
          onClick={() => onIterate(thread)}
          disabled={isProcessing}
          title="Ask Codex to rewrite this anchored section now"
        >
          <Sparkles size={13} /> {agentAction === "iterating" ? "Iterating…" : "Iterate"}
        </button>
      </div>
    </section>
  );
}

function ThreadStatusPill({ status }: { status: string }) {
  const resolved = status === "resolved";
  return (
    <span className={`status-pill ${resolved ? "is-resolved" : "is-stale"}`}>
      {resolved ? <CheckCircle2 size={11} /> : <AlertTriangle size={11} />}
      {resolved ? "Resolved" : "Stale"}
    </span>
  );
}

function KindToggle({
  kind,
  onChange,
  disabled,
}: {
  kind: ThreadKind;
  onChange: (kind: ThreadKind) => void;
  disabled: boolean;
}) {
  const isDecision = kind === "decision";
  return (
    <fieldset className="kind-toggle" aria-label="Comment kind">
      <button
        type="button"
        className={`kind-pill ${isDecision ? "is-active is-go" : ""}`}
        onClick={() => onChange("decision")}
        disabled={disabled}
        title="This comment goes to Codex in the next turn"
        aria-pressed={isDecision}
      >
        <ArrowRight size={11} /> Decision
      </button>
      <button
        type="button"
        className={`kind-pill ${!isDecision ? "is-active is-stay" : ""}`}
        onClick={() => onChange("note")}
        disabled={disabled}
        title="Keep private; this comment stays out of the handoff"
        aria-pressed={!isDecision}
      >
        <EyeOff size={11} /> Note
      </button>
    </fieldset>
  );
}
