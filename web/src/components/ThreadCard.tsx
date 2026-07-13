import { useState } from "react";
import {
  AlertTriangle,
  Archive,
  ArrowRight,
  CheckCircle2,
  EyeOff,
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
  presentation?: "inline" | "rail";
}

export function ThreadCard(props: ThreadCardProps) {
  const {
    thread,
    kind,
    sideAnswers,
    isFocused,
    onHover,
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
    presentation = "rail",
  } = props;

  const status = thread.status ?? "open";
  const isOpen = status === "open";
  const isDecisionIntent = kind === "decision";
  const isProcessing = Boolean(agentAction) || disabled;
  const replyLabel = isDecisionIntent ? "Add to next turn" : "Add private note";
  const replyTitle = isDecisionIntent
    ? "This note is included in the next handoff because this thread is marked Decision"
    : "This note stays private unless this thread is switched to Decision";

  return (
    <section
      className={`thread is-flow${presentation === "inline" ? " is-inline-comment" : ""}${isFocused ? " is-focus" : ""}${isDecisionIntent ? " is-decision" : " is-note"}${!isOpen ? " is-closed is-historical" : ""}`}
      data-thread-id={thread.id}
      data-thread-kind={kind}
      onMouseEnter={() => onHover(thread.id)}
      onMouseLeave={() => onHover(null)}
    >
      <header className="thread-card-header">
        <div className="thread-card-heading">
          <span className={`thread-card-eyebrow${isOpen ? "" : " is-historical"}`}>
            {isOpen ? "Active feedback" : <><Archive size={11} /> Archived feedback</>}
          </span>
          <h3>{anchorLabel(thread.anchor)}</h3>
        </div>
        <div className="thread-card-tools">
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            onClick={() => onEdit(thread.id)}
            disabled={isProcessing}
            aria-label={isOpen ? "Edit comment" : "Inspect or re-anchor archived comment"}
            title={isOpen ? "Edit comment and selected text" : "Inspect or re-anchor archived comment"}
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
        </div>
      </header>

      <div className={`thread-card-meta${isOpen ? "" : " is-historical"}`}>
        {isOpen ? (
          <>
            <KindToggle kind={kind} onChange={(k) => onSetKind(thread.id, k)} disabled={isProcessing} />
            <span className="thread-intent-copy">
              {isDecisionIntent ? "Included in the next iteration" : "Private to this review"}
            </span>
          </>
        ) : (
          <>
            <ThreadStatusPill status={status} />
            <span className="thread-intent-copy">Not included in the next iteration</span>
          </>
        )}
      </div>

      {!isOpen ? (
        <p className="thread-history-note">
          {status === "resolved"
            ? "Handled in a newer revision. This is history and is not sent to Codex."
            : "The anchored text changed. This is history and is not sent to Codex."}
        </p>
      ) : null}

      <ul className="thread-message-list">
        {thread.messages.map((m) => (
          <li key={m.id} className="thread-message">
            <div className="thread-message-meta">
              <span className="chip">{m.author}</span>
              <span className="text-[11px] text-foreground-muted">{relativeTime(m.createdAt)}</span>
            </div>
            <p className="whitespace-pre-wrap break-words text-foreground">{m.body}</p>
          </li>
        ))}
      </ul>

      {sideAnswers.length > 0 ? (
        <ul className="thread-side-answer-list">
          {sideAnswers.map((ans) => (
            <li
              key={ans.id}
              className={`thread-side-answer ${
                ans.promoted
                  ? "is-promoted"
                  : ""
              }`}
            >
              <div className="flex flex-wrap items-center gap-2 text-[11px]">
                <span className="chip">
                  <Sparkles size={11} /> /btw
                </span>
                {!isOpen ? (
                  <span className="pill pill-stay">
                    <EyeOff size={10} /> archived with this thread
                  </span>
                ) : ans.promoted ? (
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
              {isOpen ? <div className="mt-2 flex justify-end">
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
              </div> : null}
            </li>
          ))}
        </ul>
      ) : null}

      {agentAction && isOpen ? (
        <div className="btw-thinking mt-3" role="status" aria-live="polite">
          <Sparkles size={13} />
          <span>{agentAction === "asking" ? "Codex is thinking about this /btw…" : "Codex is iterating on this comment…"}</span>
        </div>
      ) : null}

      {isOpen ? <div className="thread-card-actions">
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
      </div> : null}
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
